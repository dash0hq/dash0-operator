#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

target_namespace=${1:-default}
kind=${2:-deployment}

if [[ -z ${SKIP_DOCKER_BUILD:-} ]]; then
  docker build . -t dash0-operator-nodejs-20-express-test-app
fi

if [[ -f ${kind}.yaml ]]; then
  ../../bin/render-templates.sh manual-testing
fi

./undeploy.sh ${target_namespace} ${kind}
kubectl apply -n ${target_namespace} -f ${kind}.yaml
