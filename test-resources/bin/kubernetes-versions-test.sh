#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail
set -x

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..

cd "$project_root"

all_versions=(
  # v1.20.2  # not tested
  # v1.20.15 # fails, this K8s version does not have cronjobs
  v1.21.14
  v1.22.17
  v1.23.17
  v1.24.17
  v1.25.16
  v1.26.15
  v1.27.16
  v1.28.15
  v1.29.10
  v1.30.6
  v1.31.2
)

echo "NOTE: Make sure cloud-provider-kind is running, otherwise tests will fail!"

run_tests_on_kubernetes_version () {
  local kubernetes_version="${1:-}"
  if [[ -z "$kubernetes_version" ]]; then
    echo Missing mandatory argument: Kubernetes Version
    exit 1
  fi

  local cluster_name="dash0-operator-playground-$kubernetes_version"
  kind create cluster \
    --image "kindest/node:$kubernetes_version" \
    --name "$cluster_name" \
    --config test-resources/kind-config.yaml

  # Install a trap to make sure we remove the cluster, even if the e2e tests fail.
  trap "{ kind delete cluster --name ""$cluster_name"" ; exit 255; }" EXIT

  # Give the cluster some time to come up, and for cloud-provider-kind to do its thing.
  sleep 20

  E2E_KUBECTX="kind-$cluster_name" make test-e2e

  # The e2e tests have been successful for this Kubernetes version, remove the bash trap installed above, then delete
  # the cluster directly.
  trap - EXIT
  kind delete cluster --name "$cluster_name"
}


for v in "${all_versions[@]}"; do
  run_tests_on_kubernetes_version "$v"
done
