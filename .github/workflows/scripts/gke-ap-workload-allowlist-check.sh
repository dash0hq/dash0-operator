#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A smoke test to verify whether the local Helm chart has been modified in a way that would require updating the GKE
# Autopilot WorkloadAllowlists.
# The test tries to deploy the chart to a GKE Autopilot cluster that has the WorkloadAllowlists installed.

set -xeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../../..

kubectx="${GKE_AP_WORKLOAD_ALLOWLIST_CHECK_KUBECTX:-}"

if [[ -z "$kubectx" ]]; then
  echo The mandatory environment variable GKE_AP_WORKLOAD_ALLOWLIST_CHECK_KUBECTX is not set. Terminating.
  exit 1
fi

namespace=gke-ap-workload-allowlist-check
helm_release_name=dash0-operator
image_tag=latest

cleanup() {
  set +e

  helm uninstall \
    --kube-context "$kubectx" \
    --namespace "$namespace" \
    --ignore-not-found \
    --wait \
    "$helm_release_name"

  # If the workload allow list for the pre-delete hook does not match, it might stick around as a Zombie job, force-delete it.
  kubectl --context "$kubectx" delete job --namespace "$namespace" --ignore-not-found "${helm_release_name}-pre-delete" --wait --grace-period=0 --force

  kubectl --context "$kubectx" delete namespace "$namespace" --ignore-not-found --grace-period=0 --force
}

# Install a trap to make sure we clean up after ourselves, no matter the outcome of the check.
trap cleanup HUP INT TERM EXIT

# Create the namespace and a dummy auth token secret.
kubectl --context "$kubectx" create namespace "$namespace"
kubectl --context "$kubectx" create secret \
  generic \
  dash0-authorization-secret \
  --namespace "$namespace" \
  --from-literal=token=dummy-token

# Try to install the Helm chart with a typical set of configuration values:
helm install \
  --kube-context "$kubectx" \
  --namespace "$namespace" \
  --set operator.gke.autopilot.enabled=true \
  --set operator.dash0Export.enabled=true \
  --set operator.dash0Export.endpoint=ingress.dummy-url.aws.dash0.com:4317 \
  --set operator.dash0Export.secretRef.name=dash0-authorization-secret \
  --set operator.dash0Export.secretRef.key=token \
  --set operator.dash0Export.apiEndpoint=https://api.dummy-url.aws.dash0.com \
  --set operator.clusterName=dummy-cluster-name \
  --set operator.image.tag="$image_tag" \
  --set operator.initContainerImage.tag="$image_tag" \
  --set operator.collectorImage.tag="$image_tag" \
  --set operator.configurationReloaderImage.tag="$image_tag" \
  --set operator.filelogOffsetSyncImage.tag="$image_tag" \
  --set operator.filelogOffsetVolumeOwnershipImage.tag="$image_tag" \
  dash0-operator helm-chart/dash0-operator

# Wait for the OTel collector workloads to become ready, this ensures that the WorkloadAllowlists for those also match.
kubectl --context "$kubectx" \
  rollout status \
  daemonset "${helm_release_name}-opentelemetry-collector-agent-daemonset" \
  --namespace "$namespace" \
  --timeout 90s
kubectl --context "$kubectx" \
  rollout status \
  deployment "${helm_release_name}-cluster-metrics-collector-deployment" \
  --namespace "$namespace" \
  --timeout 60s

echo "helm install has been successful"

