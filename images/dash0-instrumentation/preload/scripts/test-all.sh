#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

cd "$(dirname "$0")"/..

scripts/build-in-container.sh

ARCH=arm64 LIBC=glibc scripts/run-tests-in-container.sh
ARCH=x86_64 LIBC=glibc scripts/run-tests-in-container.sh
ARCH=arm64 LIBC=musl scripts/run-tests-in-container.sh
ARCH=x86_64 LIBC=musl scripts/run-tests-in-container.sh

