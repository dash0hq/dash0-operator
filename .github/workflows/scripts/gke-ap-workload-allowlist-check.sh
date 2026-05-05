#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A smoke test to check whether the most recently published Helm chart has been modified in a way that would require
# updating the GKE Autopilot WorkloadAllowlists.
# The test tries to deploy the chart to a GKE Autopilot cluster.
#
# - By default, the most recent published Helm chart (dash0-operator/dash0-operator) will be tested.
# - Set USE_LOCAL_CHART=true to test a local Helm chart.
#   By default, the local chart directory helm-chart/dash0-operator will be used, set OPERATOR_HELM_CHART to override that.
#   When using a local Helm chart, the container image repositories ghcr.io/dash0hq/gke-ap-xxx will be used.
#   (These are allow-listed in addition to the official ghcr.io/dash0hq repositories in the Google GKE AutoPilot allowlists.
#   Testing with arbitrarycontainer image repositories is not supported due to allowlist restrictions on the container images.)
#   IMPORTANT: Make sure the ghcr.io/dash0hq/gke-ap-xxx repositories have up-to-date images that represent your current
#   branch. This script does not build or push images.
# - Use IMAGE_TAG to control which tag is used for the ghcr.io/dash0hq/gke-ap-xxx images (defaults to "latest").

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../../..

operator_namespace=gke-ap-workload-allowlist-check-operator
monitored_namespace=gke-ap-workload-allowlist-check-monitored
use_local_chart="${USE_LOCAL_CHART:-}"
if [[ "$use_local_chart" = "true" ]]; then
  chart="${OPERATOR_HELM_CHART:-helm-chart/dash0-operator}"
  image_tag="${IMAGE_TAG:-latest}"
else
  chart="${OPERATOR_HELM_CHART:-dash0-operator/dash0-operator}"
fi
helm_release_name=dash0-operator

cleanup() {
  set +e
  set -x
  echo "running cleanup"

  helm uninstall \
    --namespace "$operator_namespace" \
    --ignore-not-found \
    --wait \
    "$helm_release_name"

  # If the workload allow list for the pre-delete hook does not match, it might stick around as a Zombie job, force-delete it.
  kubectl delete job --namespace "$operator_namespace" --ignore-not-found "${helm_release_name}-pre-delete" --wait --grace-period=0 --force

  kubectl delete namespace "$operator_namespace" --ignore-not-found --grace-period=0 --force

  kubectl delete namespace "$monitored_namespace" --ignore-not-found --grace-period=0 --force

  helm uninstall --namespace ensure-at-least-one-node podinfo
  kubectl delete namespace ensure-at-least-one-node --ignore-not-found --grace-period=0 --force

  set +x
  set -e
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

echo kubectl version:
kubectl version
echo
echo "current kubectx: $(kubectl config current-context)"
echo

# Install a trap to make sure we clean up after ourselves, no matter the outcome of the check.
trap cleanup HUP INT TERM EXIT

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

# Verify that the cluster does not already contain leftover GKE Autopilot allowlist resources from a previous run. This might
# make the test invalid. We want to verify that the Helm chart correctly installs the AllowlistSynchronizer before deploying
# the operator, and that cleanup works correctly when removing the Helm chart. A manually installed AllowlistSynchronizer
# or WorkloadAllowlist might hide issues.
echo "checking that no GKE Autopilot allowlist resources exist before the check starts"
existing_synchronizers=$(kubectl get allowlistsynchronizers.auto.gke.io --no-headers --ignore-not-found 2>/dev/null || true)
existing_allowlists=$(kubectl get workloadallowlists.auto.gke.io --no-headers --ignore-not-found 2>/dev/null || true)
if [[ -n "$existing_synchronizers" || -n "$existing_allowlists" ]]; then
  echo "ERROR: the cluster already contains GKE Autopilot allowlist resources, please clean them up before running the check."
  if [[ -n "$existing_synchronizers" ]]; then
    echo "Existing AllowlistSynchronizers:"
    echo "$existing_synchronizers"
  fi
  if [[ -n "$existing_allowlists" ]]; then
    echo "Existing WorkloadAllowlists:"
    echo "$existing_allowlists"
  fi
  exit 1
fi

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
if [[ "$use_local_chart" = "true" ]]; then
  helm_command+=" --set operator.image.repository=ghcr.io/dash0hq/gke-ap-operator-controller"
  helm_command+=" --set operator.image.tag=$image_tag"
  helm_command+=" --set operator.instrumentationImage.repository=ghcr.io/dash0hq/gke-ap-instrumentation"
  helm_command+=" --set operator.instrumentationImage.tag=$image_tag"
  helm_command+=" --set operator.collectorImage.repository=ghcr.io/dash0hq/gke-ap-collector"
  helm_command+=" --set operator.collectorImage.tag=$image_tag"
  helm_command+=" --set operator.configurationReloaderImage.repository=ghcr.io/dash0hq/gke-ap-configuration-reloader"
  helm_command+=" --set operator.configurationReloaderImage.tag=$image_tag"
  helm_command+=" --set operator.filelogOffsetSyncImage.repository=ghcr.io/dash0hq/gke-ap-filelog-offset-sync"
  helm_command+=" --set operator.filelogOffsetSyncImage.tag=$image_tag"
  helm_command+=" --set operator.filelogOffsetVolumeOwnershipImage.repository=ghcr.io/dash0hq/gke-ap-filelog-offset-volume-ownership"
  helm_command+=" --set operator.filelogOffsetVolumeOwnershipImage.tag=$image_tag"
  helm_command+=" --set operator.targetAllocatorImage.repository=ghcr.io/dash0hq/gke-ap-target-allocator"
  helm_command+=" --set operator.targetAllocatorImage.tag=$image_tag"
fi
helm_command+=" $helm_release_name"
helm_command+=" $chart"

echo "running: $helm_command"
$helm_command

# Wait for the OTel collector workloads to become ready, this ensures that the WorkloadAllowlists for those also match.
echo "helm install has been successful, waiting for the collectors to be deployed and become ready"

set -x
kubectl wait \
  --for=create \
  daemonset "${helm_release_name}-opentelemetry-collector-agent-daemonset" \
  --namespace "$operator_namespace" \
  --timeout=60s
kubectl \
  rollout status \
  daemonset "${helm_release_name}-opentelemetry-collector-agent-daemonset" \
  --namespace "$operator_namespace" \
  --timeout 90s
set +x

echo "the daemonset collector is ready now"

set -x
kubectl wait \
  --for=create \
  deployment "${helm_release_name}-cluster-metrics-collector-deployment" \
  --namespace "$operator_namespace" \
  --timeout=20s
kubectl \
  rollout status \
  deployment "${helm_release_name}-cluster-metrics-collector-deployment" \
  --namespace "$operator_namespace" \
  --timeout 60s
set +x

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

set -x
kubectl wait \
  --for=create \
  deployment "${helm_release_name}-opentelemetry-target-allocator-deployment" \
  --namespace "$operator_namespace" \
  --timeout=60s
kubectl \
  rollout status \
  deployment "${helm_release_name}-opentelemetry-target-allocator-deployment" \
  --namespace "$operator_namespace" \
  --timeout 60s
set +x

echo "the target-allocator is ready now"
echo
echo "success: all checks have passed"
echo
