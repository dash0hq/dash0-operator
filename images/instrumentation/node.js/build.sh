#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

if [[ "${USE_LOCAL_SOURCES_FOR_NODEJS_DISTRIBUTION:-}" = "true" ]]; then
  echo "Node.js: using the local sources for @dash0/opentelemetry"
  rm -f dash0-opentelemetry-*.tgz
  pushd ../../../../opentelemetry-js-distribution > /dev/null
  npm pack
  popd > /dev/null
  cp ../../../../opentelemetry-js-distribution/dash0-opentelemetry-*.tgz .

  NPM_CONFIG_UPDATE_NOTIFIER=false \
    npm install \
    --package-lock-only \
    --ignore-scripts \
    --omit=dev \
    --no-audit \
    --no-fund=true \
    dash0-opentelemetry-*.tgz
else
  rm -f dash0-opentelemetry-*.tgz
  rm -rf node_modules
fi
