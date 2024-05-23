#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

resource_types=( cronjob daemonset deployment job replicaset statefulset )

for resource_type in "${resource_types[@]}"; do
  cat test-resources/node.js/express/${resource_type}.yaml.template | envsubst > test-resources/node.js/express/${resource_type}.yaml
done

if [[ ${1:-} == manual-testing ]]; then
  # Generate collector values file for manual testing:
  if [[ -n ${DASH0_AUTHORIZATION_TOKEN:-} ]] && [[ -n ${OTEL_EXPORTER_OTLP_ENDPOINT:-} ]] then
      echo "Using DASH0_AUTHORIZATION_TOKEN and OTEL_EXPORTER_OTLP_ENDPOINT to render the template test-resources/collector/manual.values.yaml.template. The rendered file test-resources/collector/manual.values.yaml can be used for manual testing and actually reporting data to Dash0."
      cat test-resources/collector/manual.values.yaml.template | envsubst > test-resources/collector/manual.values.yaml
  elif [[ -f test-resources/collector/manual.values.yaml ]]; then
    echo "The file test-resources/collector/manual.values.yaml already exists, the file will be reused. Set DASH0_AUTHORIZATION_TOKEN and OTEL_EXPORTER_OTLP_ENDPOINT to generate a new file."
  else
    echo "The file test-resources/collector/manual.values.yaml does not exist and either DASH0_AUTHORIZATION_TOKEN or OTEL_EXPORTER_OTLP_ENDPOINT are not set (or both). Both environment variables are required when running this script for the first time, to generate a valid manual.values.yaml file for the OpenTelemetry collector. They can be omitted on subsequent runs, unless test-resources/collector/manual.values.yaml is deleted and needs to be regenerated."
    exit 1
  fi
else
  # Generate collector values file for end-to-end tests:
  cat test-resources/collector/e2e.values.yaml.template | envsubst > test-resources/collector/e2e.values.yaml
fi
