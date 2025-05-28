#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

node_sources=/home/dash0/instrumentation/injector-experiments/third-party/node

# Use installed Node.js binary:
node_js=node

# Use locally built Node.js binary (release build):
# node_js="$node_sources"/out/Release/node
# Use locally built Node.js binary (debug build):
# node_js="$node_sources"/out/Debug/node

if [[ "$node_js" =~ "/out/" ]]; then
  echo "Using locally built Node.js binary, rebuiding Node.js"
  pushd "$node_sources" > /dev/null
  # ./configure
  # ./configure --debug
  # Building the whole Node.js code base in the container was somewhat finicky, building with just one parallel job
  # (no -j) was abysmally slow, but running with -j4 crashed in linking steps due to out of memory (I believe):
  #   LINK(target) /home/dash0/instrumentation/injector-experiments/third-party/node/out/Debug/node_mksnapshot
  #   collect2: fatal error: ld terminated with signal 9 [Killed]
  #   compilation terminated.
  # A good compromise might be to run an initial build with -j4, then finish up with -j2 or without -j.
  # Giving the Docker VM more memory might help.
  time make -j8
  popd > /dev/null
fi

echo building zig
zig build
echo zig build successful

echo starting Node.js
ulimit -c unlimited
set -x
LD_PRELOAD=./zig-out/libsymbols.so "$node_js" script.js
