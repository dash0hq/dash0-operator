#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

target_namespace=${1:-default}

if [[ -z ${SKIP_DOCKER_BUILD:-} ]]; then
  docker build . -t dash0-operator-nodejs-20-express-test-app
fi

./undeploy.sh ${target_namespace}
kubectl apply -n ${target_namespace} -f deploy.yaml
