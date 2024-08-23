#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

target_namespace=${1:-test-namespace}
kind=${2:-deployment}

source test-resources/bin/util
load_env_file
verify_kubectx
setup_test_environment

echo "STEP 1: creating target namespace (if necessary)"
test-resources/bin/ensure-namespace-exists.sh ${target_namespace}
echo
echo

echo "STEP 2: remove old test resources"
test-resources/bin/test-cleanup.sh ${target_namespace} false
echo
echo

echo "STEP 3: creating operator namespace and authorization token secret"
test-resources/bin/ensure-namespace-exists.sh dash0-system
kubectl create secret \
  generic \
  dash0-authorization-secret \
  --namespace dash0-system \
  --from-literal=token="${DASH0_AUTHORIZATION_TOKEN}"
echo
echo

echo "STEP 4: rebuild images"
build_all_images
echo
echo

echo "STEP 5: deploy application under monitoring"
test-resources/node.js/express/deploy.sh ${target_namespace} ${kind}
echo
echo

echo "STEP 6: deploy the Dash0 operator using helm"
deploy_via_helm
echo
echo

sleep 5

echo "STEP 7: deploy the Dash0 monitoring resource to namespace ${target_namespace}"
kubectl apply -n ${target_namespace} -f test-resources/customresources/dash0monitoring/dash0monitoring.secret.yaml

