#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

operator_namespace="${OPERATOR_NAMESPACE:-$default_operator_ns}"
target_namespace="${1:-$default_target_ns}"
kind="${2:-$default_workload_kind}"
runtime_under_test="${3:-$default_runtime}"
additional_namespaces="${ADDITIONAL_NAMESPACES:-false}"
operator_webhook_service_name="$default_operator_webhook_service_name"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file
verify_kubectx
setup_test_environment "$target_namespace"

step_counter=1

echo "STEP $step_counter: remove old test resources"
test-resources/bin/test-cleanup.sh "${target_namespace}" false
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

deploy_filelog_offsets_pvc

echo "STEP $step_counter: creating test namespaces"
ensure_namespace_exists test-namespace-1
ensure_namespace_exists test-namespace-2
ensure_namespace_exists test-namespace-3
kubectl label namespace test-namespace-3 dash0.com/enable=false

DEPLOY_OPERATOR_CONFIGURATION_VIA_HELM=false
echo "STEP $step_counter: deploy the Dash0 operator using helm"
deploy_via_helm
finish_step

sleep 10

echo "STEP $step_counter: deploy the Dash0 operator configuration resource"
kubectl apply -f test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.auto-namespace-monitoring.yaml

echo "waiting for the operator configuration resource to become available"
kubectl wait dash0operatorconfigurations.operator.dash0.com/dash0-operator-configuration-resource --for condition=Available
finish_step

echo "STEP $step_counter: creating test namespaces"
ensure_namespace_exists test-namespace-4
ensure_namespace_exists test-namespace-5
ensure_namespace_exists test-namespace-6
kubectl label namespace test-namespace-6 dash0.com/enable=false

finish_step

finish_scenario
