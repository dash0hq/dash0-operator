#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Compares the scrape configs and the target assignments of the Dash0 scrape coordinator with the ones of the
# OpenTelemetry target-allocator. Run test-scenario-05-operator-prom-crd.sh first (with TA_MTLS_ENABLED unset), which
# builds the images, deploys the operator with the target-allocator and the Node.js test application with a
# ServiceMonitor. This script then deploys additional Prometheus custom resources (test-resources/scrape-coordinator)
# and a scrape coordinator next to the target-allocator, fetches /scrape_configs and, for every job and every collector
# DaemonSet pod, /jobs/<job>/targets?collector_id=<pod> from both, and prints the differences. Usage:
#   test-resources/bin/scrape-coordinator-diff.sh [target-namespace]
#
# Expected differences:
# * The job of the ServiceMonitor scrape-coordinator-diff-bearer-token-file only exists in the target-allocator, the
#   scrape coordinator drops scrape configs that reference a file.
# * Targets that only carry __meta_kubernetes_endpointslice_endpoint_node_name, but not
#   __meta_kubernetes_pod_node_name, are only assigned by the scrape coordinator.
# * Targets that Prometheus rejects after relabeling (for example an invalid address or label value) are only served by
#   the target-allocator.
# * Among targets of a job that are identical after relabeling but differ in their discovered labels, the two may serve
#   a different one.
#
# Both serve the secrets of /scrape_configs masked over plain HTTP, so the rotation of the basic auth password during
# the run only shows up as a difference if one of them drops or keeps the job inconsistently.
#
# Set CLEANUP=false to keep the scrape coordinator and the additional custom resources after the comparison.

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

operator_namespace="${OPERATOR_NAMESPACE:-$default_operator_ns}"
target_namespace="${1:-$default_target_ns}"
target_allocator_service="${TARGET_ALLOCATOR_SERVICE:-dash0-operator-opentelemetry-target-allocator-service}"
target_allocator_local_port="${TARGET_ALLOCATOR_LOCAL_PORT:-18080}"
scrape_coordinator_local_port="${SCRAPE_COORDINATOR_LOCAL_PORT:-18081}"
# Both components debounce changes of custom resources, targets and collectors by a few seconds.
settle_seconds="${SETTLE_SECONDS:-30}"

# shellcheck source=./lib/util
source "$scripts_lib/util"

for required_command in curl envsubst jq kubectl; do
  if ! command -v "$required_command" > /dev/null; then
    echo "error: $required_command is required"
    exit 1
  fi
done

load_env_file
verify_kubectx
determine_container_images

scrape_coordinator_image="${scrape_coordinator_image_repository}:${scrape_coordinator_image_tag}"
if [[ -n "$scrape_coordinator_image_digest" ]]; then
  scrape_coordinator_image="${scrape_coordinator_image_repository}@${scrape_coordinator_image_digest}"
fi

work_dir="$(mktemp -d)"
port_forward_pids=()

cleanup() {
  for pid in "${port_forward_pids[@]}"; do
    kill "$pid" 2> /dev/null || true
  done
  if [[ "${CLEANUP:-}" != "false" ]]; then
    echo "removing the scrape coordinator and the additional custom resources"
    kubectl delete --ignore-not-found -f "$work_dir/scrape-coordinator.yaml" || true
    kubectl delete --ignore-not-found -n "$target_namespace" -f test-resources/scrape-coordinator/monitors.yaml || true
    kubectl delete --ignore-not-found -n "$target_namespace" -f test-resources/scrape-coordinator/probe.yaml || true
  fi
  rm -rf "$work_dir"
}
trap cleanup EXIT

step_counter=1

echo "STEP $step_counter: verify that the target-allocator is running"
kubectl get --namespace "$operator_namespace" service "$target_allocator_service" > /dev/null
finish_step

echo "STEP $step_counter: deploy additional Prometheus custom resources to namespace $target_namespace"
kubectl apply -n "$target_namespace" -f test-resources/scrape-coordinator/monitors.yaml
if kubectl get crd probes.monitoring.coreos.com > /dev/null 2>&1; then
  kubectl apply -n "$target_namespace" -f test-resources/scrape-coordinator/probe.yaml
else
  echo "the Probe custom resource definition is not installed, skipping the Probe"
fi
finish_step

