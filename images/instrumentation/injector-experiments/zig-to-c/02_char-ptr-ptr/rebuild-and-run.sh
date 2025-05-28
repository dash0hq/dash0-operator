#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

echo building zig
zig build
echo zig build successful
echo building C
make
echo C build successful

echo running code
ulimit -c unlimited
LD_PRELOAD=./zig-out/libsymbols.so ./app.o
