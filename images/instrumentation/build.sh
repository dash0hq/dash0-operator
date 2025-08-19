#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Note: By default, this script is used neither in the e2e tests nor in the semi-manual test scenarios in
# test-resources/bin, nor in the CI/CD pipeline. It might be outdated. The only use case for this script is when
# you want to build the instrumentation image locally while also using modified sources for the Dash0 Nodes.js distro.
# (See images/instrumentation/node.js/build.sh, USE_LOCAL_SOURCES_FOR_NODEJS_DISTRIBUTION).

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

pushd node.js > /dev/null
./build.sh
popd > /dev/null

image_repository=instrumentation
image_version=latest

if [[ -n "${1:-}" ]]; then
  image_repository=$1
fi
if [[ -n "${2:-}" ]]; then
  image_version=$2
fi

docker build . -t "$image_repository":"$image_version"
