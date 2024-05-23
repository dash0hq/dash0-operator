#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

target_namespace=${1:-default}

kubectl delete -n ${target_namespace} -k config/samples || true
make uninstall || true
make undeploy || true

resource_types=( cronjob daemonset deployment job replicaset statefulset )
for resource_type in "${resource_types[@]}"; do
  test-resources/node.js/express/undeploy.sh ${target_namespace} ${resource_type}
done

test-resources/collector/undeploy.sh ${target_namespace}

if [[ "${target_namespace}" != "default" ]]; then
  kubectl delete ns ${target_namespace}
fi