echo "STEP $step_counter: deploy the scrape coordinator ($scrape_coordinator_image)"
# shellcheck disable=SC2016
OPERATOR_NAMESPACE="$operator_namespace" \
  TARGET_NAMESPACE="$target_namespace" \
  SCRAPE_COORDINATOR_IMAGE="$scrape_coordinator_image" \
  SCRAPE_COORDINATOR_IMAGE_PULL_POLICY="${scrape_coordinator_image_pull_policy:-IfNotPresent}" \
  envsubst \
  '$OPERATOR_NAMESPACE $TARGET_NAMESPACE $SCRAPE_COORDINATOR_IMAGE $SCRAPE_COORDINATOR_IMAGE_PULL_POLICY' \
  < test-resources/scrape-coordinator/scrape-coordinator.yaml.template \
  > "$work_dir/scrape-coordinator.yaml"
kubectl apply -f "$work_dir/scrape-coordinator.yaml"
kubectl rollout status --namespace "$operator_namespace" deployment/scrape-coordinator-diff --timeout 120s
finish_step

echo "STEP $step_counter: wait $settle_seconds seconds for both components to pick up the changes"
sleep "$settle_seconds"
finish_step

echo "STEP $step_counter: rotate the basic auth password and wait $settle_seconds seconds"
kubectl patch secret scrape-coordinator-diff-basic-auth \
  --namespace "$target_namespace" \
  --type merge \
  --patch '{"stringData":{"password":"scrape-coordinator-diff-password-rotated"}}'
sleep "$settle_seconds"
finish_step

echo "STEP $step_counter: forward local ports to the target-allocator and the scrape coordinator"
kubectl port-forward --namespace "$operator_namespace" "service/$target_allocator_service" \
  "$target_allocator_local_port:80" > "$work_dir/port-forward-ta.log" 2>&1 &
port_forward_pids+=("$!")
kubectl port-forward --namespace "$operator_namespace" deployment/scrape-coordinator-diff \
  "$scrape_coordinator_local_port:8080" > "$work_dir/port-forward-sc.log" 2>&1 &
port_forward_pids+=("$!")
for local_port in "$target_allocator_local_port" "$scrape_coordinator_local_port"; do
  for _ in {1..30}; do
    if curl --silent --fail --output /dev/null "http://localhost:$local_port/scrape_configs"; then
      break
    fi
    sleep 1
  done
done
finish_step

target_allocator_url="http://localhost:$target_allocator_local_port"
scrape_coordinator_url="http://localhost:$scrape_coordinator_local_port"
differences=0

compare() {
  local description="$1"
  local target_allocator_file="$2"
  local scrape_coordinator_file="$3"
  if diff -u \
    --label "target-allocator: $description" "$target_allocator_file" \
    --label "scrape coordinator: $description" "$scrape_coordinator_file"; then
    echo "identical: $description"
  else
    differences=$((differences + 1))
  fi
}

echo "STEP $step_counter: compare /scrape_configs"
curl --silent --fail "$target_allocator_url/scrape_configs" | jq --sort-keys . > "$work_dir/ta-scrape-configs.json"
curl --silent --fail "$scrape_coordinator_url/scrape_configs" | jq --sort-keys . > "$work_dir/sc-scrape-configs.json"
compare "/scrape_configs" "$work_dir/ta-scrape-configs.json" "$work_dir/sc-scrape-configs.json"
finish_step

echo "STEP $step_counter: compare the targets per job and collector"
jq --raw-output 'keys[]' "$work_dir/ta-scrape-configs.json" "$work_dir/sc-scrape-configs.json" | sort -u \
  > "$work_dir/jobs.txt"
kubectl get pods \
  --namespace "$operator_namespace" \
  --selector app.kubernetes.io/component=agent-collector \
  --output jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' \
  > "$work_dir/collectors.txt"
while read -r job; do
  escaped_job="$(jq --raw-output --null-input --arg job "$job" '$job | @uri')"
  while read -r collector; do
    for component in ta sc; do
      base_url="$target_allocator_url"
      if [[ "$component" = "sc" ]]; then
        base_url="$scrape_coordinator_url"
      fi
      curl --silent --fail "$base_url/jobs/$escaped_job/targets?collector_id=$collector" |
        jq --sort-keys --compact-output '.[] | .targets |= sort' |
        sort > "$work_dir/$component-targets.json" || true
    done
    compare "job $job, collector $collector" "$work_dir/ta-targets.json" "$work_dir/sc-targets.json"
  done < "$work_dir/collectors.txt"
done < "$work_dir/jobs.txt"
finish_step

if [[ "$differences" -gt 0 ]]; then
  echo "found $differences difference(s), see the header of this script for the expected ones"
  exit 1
fi
echo "no differences found"
