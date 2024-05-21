#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/..

kubectl delete -k config/samples || true
make uninstall || true
make undeploy || true
test-resources/node.js/express/undeploy.sh
