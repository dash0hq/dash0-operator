#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

jdk_sources=/home/dash0/instrumentation/injector-experiments/third-party/jdk

# Use installed JDK binaries:
jdk_binaries_java=java
# jdk_binaries_jshell=jshell

# Use locally built JDK binaries:
# jdk_binaries_java="$jdk_sources"/build/linux-aarch64-server-release/images/jdk/bin/java
# jdk_binaries_jshell="$jdk_sources"/build/linux-aarch64-server-release/images/jdk/bin/jshell

if [[ "$jdk_binaries_java" =~ "/build/" ]]; then
  echo "Using locally built JDK binaries, rebuiding JDK"
  pushd "$jdk_sources" > /dev/null
  # bash configure
  time make images
  popd > /dev/null
fi

echo building zig
zig build
echo zig build successful

echo starting JVM
ulimit -c unlimited
set -x
LD_PRELOAD=./zig-out/libsymbols.so $jdk_binaries_java -version
