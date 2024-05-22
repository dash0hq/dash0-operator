#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail


cd "$(dirname ${BASH_SOURCE})"/../..

target_namespace=${1:-default}

kubectl delete -n ${target_namespace} -k config/samples || true
make uninstall || true
make undeploy || true
test-resources/node.js/express/undeploy.sh ${target_namespace}
test-resources/collector/undeploy.sh ${target_namespace}