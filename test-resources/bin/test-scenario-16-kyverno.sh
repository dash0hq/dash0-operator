#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Installs Kyverno with restrictive policies in Enforce mode via test-resources/bin/deploy-kyverno.sh, then deploys the
# operator with the target-allocator, the agent0-connector and Signal Control enabled, and checks that Kyverno admits
# all operator workloads. Kyverno only enforces its policies in the operator namespace by default (see
# KYVERNO_ENFORCED_NAMESPACES in deploy-kyverno.sh), so the application under monitoring is not affected.
#
# The scenario fails if an expected workload is not created or does not become ready, if there are FailedCreate
# events in the operator namespace, or if the operator manager logs a Kyverno denial. The pre-delete hook is exercised
# by the next test-cleanup.sh run, which uninstalls the operator while Kyverno is still installed and removes Kyverno
# afterwards.

export PROMETHEUS_CRD_SUPPORT_ENABLED="${PROMETHEUS_CRD_SUPPORT_ENABLED:-true}"
export AGENT0_CONNECTOR_ENABLED="${AGENT0_CONNECTOR_ENABLED:-true}"
export FEATURE_SIGNAL_CONTROL_ENABLED="${FEATURE_SIGNAL_CONTROL_ENABLED:-true}"

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

operator_namespace="${OPERATOR_NAMESPACE:-$default_operator_ns}"
target_namespace="${1:-$default_target_ns}"
kind="${2:-$default_workload_kind}"
runtime_under_test="${3:-$default_runtime}"
additional_namespaces="false"
operator_webhook_service_name="$default_operator_webhook_service_name"
workload_timeout_seconds="${KYVERNO_SCENARIO_WORKLOAD_TIMEOUT_SECONDS:-300}"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file
verify_kubectx
setup_test_environment "$target_namespace"

# Prints the logs of all operator manager pods, not only of one of them, in case there are several replicas.
manager_logs() {
  kubectl logs \
    --namespace "$operator_namespace" \
    --selector app.kubernetes.io/name=dash0-operator,app.kubernetes.io/component=controller \
    --container manager \
    --tail=-1 \
    --prefix
}

print_admission_errors() {
  echo "FailedCreate events in the namespace $operator_namespace:"
  kubectl get events --namespace "$operator_namespace" --field-selector reason=FailedCreate || true
  echo "Kyverno denials in the operator manager log:"
  manager_logs | grep "validate.kyverno.svc" || true
}

