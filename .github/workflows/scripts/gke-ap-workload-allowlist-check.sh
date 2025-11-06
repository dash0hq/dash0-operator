#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A smoke test to check whether the most recently published Helm chart has been modified in a way that would require
# updating the GKE Autopilot WorkloadAllowlists.
# The test tries to deploy the chart to a GKE Autopilot cluster.

set -xeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../../..

operator_namespace=gke-ap-workload-allowlist-check-operator
monitored_namespace=gke-ap-workload-allowlist-check-monitored
chart="${OPERATOR_HELM_CHART:-dash0-operator/dash0-operator}"
helm_release_name=dash0-operator

cleanup() {
  set +e

  helm uninstall \
    --namespace "$operator_namespace" \
    --ignore-not-found \
    --wait \
    "$helm_release_name"

  # If the workload allow list for the pre-delete hook does not match, it might stick around as a Zombie job, force-delete it.
  kubectl delete job --namespace "$operator_namespace" --ignore-not-found "${helm_release_name}-pre-delete" --wait --grace-period=0 --force

  kubectl delete namespace "$operator_namespace" --ignore-not-found --grace-period=0 --force

  kubectl delete namespace "$monitored_namespace" --ignore-not-found --grace-period=0 --force

  helm uninstall --namespace ensure-at-least-one-node podinfo || true
  kubectl delete namespace ensure-at-least-one-node --ignore-not-found --grace-period=0 --force || true

  return 0
}

retry_command() {
  local max_retries=10
  local retry_delay=5
  local attempt=1

  while [[ $attempt -le $max_retries ]]; do
    if "$@"; then
      return 0
    fi

    if [[ $attempt -eq $max_retries ]]; then
      echo "Command failed after $max_retries attempts: $*"
      return 1
    fi

    echo "Attempt $attempt failed, retrying in ${retry_delay} seconds..."
    sleep $retry_delay
    attempt=$((attempt + 1))
  done
}

# Deploy a dummy pod, to ensure the cluster is scaled up to at least one node. GKE AP has the infuriating UX problem
# that for example a namespace can be in state terminating forever if it is scaled down to zero nodes.
helm repo add podinfo https://stefanprodan.github.io/podinfo
kubectl create namespace ensure-at-least-one-node || true
helm install --namespace ensure-at-least-one-node podinfo podinfo/podinfo || true

if [[ "$chart" != "helm-chart/dash0-operator" ]]; then
  echo "installing the operator helm repo"
  helm repo add dash0-operator https://dash0hq.github.io/dash0-operator
  helm repo update dash0-operator
fi

# Install a trap to make sure we clean up after ourselves, no matter the outcome of the check.
trap cleanup HUP INT TERM EXIT

# Create the namespace and a dummy auth token secret.
echo "creating operator namespace $operator_namespace and auth token secret"
kubectl create namespace "$operator_namespace"
kubectl create secret \
  generic \
  dash0-authorization-secret \
  --namespace "$operator_namespace" \
  --from-literal=token=dummy-token

# Try to install the Helm chart:
helm_command="helm install --namespace $operator_namespace"
helm_command+=" --set operator.gke.autopilot.enabled=true"
helm_command+=" --set operator.dash0Export.enabled=true"
helm_command+=" --set operator.dash0Export.endpoint=ingress.dummy-url.aws.dash0.com:4317"
helm_command+=" --set operator.dash0Export.secretRef.name=dash0-authorization-secret"
helm_command+=" --set operator.dash0Export.secretRef.key=token"
helm_command+=" --set operator.dash0Export.apiEndpoint=https://api.dummy-url.aws.dash0.com"
helm_command+=" --set operator.prometheusCrdSupportEnabled=true"
helm_command+=" --set operator.clusterName=dummy-cluster-name"
if [[ "$chart" = "helm-chart/dash0-operator" ]]; then
  # When using a local Helm chart, the test repositories ghcr.io/dash0hq/gke-ap-xxx will be used. Make sure they have
  # up-to-date images, this script does not build or push images.
  helm_command+=" --set operator.image.repository=ghcr.io/dash0hq/gke-ap-operator-controller"
  helm_command+=" --set operator.image.tag=latest"
  helm_command+=" --set operator.initContainerImage.repository=ghcr.io/dash0hq/gke-ap-instrumentation"
  helm_command+=" --set operator.initContainerImage.tag=latest"
  helm_command+=" --set operator.collectorImage.repository=ghcr.io/dash0hq/gke-ap-collector"
  helm_command+=" --set operator.collectorImage.tag=latest"
  helm_command+=" --set operator.configurationReloaderImage.repository=ghcr.io/dash0hq/gke-ap-configuration-reloader"
  helm_command+=" --set operator.configurationReloaderImage.tag=latest"
  helm_command+=" --set operator.filelogOffsetSyncImage.repository=ghcr.io/dash0hq/gke-ap-filelog-offset-sync"
  helm_command+=" --set operator.filelogOffsetSyncImage.tag=latest"
  helm_command+=" --set operator.filelogOffsetVolumeOwnershipImage.repository=ghcr.io/dash0hq/gke-ap-filelog-offset-volume-ownership"
  helm_command+=" --set operator.filelogOffsetVolumeOwnershipImage.tag=latest"
  helm_command+=" --set operator.targetAllocatorImage.repository=ghcr.io/dash0hq/gke-ap-target-allocator"
  helm_command+=" --set operator.targetAllocatorImage.tag=latest"
fi
helm_command+=" $helm_release_name"
helm_command+=" $chart"

echo "running helm install"
$helm_command

echo "helm install has been successful, waiting for the collectors to become ready"

# Wait for the OTel collector workloads to become ready, this ensures that the WorkloadAllowlists for those also match.
kubectl \
  rollout status \
  daemonset "${helm_release_name}-opentelemetry-collector-agent-daemonset" \
  --namespace "$operator_namespace" \
  --timeout 90s

echo "the daemonset collector is ready now"

kubectl \
  rollout status \
  deployment "${helm_release_name}-cluster-metrics-collector-deployment" \
  --namespace "$operator_namespace" \
  --timeout 60s

echo "the deployment collector is ready now"

echo "deploying a monitoring resource to trigger deploying the target-allocator"

kubectl create namespace "$monitored_namespace"
kubectl apply --namespace "$monitored_namespace" -f - <<EOF
apiVersion: operator.dash0.com/v1beta1
kind: Dash0Monitoring
metadata:
  name: dash0-monitoring-resource
EOF
retry_command kubectl get --namespace "$monitored_namespace" dash0monitorings.operator.dash0.com/dash0-monitoring-resource
kubectl wait --namespace "$monitored_namespace" dash0monitorings.operator.dash0.com/dash0-monitoring-resource --for condition=Available --timeout 30s

kubectl \
  rollout status \
  deployment "${helm_release_name}-opentelemetry-target-allocator-deployment" \
  --namespace "$operator_namespace" \
  --timeout 60s
