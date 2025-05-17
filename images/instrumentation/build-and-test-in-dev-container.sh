#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

pushd injector > /dev/null
echo building injector
zig build
echo injector build successful
popd > /dev/null

pushd test/c > /dev/null
make clean
make build
popd > /dev/null

LD_PRELOAD=injector/dash0_injector.so test/c/test-cases/test/app.o
