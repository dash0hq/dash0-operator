#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

echo building zig
zig build
echo zig build

echo
echo accessing specific env vars:
LD_PRELOAD=./libsymbols.so node -e 'console.log("PATH:", process.env.PATH); console.log("VAR3:", process.env.VAR3); console.log("VAR4:", process.env.VAR4);'

# !! This segfaults:
echo
echo accessing raw process.env segfaults:
LD_PRELOAD=./libsymbols.so node -e 'console.log(process.env);'
