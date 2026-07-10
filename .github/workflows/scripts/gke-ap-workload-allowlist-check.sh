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
#   When using a local Helm chart, the container image repositories ghcr.io/dash0hq/gke-ap-xxx will be used by default.
#   (These are allow-listed in addition to the official ghcr.io/dash0hq repositories in the Google GKE AutoPilot allowlists.
#   Testing with arbitrary container image repositories is not supported due to allowlist restrictions on the container images.)
#   IMPORTANT: Make sure the ghcr.io/dash0hq/gke-ap-xxx repositories have up-to-date images that represent your current
#   branch. This script does not build or push images.
# - Use IMAGE_REPOSITORY_PREFIX to control the prefix of the container image repositories used with a local Helm chart
#   (defaults to "ghcr.io/dash0hq/gke-ap-"). For example, set it to "ghcr.io/dash0hq/" to use the official image
#   repositories (both prefixes are allow-listed on the GKE Autopilot cluster).
# - Use IMAGE_TAG to control which tag is used for the container images (defaults to "latest").

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../../..

operator_namespace=gke-ap-workload-allowlist-check-operator
monitored_namespace=gke-ap-workload-allowlist-check-monitored
use_local_chart="${USE_LOCAL_CHART:-}"
if [[ "$use_local_chart" = "true" ]]; then
  chart="${OPERATOR_HELM_CHART:-helm-chart/dash0-operator}"
  image_tag="${IMAGE_TAG:-latest}"
  image_repository_prefix="${IMAGE_REPOSITORY_PREFIX:-ghcr.io/dash0hq/gke-ap-}"
  # Only the official ghcr.io/dash0hq repositories and the ghcr.io/dash0hq/gke-ap- repositories are allow-listed on the
  # GKE Autopilot cluster. Using any other prefix would make the operator pods unschedulable.
  if [[ "$image_repository_prefix" != "ghcr.io/dash0hq/" && "$image_repository_prefix" != "ghcr.io/dash0hq/gke-ap-" ]]; then
    echo "ERROR: unsupported IMAGE_REPOSITORY_PREFIX '$image_repository_prefix'."
    echo "Only 'ghcr.io/dash0hq/' and 'ghcr.io/dash0hq/gke-ap-' are allow-listed."
    exit 1
  fi
else
  chart="${OPERATOR_HELM_CHART:-dash0-operator/dash0-operator}"
fi
helm_release_name=dash0-operator

# Directory and archive used to collect cluster diagnostics when the check fails. These paths are relative to the
# repository root (we cd into it above), which is also the working directory of the GitHub Actions job, so the workflow
# can upload the archive as an artifact.
diagnostics_dir=gke-ap-allowlist-check-diagnostics
diagnostics_archive="${diagnostics_dir}.tar.gz"

# Set to "true" once all checks have passed. As long as this is not "true" when cleanup() runs, we assume the check has
# failed and collect cluster diagnostics before tearing everything down.
check_succeeded=false

