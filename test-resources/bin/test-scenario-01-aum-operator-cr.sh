#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

target_namespace=${1:-test-namespace}
kind=${2:-deployment}

source test-resources/bin/util
load_env_file
verify_kubectx
setup_test_environment

step_counter=1

echo "STEP $step_counter: remove old test resources"
test-resources/bin/test-cleanup.sh "${target_namespace}" false
finish_step

echo "STEP $step_counter: creating target namespace (if necessary)"
test-resources/bin/ensure-namespace-exists.sh "${target_namespace}"
finish_step

echo "STEP $step_counter: creating operator namespace and authorization token secret"
test-resources/bin/ensure-namespace-exists.sh dash0-system
kubectl create secret \
  generic \
  dash0-authorization-secret \
  --namespace dash0-system \
  --from-literal=token="${DASH0_AUTHORIZATION_TOKEN}"
finish_step

echo "STEP $step_counter: install third-party custom resource definitions"
install_third_party_crds
finish_step


install_third_party_resources

echo "STEP $step_counter: rebuild images"
build_all_images
finish_step

if [[ "${DEPLOY_APPLICATION_UNDER_MONITORING:-}" != false ]]; then
  echo "STEP $step_counter: deploy application under monitoring"
  test-resources/node.js/express/deploy.sh "${target_namespace}" "${kind}"
  finish_step
fi

echo "STEP $step_counter: deploy the Dash0 operator using helm"
deploy_via_helm
finish_step

if [[ "${DEPLOY_OPERATOR_CONFIGURATION_VIA_HELM:-}" == false ]]; then
  # if no operator configuration resource has been deployed via the helm chart, deploy one now
  echo "STEP $step_counter: deploy the Dash0 operator configuration resource"
  install_operator_configuration_resource
  finish_step
else
  echo "not deploying a Dash0 operator configuration resource (has been deployed with the helm chart already)"
  echo
fi

if [[ "${DEPLOY_MONITORING_RESOURCE:-}" != false ]]; then
  echo "STEP $step_counter: deploy the Dash0 monitoring resource to namespace ${target_namespace}"
  install_monitoring_resource
  finish_step
else
  echo "not deploying a Dash0 monitoring resource"
  echo
fi

