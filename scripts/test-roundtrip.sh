#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/..

echo Make sure cert-manager and collector are deployed as well:
echo test-resources/cert-manager/deploy.sh
echo test-resources/collector/deploy.sh

scripts/test-cleanup.sh

make docker-build
test-resources/node.js/express/build-and-deploy.sh
make install
kubectl apply -k config/samples
make deploy
