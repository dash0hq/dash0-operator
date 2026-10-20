#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Deploys the operator with Prometheus CRD support and operator.targetAllocator.restrictToNamespaces set to the target
# namespace, with ServiceMonitors and Dash0 monitoring resources in the target namespace and in the namespaces
# test-namespace-2 and test-namespace-3, which are not listed. See CONTRIBUTING.md for the expected outcome.

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
# The apps in test-namespace-2 and test-namespace-3 are installed by this script, see below.
additional_namespaces="false"
USE_CERT_MANAGER="true"
PROMETHEUS_CRD_SUPPORT_ENABLED="true"
TA_MTLS_ENABLED="${TA_MTLS_ENABLED:-true}"
TA_ALLOW_INSECURE_AUTH_SECRETS="${TA_ALLOW_INSECURE_AUTH_SECRETS:-false}"
TA_RESTRICT_TO_NAMESPACES="${TA_RESTRICT_TO_NAMESPACES:-$target_namespace}"
DEPLOY_CADVISOR_SCRAPECONFIG="true"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file
verify_kubectx
setup_test_environment "$target_namespace"

step_counter=1

echo "STEP $step_counter: remove old test resources"
test-resources/bin/test-cleanup.sh "${target_namespace}" false
finish_step

echo "STEP $step_counter: creating target namespaces (if necessary)"
ensure_namespace_exists "${target_namespace}"
ensure_namespace_exists test-namespace-2
ensure_namespace_exists test-namespace-3
IFS=',' read -r -a restrict_to_namespaces <<< "$TA_RESTRICT_TO_NAMESPACES"
for restrict_to_namespace in "${restrict_to_namespaces[@]}"; do
  # The Helm chart creates a role binding in every listed namespace, so they need to exist before deploying it.
  ensure_namespace_exists "$restrict_to_namespace"
done
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

echo "STEP $step_counter: deploy target-allocator issuers and certs"
deploy_ta_issuers_and_certs "$operator_namespace"

echo "STEP $step_counter: rebuild images"
build_all_images
finish_step

echo "STEP $step_counter: push images"
push_all_images
finish_step

echo "STEP $step_counter: deploy the Dash0 operator using helm"
deploy_via_helm
finish_step

echo "STEP $step_counter: deploy nodejs apps with servicemonitor"
deploy_application_under_monitoring "$runtime_under_test" "$nodejs_values_with_service_monitor"
if [[ "${DEPLOY_APPLICATION_UNDER_MONITORING:-}" != "false" ]]; then
  # Only the deployment of the test app has a ServiceMonitor. Its ingress already exists in the target namespace.
  for additional_namespace_index in 2 3; do
    helm_install_test_application \
      test-resources/node.js/express/helm-chart \
      "test-app-nodejs-$additional_namespace_index" \
      "test-namespace-$additional_namespace_index" \
      deployment \
      "$test_app_nodejs_image_repository" \
      "$test_app_nodejs_image_tag" \
      "$test_app_nodejs_image_pull_policy" \
      "$nodejs_values_with_service_monitor_without_ingress"
  done
fi
finish_step

echo "STEP $step_counter: deploy scrapeconfig for scraping cadvisor metrics"
deploy_cadvisor_scrapeconfig
finish_step

if [[ "${DEPLOY_MONITORING_RESOURCE:-}" != "false" ]]; then
  echo "STEP $step_counter: deploy the Dash0 monitoring resources to namespace ${target_namespace}, test-namespace-2 and test-namespace-3"
  install_monitoring_resource true
  finish_step
else
  echo "not deploying a Dash0 monitoring resource"
  echo
fi

finish_scenario
