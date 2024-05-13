#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

rm -f dash0-opentelemetry-*.tgz
pushd ../../../../opentelemetry-js-distro > /dev/null
bin/pack.sh
popd > /dev/null
cp ../../../../opentelemetry-js-distro/dash0-opentelemetry-*.tgz .

NPM_CONFIG_UPDATE_NOTIFIER=false \
  npm install \
  --package-lock-only \
  --ignore-scripts \
  --omit=dev \
  --no-audit \
  --no-fund=true \
  dash0-opentelemetry-*.tgz
