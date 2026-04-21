#!/usr/bin/env bash

set -euo pipefail

if [[ -z "${DASH0_API_ENDPOINT:-}" ]]; then
  echo "DASH0_API_ENDPOINT is not set (e.g. https://api.europe-west4.gcp.dash0-dev.com)"
  exit 1
fi
if [[ -z "${DASH0_AUTHORIZATION_TOKEN:-}" ]]; then
  echo "DASH0_AUTHORIZATION_TOKEN is not set"
  exit 1
fi

dataset="${OPERATOR_CONFIGURATION_VIA_HELM_DATASET:-${DASH0_DATASET:-default}}"
base_url="${DASH0_API_ENDPOINT%/}/api/sampling-rules"

echo "Fetching sampling rules from ${base_url} (dataset=${dataset})..."

response=$(curl -s -H "Authorization: Bearer ${DASH0_AUTHORIZATION_TOKEN}" "${base_url}?dataset=${dataset}")

ids=$(echo "${response}" | jq -r '.samplingRules[]?.metadata.labels["dash0.com/id"] // empty')

if [[ -z "${ids}" ]]; then
  echo "No sampling rules found."
  exit 0
fi

count=$(echo "${ids}" | wc -l)
echo "Found ${count} sampling rule(s). Deleting..."

for id in ${ids}; do
  echo "  Deleting ${id}..."
  curl -s -o /dev/null -w "  HTTP %{http_code}\n" \
    -X DELETE \
    -H "Authorization: Bearer ${DASH0_AUTHORIZATION_TOKEN}" \
    "${base_url}/${id}?dataset=${dataset}"
done

echo "Done."
