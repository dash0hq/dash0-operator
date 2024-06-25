#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

pushd node.js > /dev/null
./build.sh
popd > /dev/null

image_repository=instrumentation
image_version=latest

if [[ -n "${1:-}" ]]; then
  image_repository=$1
fi
if [[ -n "${1:-}" ]]; then
  image_version=$2
fi

docker build . -t "$image_repository":"$image_version"
