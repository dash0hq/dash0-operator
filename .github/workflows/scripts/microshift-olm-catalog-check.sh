#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A smoke test that installs the operator from an OLM catalog image into the MicroShift cluster of the current kube
# context via test-resources/bin/test-olm-catalog.sh, and checks that the security context constraints admit the
# collectors and that they become ready.
#
# - MicroShift needs to run with its optional OLM package, like the MicroShift images of minc do.
# - The catalog ghcr.io/dash0hq/dash0-operator-catalog:main-dev is installed unless CATALOG_IMAGE is set.
# - If UPGRADE_FROM_CSV is set (e.g. dash0-operator.v0.162.0), the script installs that version from the catalog
#   instead of the channel head, runs the checks, approves the upgrade to the channel head and runs the checks again.
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
upgrade_from_csv="${UPGRADE_FROM_CSV:-}"
upgrade_timeout_seconds=600
# the Subscription that test-resources/bin/test-olm-catalog.sh creates
subscription=dash0-operator

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

# Checks that the collectors are ready and admitted, that the monitoring resource is available and that no pod has been
# rejected.
verify_installation() {
  deadline=$((SECONDS + workload_timeout_seconds))
  local workload
  for workload in "${collector_workloads[@]}"; do
    wait_for_workload "$workload"
  done
  verify_scc app.kubernetes.io/name=dash0-operator,app.kubernetes.io/component=controller
  verify_scc app.kubernetes.io/component=agent-collector

  log "waiting for the monitoring resource to become available"
  kubectl wait --namespace "$monitored_namespace" dash0monitorings.operator.dash0.com --all --for condition=Available \
    --timeout 60s

  # Security context constraints and pod security admission reject pods with the reason FailedCreate.
  local failed_create_events
  failed_create_events=$(kubectl get events --namespace "$operator_namespace" --field-selector reason=FailedCreate \
    -o name)
  if [[ -n "$failed_create_events" ]]; then
    log "error: there are FailedCreate events in the namespace $operator_namespace:"
    kubectl get events --namespace "$operator_namespace" --field-selector reason=FailedCreate
    exit 1
  fi
}

# Runs the command $2 with the arguments $3... every 5 seconds until it succeeds, $1 describes the awaited state. Fails
# when the deadline (in SECONDS) passes.
wait_until() {
  local description=$1
  shift
  log "waiting until $description"
  until "$@"; do
    if ((SECONDS >= deadline)); then
      log "error: timed out waiting until $description"
      exit 1
    fi
    sleep 5
  done
}

# Succeeds once OLM has resolved the upgrade from $upgrade_from_csv: the Subscription progresses to another CSV and
# references the install plan for it. Sets upgrade_target_csv and upgrade_install_plan.
upgrade_resolved() {
  upgrade_target_csv=$(kubectl get subscriptions.operators.coreos.com "$subscription" \
    --namespace "$operator_namespace" -o jsonpath='{.status.currentCSV}')
  upgrade_install_plan=$(kubectl get subscriptions.operators.coreos.com "$subscription" \
    --namespace "$operator_namespace" -o jsonpath='{.status.installPlanRef.name}')
  if [[ -z "$upgrade_target_csv" || "$upgrade_target_csv" == "$upgrade_from_csv" || -z "$upgrade_install_plan" ]]; then
    return 1
  fi
  local install_plan_csvs
  install_plan_csvs=$(kubectl get installplans.operators.coreos.com "$upgrade_install_plan" \
    --namespace "$operator_namespace" -o jsonpath='{.spec.clusterServiceVersionNames[*]}')
  [[ " $install_plan_csvs " == *" $upgrade_target_csv "* ]]
}

# Succeeds once the Subscription has installed $upgrade_target_csv and the CSV has succeeded.
upgrade_installed() {
  local installed_csv
  local phase
  installed_csv=$(kubectl get subscriptions.operators.coreos.com "$subscription" \
    --namespace "$operator_namespace" -o jsonpath='{.status.installedCSV}')
  phase=$(kubectl get clusterserviceversions.operators.coreos.com "$upgrade_target_csv" \
    --namespace "$operator_namespace" --ignore-not-found -o jsonpath='{.status.phase}')
  [[ "$installed_csv" == "$upgrade_target_csv" && "$phase" == Succeeded ]]
}

# Succeeds once OLM has deleted the replaced CSV $upgrade_from_csv.
replaced_csv_deleted() {
  [[ -z "$(kubectl get clusterserviceversions.operators.coreos.com "$upgrade_from_csv" \
    --namespace "$operator_namespace" --ignore-not-found -o name)" ]]
}

# Succeeds once the pod templates of all collector workloads use the image $1.
collectors_use_image() {
  local workload
  local images
  for workload in "${collector_workloads[@]}"; do
    images=$(kubectl get --namespace "$operator_namespace" "$workload" \
      -o jsonpath='{.spec.template.spec.containers[*].image}')
    if [[ " $images " != *" $1 "* ]]; then
      return 1
    fi
  done
}

# Approves the upgrade from $upgrade_from_csv to the channel head and waits until OLM has replaced the CSV and the new
# operator version has updated the collector workloads.
upgrade_operator() {
  deadline=$((SECONDS + upgrade_timeout_seconds))
  wait_until "OLM has resolved the upgrade from $upgrade_from_csv" upgrade_resolved
  log "approving the install plan $upgrade_install_plan of the upgrade from $upgrade_from_csv to $upgrade_target_csv"
  kubectl patch installplans.operators.coreos.com "$upgrade_install_plan" --namespace "$operator_namespace" \
    --type merge --patch '{"spec":{"approved":true}}'
  wait_until "$upgrade_target_csv has been installed" upgrade_installed
  wait_until "the replaced CSV $upgrade_from_csv has been deleted" replaced_csv_deleted

  local collector_image
  collector_image=$(kubectl get deployment/dash0-operator-controller --namespace "$operator_namespace" \
    -o jsonpath='{.spec.template.spec.containers[?(@.name=="manager")].env[?(@.name=="DASH0_COLLECTOR_IMAGE")].value}')
  if [[ -z "$collector_image" ]]; then
    log "error: the operator deployment has no DASH0_COLLECTOR_IMAGE environment variable"
    exit 1
  fi
  wait_until "the collector workloads use the image $collector_image of $upgrade_target_csv" \
    collectors_use_image "$collector_image"
}

collector_workloads=(
  daemonset/dash0-operator-opentelemetry-collector-agent-daemonset
  deployment/dash0-operator-cluster-metrics-collector-deployment
)

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

catalog_image="${CATALOG_IMAGE:-ghcr.io/dash0hq/dash0-operator-catalog:main-dev}"
if [[ -z "$upgrade_from_csv" ]]; then
  log "installing the operator from the catalog $catalog_image"
  test-resources/bin/test-olm-catalog.sh
  verify_installation
else
  log "installing $upgrade_from_csv from the catalog $catalog_image"
  CATALOG_STARTING_CSV="$upgrade_from_csv" test-resources/bin/test-olm-catalog.sh
  verify_installation
  log "upgrading the operator from $upgrade_from_csv to the channel head"
  upgrade_operator
  verify_installation
fi

log "success: all checks have passed"
check_succeeded=true
