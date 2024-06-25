#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

pushd node.js > /dev/null
./build.sh
popd > /dev/null

image_version=1.0.0

docker build . -t dash0-instrumentation:"$image_version"
