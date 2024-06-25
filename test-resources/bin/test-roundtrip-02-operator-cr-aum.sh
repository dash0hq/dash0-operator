#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

target_namespace=${1:-test-namespace}
kind=${2:-deployment}
deployment_tool=helm
if [[ -n "${USE_KUSTOMIZE:-}" ]]; then
  deployment_tool=kustomize
fi

source test-resources/bin/util
setup_test_environment $deployment_tool

echo "STEP 1: creating target namespace (if necessary)"
test-resources/bin/ensure-namespace-exists.sh ${target_namespace}

echo "STEP 2: remove old test resources"
test-resources/bin/test-cleanup.sh ${target_namespace} false
echo
echo

if [[ "${deployment_tool}" != "helm" ]]; then
  echo "STEP 3: deploy the collector to namespace dash0-operator-system"
  test-resources/collector/deploy.sh dash0-operator-system
else
  echo "STEP 3: skipping collector deployment, the collector will be deployed by the operator helm chart"
fi
echo
echo

echo "STEP 4: rebuild the instrumentation image"
images/instrumentation/build.sh instrumentation latest
echo
echo

echo "STEP 5: rebuild the operator image"
make docker-build
echo
echo

echo "STEP 6: deploy the Dash0 operator (using ${deployment_tool})"
make deploy-via-${deployment_tool}
echo
echo

sleep 5

echo "STEP 7: deploy the Dash0 custom resource to namespace ${target_namespace}"
kubectl apply -n ${target_namespace} -k config/samples
echo
echo

sleep 5

echo "STEP 8: deploy application under monitoring"
test-resources/node.js/express/deploy.sh ${target_namespace} ${kind}
