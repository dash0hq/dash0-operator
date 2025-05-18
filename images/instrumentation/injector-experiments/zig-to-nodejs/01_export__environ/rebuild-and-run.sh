#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

node_sources=/home/dash0/instrumentation/injector-experiments/third-party/node

# Use installed Node.js binary:
# node_js=node

# Use locally built Node.js binary (release build):
# node_js="$node_sources"/out/Release/node
# Use locally built Node.js binary (debug build):
node_js="$node_sources"/out/Debug/node

if [[ "$node_js" =~ "out/Release" ]]; then
  echo "Using locally built Node.js binary, rebuiding Node.js"
  pushd "$node_sources" > /dev/null
  ./configure --debug
  make -j4
  popd > /dev/null
fi

echo building zig
zig build
echo zig build

# $ gdb /opt/node-debug/node core.node.8.1535359906
# (gdb) backtrace

echo
set -x
# LD_PRELOAD=./libsymbols.so /home/dash0/instrumentation/injector-experiments/third-party/node/out/Release/node script.js
LD_PRELOAD=./libsymbols.so "$node_js" --no-node-snapshot script.js
