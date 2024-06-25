#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

if [[ "${USE_LOCAL_SOURCES_FOR_NODEJS_DISTRIBUTION:-}" = true ]]; then
  echo "Node.js: using the local sources for @dash0hq/opentelemetry"
  rm -f dash0hq-opentelemetry-*.tgz
  pushd ../../../../opentelemetry-js-distribution > /dev/null
  npm pack
  popd > /dev/null
  cp ../../../../opentelemetry-js-distribution/dash0hq-opentelemetry-*.tgz .

  NPM_CONFIG_UPDATE_NOTIFIER=false \
    npm install \
    --package-lock-only \
    --ignore-scripts \
    --omit=dev \
    --no-audit \
    --no-fund=true \
    dash0hq-opentelemetry-*.tgz

else
  nodejs_distribution_version="1.0.1"
  fully_qualified_package_name="@dash0hq/opentelemetry@${nodejs_distribution_version}"
  rm -f dash0hq-opentelemetry-*.tgz
  echo "Node.js: using @dash0hq/opentelemetry@${nodejs_distribution_version}"

  NPM_CONFIG_UPDATE_NOTIFIER=false \
  npm install \
  --save-exact \
  --package-lock-only \
  --ignore-scripts \
  --omit=dev \
  --no-audit \
  --no-fund=true \
  "$fully_qualified_package_name"
fi
