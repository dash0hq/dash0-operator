#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

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
