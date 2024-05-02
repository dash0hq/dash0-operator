#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

pushd node.js > /dev/null
./build.sh
popd > /dev/null

docker build . -t dash0-instrumentation:1.0.0