verify_workloads_admitted() {
  local workloads=("deployment/dash0-operator-controller")
  if [[ "${TELEMETRY_COLLECTION_ENABLED:-}" != "false" ]]; then
    workloads+=("daemonset/dash0-operator-opentelemetry-collector-agent-daemonset")
    if [[ "${KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED:-}" != "false" ]]; then
      workloads+=("deployment/dash0-operator-cluster-metrics-collector-deployment")
    fi
    # Prometheus CRD support is only enabled via Helm, the operator configuration resource templates do not set it.
    if [[ "$PROMETHEUS_CRD_SUPPORT_ENABLED" = "true" && "${PROMETHEUS_SCRAPING_ENABLED:-}" != "false" &&
      "${DEPLOY_MONITORING_RESOURCE:-}" != "false" && "${DEPLOY_OPERATOR_CONFIGURATION_VIA_HELM:-}" != "false" ]]; then
      workloads+=("deployment/dash0-operator-opentelemetry-target-allocator-deployment")
    fi
  fi
  if [[ "$AGENT0_CONNECTOR_ENABLED" = "true" ]]; then
    workloads+=("deployment/dash0-operator-agent0-connector")
  fi
  if [[ "$FEATURE_SIGNAL_CONTROL_ENABLED" = "true" ]]; then
    # The Signal Control collector needs a Dash0 export, a plain gRPC export via Helm does not provide one.
    if [[ -z "${OPERATOR_CONFIGURATION_VIA_HELM_GRPC_EXPORT_ENDPOINT:-}" ]]; then
      workloads+=("deployment/dash0-operator-signal-control-collector-deployment")
    fi
    workloads+=("deployment/dash0-operator-edge-proxy")
  fi

  local deadline=$((SECONDS + workload_timeout_seconds))
  local workload
  for workload in "${workloads[@]}"; do
    echo "waiting for $workload to be created and to become ready"
    until kubectl get --namespace "$operator_namespace" "$workload" > /dev/null 2>&1; do
      if ((SECONDS >= deadline)); then
        echo "error: $workload has not been created within ${workload_timeout_seconds} seconds"
        print_admission_errors
        exit 1
      fi
      sleep 5
    done
    local remaining_seconds=$((deadline - SECONDS))
    if ((remaining_seconds < 1)); then
      remaining_seconds=1
    fi
    if ! kubectl rollout status --namespace "$operator_namespace" "$workload" --timeout="${remaining_seconds}s"; then
      echo "error: $workload has not become ready within ${workload_timeout_seconds} seconds"
      print_admission_errors
      exit 1
    fi
  done

  local failed_create_events
  failed_create_events=$(
    kubectl get events --namespace "$operator_namespace" --field-selector reason=FailedCreate -o name
  )
  if [[ -n "$failed_create_events" ]]; then
    echo "error: there are FailedCreate events in the namespace $operator_namespace"
    print_admission_errors
    exit 1
  fi
  local manager_log
  manager_log=$(manager_logs)
  if [[ "$manager_log" == *"validate.kyverno.svc"* ]]; then
    echo "error: Kyverno has rejected a request of the operator manager"
    print_admission_errors
    exit 1
  fi
  echo "Kyverno has admitted all workloads of the operator: ${workloads[*]}"
}

step_counter=1

echo "STEP $step_counter: remove old test resources"
test-resources/bin/test-cleanup.sh "${target_namespace}" false
finish_step

echo "STEP $step_counter: deploy Kyverno and the Kyverno policies"
test-resources/bin/deploy-kyverno.sh
finish_step

echo "STEP $step_counter: creating target namespace (if necessary)"
ensure_namespace_exists "${target_namespace}"
finish_step

echo "STEP $step_counter: creating operator namespace and authorization token secret"
ensure_namespace_exists "$operator_namespace"
kubectl create secret \
  generic \
  dash0-authorization-secret \
  --namespace "$operator_namespace" \
  --from-literal=token="${DASH0_AUTHORIZATION_TOKEN}"
finish_step

echo "STEP $step_counter: install third-party custom resource definitions"
install_third_party_crds
finish_step

deploy_additional_resources

echo "STEP $step_counter: rebuild images"
build_all_images
finish_step

echo "STEP $step_counter: push images"
push_all_images
finish_step

deploy_application_under_monitoring "$runtime_under_test"

echo "STEP $step_counter: deploy the Dash0 operator using helm"
deploy_via_helm
finish_step

if [[ "${DEPLOY_OPERATOR_CONFIGURATION_VIA_HELM:-}" = "false" ]]; then
  # if no operator configuration resource has been deployed via the helm chart, deploy one now
  echo "STEP $step_counter: deploy the Dash0 operator configuration resource"
  install_operator_configuration_resource
  finish_step
else
  echo "not deploying a Dash0 operator configuration resource (has been deployed with the helm chart already)"
  echo
fi

if [[ "${DEPLOY_MONITORING_RESOURCE:-}" != "false" ]]; then
  echo "STEP $step_counter: deploy the Dash0 monitoring resource to namespace ${target_namespace}"
  install_monitoring_resource "$additional_namespaces"
  finish_step
else
  echo "not deploying a Dash0 monitoring resource"
  echo
fi

deploy_signal_control_resource test-resources/customresources/dash0signalcontrol/dash0signalcontrol.yaml

echo "STEP $step_counter: verify that Kyverno has admitted all workloads of the operator"
verify_workloads_admitted
finish_step

finish_scenario
