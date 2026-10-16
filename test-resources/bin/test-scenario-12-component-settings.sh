#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Sets the component settings via spec.components of the Dash0 operator configuration resource and of the Dash0 Signal
# Control resource, and sets some of them to different values via Helm as well.

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

operator_namespace="${OPERATOR_NAMESPACE:-$default_operator_ns}"
target_namespace="${1:-$default_target_ns}"
kind="deployment"
runtime_under_test="nodejs" # nodejs is currently the only runtime with a /metrics endpoint
additional_namespaces="false"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file

# enable Signal Control in the Helm values and deploy the Signal Control resource
export FEATURE_SIGNAL_CONTROL_ENABLED=true
COMPONENT_SETTINGS_VIA_HELM="${COMPONENT_SETTINGS_VIA_HELM:-true}"
# The operator configuration resource is created manually, since the one the operator creates from the Helm values must
# not be edited.
DEPLOY_OPERATOR_CONFIGURATION_VIA_HELM="false"

verify_kubectx
setup_test_environment "$target_namespace"

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

echo "STEP $step_counter: rebuild images"
build_all_images
finish_step

echo "STEP $step_counter: push images"
push_all_images
finish_step

deploy_filelog_offsets_pvc

echo "STEP $step_counter: deploy the Dash0 operator using helm"
deploy_via_helm
finish_step

echo "STEP $step_counter: deploy the Dash0 operator configuration resource"
install_operator_configuration_resource
finish_step

operator_configuration_resource="dash0operatorconfigurations.operator.dash0.com"
operator_configuration_resource+="/dash0-operator-configuration-resource"

echo "STEP $step_counter: add spec.components and Prometheus CRD support to the operator configuration resource"
kubectl patch "$operator_configuration_resource" \
  --type merge \
  --patch-file test-resources/component-settings/operator-configuration-components.yaml
finish_step

deploy_signal_control_resource test-resources/component-settings/signal-control.yaml

deploy_application_under_monitoring "$runtime_under_test" "$nodejs_values_with_service_monitor"

if [[ "${DEPLOY_MONITORING_RESOURCE:-}" != "false" ]]; then
  echo "STEP $step_counter: deploy the Dash0 monitoring resource to namespace ${target_namespace}"
  install_monitoring_resource "$additional_namespaces"
  finish_step
else
  echo "not deploying a Dash0 monitoring resource"
  echo
fi

finish_scenario
