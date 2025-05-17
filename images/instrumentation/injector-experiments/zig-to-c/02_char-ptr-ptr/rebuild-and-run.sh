#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

echo building zig
zig build
echo zig build
echo building C
make
echo C build successful

echo running code
LD_PRELOAD=./libsymbols.so ./app.o
