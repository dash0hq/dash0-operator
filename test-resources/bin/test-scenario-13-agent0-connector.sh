#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Deploys the operator with the agent0-connector enabled. The agent0-connector connects to
# DASH0_OUTBOUND_CONNECTOR_ENDPOINT. Set AGENT0_CONNECTOR_INSECURE=true for a plaintext connection.
#
# Run this with TELEMETRY_COLLECTION_ENABLED=false (no collectors, no target allocator), an empty DASH0_API_ENDPOINT,
# and potentially even SELF_MONITORING_ENABLED=false to have the operator only deploy agent0-connector, with everything
# else switched off.

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

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file

if [[ -z "${DASH0_OUTBOUND_CONNECTOR_ENDPOINT:-}" ]]; then
  echo "error: DASH0_OUTBOUND_CONNECTOR_ENDPOINT is not set"
  exit 1
fi
export AGENT0_CONNECTOR_ENABLED=true

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

echo "STEP $step_counter: rebuild images"
build_all_images
finish_step

echo "STEP $step_counter: push images"
push_all_images
finish_step

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

finish_scenario