# Collects diagnostic information from the cluster (workloads, pods, their logs, events, config maps and the relevant
# custom resources) into $diagnostics_dir and compresses it into $diagnostics_archive. This runs at the start of
# cleanup() when the check has failed, that is, before "helm uninstall" removes everything from the cluster.
collect_diagnostics() {
  set +e
  echo "the check did not succeed, collecting cluster diagnostics into \"$diagnostics_dir\" before cleanup"
  mkdir -p "$diagnostics_dir"

  # Runs a command and stores its combined stdout/stderr in $diagnostics_dir/$1. Best effort: a failing command must
  # never abort the diagnostics collection or the cleanup.
  dump() {
    local file="$diagnostics_dir/$1"
    shift
    mkdir -p "$(dirname "$file")"
    {
      echo "\$ $*"
      echo
      "$@" 2>&1
    } > "$file" || echo "(command exited with a non-zero status)" >> "$file"
  }

  # Cluster-scoped GKE Autopilot allowlist resources. A mismatch or a not-yet-ready AllowlistSynchronizer is the most
  # likely reason for this check to fail.
  dump allowlistsynchronizers-get.txt kubectl get allowlistsynchronizers.auto.gke.io -o wide
  dump allowlistsynchronizers-describe.txt kubectl describe allowlistsynchronizers.auto.gke.io
  dump workloadallowlists-get.txt kubectl get workloadallowlists.auto.gke.io -o wide
  dump workloadallowlists-describe.txt kubectl describe workloadallowlists.auto.gke.io

  # Dash0 custom resources.
  dump dash0monitorings-get.txt kubectl get dash0monitorings.operator.dash0.com --all-namespaces -o wide
  dump dash0monitorings-describe.txt kubectl describe dash0monitorings.operator.dash0.com --all-namespaces
  dump dash0operatorconfigurations-get.txt kubectl get dash0operatorconfigurations.operator.dash0.com -o wide
  dump dash0operatorconfigurations-describe.txt kubectl describe dash0operatorconfigurations.operator.dash0.com

  # Cluster-wide events (scheduling and allowlist rejections often show up here).
  dump events-all-namespaces.txt kubectl get events --all-namespaces --sort-by=.lastTimestamp

  local namespace
  for namespace in "$operator_namespace" "$monitored_namespace"; do
    dump "$namespace/workloads-get.txt" \
      kubectl get deployments,daemonsets,statefulsets,replicasets,jobs,pods,configmaps,services \
      --namespace "$namespace" -o wide
    dump "$namespace/workloads-describe.txt" \
      kubectl describe deployments,daemonsets,statefulsets,jobs --namespace "$namespace"
    dump "$namespace/pods-describe.txt" kubectl describe pods --namespace "$namespace"
    dump "$namespace/configmaps-describe.txt" kubectl describe configmaps --namespace "$namespace"
    dump "$namespace/events.txt" kubectl get events --namespace "$namespace" --sort-by=.lastTimestamp

    # Collect logs (all containers, current and previous) for every pod in the namespace.
    local pod
    while IFS= read -r pod; do
      [[ -z "$pod" ]] && continue
      dump "$namespace/logs-${pod}.txt" \
        kubectl logs --namespace "$namespace" "$pod" --all-containers=true --prefix=true
      # Previous logs only exist for containers that have restarted; ignore the file if there are none.
      if ! kubectl logs --namespace "$namespace" "$pod" --all-containers=true --prefix=true --previous \
        > "$diagnostics_dir/$namespace/logs-${pod}-previous.txt" 2>/dev/null; then
        rm -f "$diagnostics_dir/$namespace/logs-${pod}-previous.txt"
      fi
    done < <(kubectl get pods --namespace "$namespace" \
      -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null)
  done

  if tar -czf "$diagnostics_archive" "$diagnostics_dir"; then
    echo "cluster diagnostics have been collected in \"$diagnostics_archive\""
  else
    echo "WARNING: failed to create the diagnostics archive \"$diagnostics_archive\""
  fi
  set -e
}

cleanup() {
  if [[ "$check_succeeded" != "true" ]]; then
    collect_diagnostics
  fi

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
  helm_command+=" --set operator.image.repository=${image_repository_prefix}operator-controller"
  helm_command+=" --set operator.image.tag=$image_tag"
  helm_command+=" --set operator.instrumentationImage.repository=${image_repository_prefix}instrumentation"
  helm_command+=" --set operator.instrumentationImage.tag=$image_tag"
  helm_command+=" --set operator.collectorImage.repository=${image_repository_prefix}collector"
  helm_command+=" --set operator.collectorImage.tag=$image_tag"
  helm_command+=" --set operator.configurationReloaderImage.repository=${image_repository_prefix}configuration-reloader"
  helm_command+=" --set operator.configurationReloaderImage.tag=$image_tag"
  helm_command+=" --set operator.filelogOffsetSyncImage.repository=${image_repository_prefix}filelog-offset-sync"
  helm_command+=" --set operator.filelogOffsetSyncImage.tag=$image_tag"
  helm_command+=" --set operator.filelogOffsetVolumeOwnershipImage.repository=${image_repository_prefix}filelog-offset-volume-ownership"
  helm_command+=" --set operator.filelogOffsetVolumeOwnershipImage.tag=$image_tag"
  helm_command+=" --set operator.targetAllocatorImage.repository=${image_repository_prefix}target-allocator"
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

# Mark the check as successful so that cleanup() does not collect diagnostics.
check_succeeded=true
