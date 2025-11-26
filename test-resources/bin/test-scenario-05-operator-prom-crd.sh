#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

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
kind="deployment"
runtime_under_test="nodejs" # nodejs is currently the only runtime with a /metrics endpoint
additional_namespaces="false"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file
verify_kubectx
setup_test_environment

step_counter=1

echo "STEP $step_counter: remove old test resources"
test-resources/bin/test-cleanup.sh "${target_namespace}" false
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

deploy_dash0_api_sync_resources

echo "STEP $step_counter: rebuild images"
build_all_images
finish_step

echo "STEP $step_counter: push images"
push_all_images
finish_step

echo "STEP $step_counter: deploy the Dash0 operator using helm"
deploy_via_helm
finish_step

echo "STEP $step_counter: deploy nodejs app with servicemonitor (or podmonitor)"
deploy_application_under_monitoring "$runtime_under_test" "$nodejs_values_with_service_monitor"
finish_step

if [[ "${DEPLOY_MONITORING_RESOURCE:-}" != "false" ]]; then
  echo "STEP $step_counter: deploy the Dash0 monitoring resource to namespace ${target_namespace}"
  install_monitoring_resource "$target_namespace"
  finish_step
else
  echo "not deploying a Dash0 monitoring resource"
  echo
fi

finish_scenario
