#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

source test-resources/bin/util
load_env_file

resource_types=( cronjob daemonset deployment job pod replicaset statefulset )

for resource_type in "${resource_types[@]}"; do
  cat test-resources/node.js/express/${resource_type}.yaml.template | envsubst > test-resources/node.js/express/${resource_type}.yaml
done

cat test-resources/customresources/dash0monitoring/dash0monitoring.token.yaml.template | DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" envsubst > test-resources/customresources/dash0monitoring/dash0monitoring.token.yaml
