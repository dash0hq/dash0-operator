#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

echo Make sure cert-manager is deployed as well:
echo test-resources/cert-manager/deploy.sh

echo STEP 1: remove old test resources
test-resources/bin/test-cleanup.sh
echo
echo

echo STEP 2: deploy the collector
test-resources/collector/deploy.sh
echo
echo

echo STEP 3: rebuild the operator image
make docker-build
echo
echo

echo STEP 4: deploy application under monitoring
test-resources/node.js/express/build-and-deploy.sh
echo
echo

echo STEP 5: install the custom resource definition
make install
echo
echo

sleep 5

echo STEP 6: deploy the Dash0 operator
make deploy
echo
echo

sleep 5

echo STEP 7: deploy the Dash0 custom resource to the target namespace
kubectl apply -k config/samples
