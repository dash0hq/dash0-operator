#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A smoke test that installs the operator from an OLM catalog image into the MicroShift cluster of the current kube
# context via test-resources/bin/test-olm-catalog.sh, and checks that the security context constraints admit the
# collectors and that they become ready.
#
# - MicroShift needs to run with its optional OLM package, like the MicroShift images of minc do.
# - The catalog ghcr.io/dash0hq/dash0-operator-catalog:main-dev is installed unless CATALOG_IMAGE is set.
# - ALLOWED_KUBECTXS needs to contain the current kube context. Like the test scenarios, the script reads
#   test-resources/.env, which takes precedence over the environment.
# - The script does not remove the operator again, test-resources/bin/test-olm-catalog.sh cleanup does that.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../../..

# shellcheck source=../../../test-resources/bin/lib/constants
source test-resources/bin/lib/constants

# shellcheck source=../../../test-resources/bin/lib/util
source test-resources/bin/lib/util

load_env_file
verify_kubectx

# Prints a message prefixed with the current UTC time, so the duration of each step can be derived from the output.
log() {
  echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*"
}

operator_namespace="${OPERATOR_NAMESPACE:-$default_operator_ns}"
monitored_namespace="$default_target_ns"
olm_namespace=openshift-operator-lifecycle-manager
olm_timeout=5m
workload_timeout_seconds=300

# The path is relative to the repository root, which is also the working directory of the GitHub Actions job, so the
# workflow can upload the directory as an artifact.
diagnostics_dir=microshift-olm-catalog-check-diagnostics

# Set to "true" once all checks have passed, otherwise the EXIT trap collects diagnostics.
check_succeeded=false

collect_diagnostics() {
  if [[ "$check_succeeded" = "true" ]]; then
    return
  fi
  set +e
  log "the check did not succeed, collecting cluster diagnostics into \"$diagnostics_dir\""
  mkdir -p "$diagnostics_dir"

  # Runs a command and stores its combined stdout/stderr in $diagnostics_dir/$1. A failing command must never abort the
  # diagnostics collection.
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

  dump nodes-describe.txt kubectl describe nodes
  dump events-all-namespaces.txt kubectl get events --all-namespaces --sort-by=.lastTimestamp
  dump olm-resources.yaml kubectl get \
    catalogsources.operators.coreos.com,operatorgroups.operators.coreos.com,subscriptions.operators.coreos.com,installplans.operators.coreos.com,clusterserviceversions.operators.coreos.com \
    --namespace "$operator_namespace" -o yaml
  dump dash0operatorconfigurations.yaml kubectl get dash0operatorconfigurations.operator.dash0.com -o yaml
  dump dash0monitorings.yaml kubectl get dash0monitorings.operator.dash0.com --all-namespaces -o yaml
  # The pod security admission labels and the security context constraints decide which pods are admitted.
  dump namespaces.yaml kubectl get namespace "$operator_namespace" "$monitored_namespace" -o yaml
  dump securitycontextconstraints.txt kubectl get securitycontextconstraints.security.openshift.io

  local namespace
  for namespace in "$operator_namespace" "$monitored_namespace" "$olm_namespace"; do
    dump "$namespace/workloads-get.txt" \
      kubectl get deployments,daemonsets,replicasets,jobs,pods,configmaps,services --namespace "$namespace" -o wide
    dump "$namespace/workloads-describe.txt" kubectl describe deployments,daemonsets,jobs --namespace "$namespace"
    dump "$namespace/pods-describe.txt" kubectl describe pods --namespace "$namespace"
    local pod
    while IFS= read -r pod; do
      [[ -z "$pod" ]] && continue
      dump "$namespace/logs-${pod}.txt" kubectl logs --namespace "$namespace" "$pod" --all-containers=true --prefix=true
    done < <(kubectl get pods --namespace "$namespace" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
  done

  log "cluster diagnostics have been collected in \"$diagnostics_dir\""
}

# Waits until the workload $1 in the operator namespace exists and its rollout has finished. Fails when the deadline
# (in SECONDS) passes.
wait_for_workload() {
  local workload=$1
  log "waiting for $workload to be created and to become ready"
  until kubectl get --namespace "$operator_namespace" "$workload" > /dev/null 2>&1; do
    if ((SECONDS >= deadline)); then
      log "error: $workload has not been created within $workload_timeout_seconds seconds"
      exit 1
    fi
    sleep 5
  done
  local remaining_seconds=$((deadline - SECONDS))
  if ((remaining_seconds < 1)); then
    remaining_seconds=1
  fi
  if ! kubectl rollout status --namespace "$operator_namespace" "$workload" --timeout="${remaining_seconds}s"; then
    log "error: $workload has not become ready within $workload_timeout_seconds seconds"
    exit 1
  fi
}

# Prints the security context constraint that admitted the first pod matching the label selector $1 in the operator
# namespace, fails if there is none.
verify_scc() {
  local selector=$1
  local all_sccs
  local scc
  all_sccs=$(kubectl get pods --namespace "$operator_namespace" --selector "$selector" \
    -o jsonpath="{.items[*].metadata.annotations['openshift\.io/scc']}")
  read -r scc _ <<< "$all_sccs"
  if [[ -z "$scc" ]]; then
    log "error: the pod with the labels $selector has no openshift.io/scc annotation, the cluster does not seem to" \
      "admit pods via security context constraints"
    exit 1
  fi
  log "the pod with the labels $selector has been admitted with the security context constraint $scc"
}

trap collect_diagnostics EXIT

log "kubectl version:"
kubectl version
log "current kubectx: $(kubectl config current-context)"

# MicroShift starts OLM asynchronously, its API server can be ready before the OLM resources have been created.
log "waiting for OLM to become ready"
for crd in catalogsources operatorgroups subscriptions installplans clusterserviceversions; do
  kubectl wait --for=create "crd/$crd.operators.coreos.com" --timeout="$olm_timeout"
  kubectl wait --for=condition=Established "crd/$crd.operators.coreos.com" --timeout="$olm_timeout"
done
for deployment in olm-operator catalog-operator; do
  kubectl wait --for=create "deployment/$deployment" --namespace "$olm_namespace" --timeout="$olm_timeout"
  kubectl rollout status "deployment/$deployment" --namespace "$olm_namespace" --timeout="$olm_timeout"
done

log "installing the operator from the catalog ${CATALOG_IMAGE:-ghcr.io/dash0hq/dash0-operator-catalog:main-dev}"
test-resources/bin/test-olm-catalog.sh

deadline=$((SECONDS + workload_timeout_seconds))
wait_for_workload daemonset/dash0-operator-opentelemetry-collector-agent-daemonset
wait_for_workload deployment/dash0-operator-cluster-metrics-collector-deployment
verify_scc app.kubernetes.io/name=dash0-operator,app.kubernetes.io/component=controller
verify_scc app.kubernetes.io/component=agent-collector

log "waiting for the monitoring resource to become available"
kubectl wait --namespace "$monitored_namespace" dash0monitorings.operator.dash0.com --all --for condition=Available \
  --timeout 60s

# Security context constraints and pod security admission reject pods with the reason FailedCreate.
failed_create_events=$(kubectl get events --namespace "$operator_namespace" --field-selector reason=FailedCreate \
  -o name)
if [[ -n "$failed_create_events" ]]; then
  log "error: there are FailedCreate events in the namespace $operator_namespace:"
  kubectl get events --namespace "$operator_namespace" --field-selector reason=FailedCreate
  exit 1
fi

log "success: all checks have passed"
check_succeeded=true
