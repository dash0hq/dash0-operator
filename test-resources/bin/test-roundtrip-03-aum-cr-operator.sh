#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

if ! kubectl get ns cert-manager &> /dev/null; then
  echo cert-manager namespace not found, deploying cert-manager
  test-resources/cert-manager/deploy.sh
fi

target_namespace=${1:-test-namespace}
kind=${2:-deployment}

# Installing the CRD separately from the operator (which is what this scenario is about) is currently only supported
# via kustomize, so deployment via helm is not possible for this scenario. (The scenario is also somewhat contrived,
# and not very relevant in practice.)
deployment_tool=kustomize

test-resources/bin/render-templates.sh manual-testing

echo "STEP 1: creating target namespace (if necessary)"
test-resources/bin/ensure-namespace-exists.sh ${target_namespace}

echo "STEP 2: remove old test resources"
USE_KUSTOMIZE=true test-resources/bin/test-cleanup.sh ${target_namespace} false
echo
echo

echo "STEP 3: deploy the collector to namespace ${target_namespace}"
test-resources/collector/deploy.sh ${target_namespace}
echo
echo

echo "STEP 4: rebuild the operator image"
make docker-build
echo
echo

echo "STEP 5: deploy application under monitoring"
test-resources/node.js/express/deploy.sh ${target_namespace} ${kind}
echo
echo

# For this sequence of events (deploying the custom resource to the target namespace before actually deploying the
# operator), we need to explicitly install the custom resource definition via make install. In other scenarios, this
# is not required, as the operator installation will also include the custom resource definition, both for kustomize
# and helm as well.
echo "STEP 6: install the custom resource definition"
make install
echo
echo

sleep 5

echo "STEP 7: deploy the Dash0 custom resource to namespace ${target_namespace}"
kubectl apply -n ${target_namespace} -k config/samples
echo
echo

sleep 5

echo "STEP 8: deploy the Dash0 operator (using ${deployment_tool})"
make deploy-via-${deployment_tool}
