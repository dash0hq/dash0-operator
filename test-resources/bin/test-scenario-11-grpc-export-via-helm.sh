#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Deploys the operator with an OTLP/gRPC export configured via the Helm value operator.exports (instead of
# operator.dash0Export.*). The export points at OPERATOR_CONFIGURATION_VIA_HELM_GRPC_EXPORT_ENDPOINT (with the headers
# from OPERATOR_CONFIGURATION_VIA_HELM_GRPC_EXPORT_HEADERS), or at the otlp-sink if no endpoint is set. Verify the result
# with
# kubectl get dash0operatorconfiguration dash0-operator-configuration-auto-resource -o yaml
# and by watching the logs of the otlp-sink collector.

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"
# shellcheck source=./lib/kind
source "$scripts_lib/kind"
# shellcheck source=./lib/registry
source "$scripts_lib/registry"

operator_namespace="${OPERATOR_NAMESPACE:-$default_operator_ns}"
target_namespace="${1:-$default_target_ns}"
kind="${2:-$default_workload_kind}"
runtime_under_test="${3:-$default_runtime}"
additional_namespaces="false"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file

if [[ -z "${OPERATOR_CONFIGURATION_VIA_HELM_GRPC_EXPORT_ENDPOINT:-}" ]]; then
  export USE_OTLP_SINK=true
  # The http:// prefix marks the endpoint as insecure, for the collectors as well as for the operator's self-monitoring.
  export OPERATOR_CONFIGURATION_VIA_HELM_GRPC_EXPORT_ENDPOINT=http://otlp-sink.otlp-sink.svc.cluster.local:4317
fi
verify_kubectx
setup_test_environment "$target_namespace"

step_counter=1

echo "STEP $step_counter: remove old test resources"
test-resources/bin/test-cleanup.sh "${target_namespace}" false
finish_step

echo "STEP $step_counter: creating target namespace (if necessary)"
ensure_namespace_exists "${target_namespace}"
finish_step

echo "STEP $step_counter: creating operator namespace"
ensure_namespace_exists "$operator_namespace"
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

echo "STEP $step_counter: deploy the Dash0 operator using helm"
deploy_via_helm
finish_step

echo "STEP $step_counter: deploy the Dash0 monitoring resource to namespace ${target_namespace}"
install_monitoring_resource "$additional_namespaces"
finish_step

deploy_application_under_monitoring "$runtime_under_test"

finish_scenario
