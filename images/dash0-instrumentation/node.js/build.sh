#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

rm -rf node_modules
pushd ../../../../opentelemetry-js-distro > /dev/null
bin/pack.sh
popd > /dev/null
npm i --no-save ../../../../opentelemetry-js-distro/dash0-opentelemetry-*.tgz
