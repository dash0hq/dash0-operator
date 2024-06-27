#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

resource_types=( cronjob daemonset deployment job pod replicaset statefulset )

for resource_type in "${resource_types[@]}"; do
  cat test-resources/node.js/express/${resource_type}.yaml.template | envsubst > test-resources/node.js/express/${resource_type}.yaml
done

cat test-resources/helm/manual.values.yaml.template | envsubst > test-resources/helm/manual.values.yaml

cat test-resources/helm/e2e.values.yaml.template | envsubst > test-resources/helm/e2e.values.yaml

