#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

if [[ -z ${SKIP_DOCKER_BUILD:-} ]]; then
  docker build . -t dash0-operator-nodejs-20-express-test-app
fi

kubectl delete -f deploy.yaml || true
kubectl apply -f deploy.yaml
