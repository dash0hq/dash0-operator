#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

target_namespace=${1:-test-namespace}
delete_namespace=${2:-true}

kubectl delete -n ${target_namespace} -k config/samples || true

make undeploy-via-helm || true

kubectl delete secret \
  --namespace dash0-system \
  dash0-authorization-secret \
  --ignore-not-found

# If the custom resource definition has been installed by kustomize and the next test scenario attempts to install it
# via helm, the helm installation will fail because the custom resource definition already exists and does not have the
# "app.kubernetes.io/managed-by: Helm" label. Thus we always remove the CRD explictly and assume the next test scenario
# will install it again.
make uninstall || true

resource_types=( cronjob daemonset deployment job pod replicaset statefulset )
for resource_type in "${resource_types[@]}"; do
  test-resources/node.js/express/undeploy.sh ${target_namespace} ${resource_type}
done

test-resources/collector/undeploy.sh ${target_namespace}

if [[ "${target_namespace}" != "default" ]] && [[ "${delete_namespace}" == "true" ]]; then
  kubectl delete ns ${target_namespace} --ignore-not-found
fi
