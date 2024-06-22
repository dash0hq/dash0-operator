#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

cd "$(dirname "$0")"

ARCH=arm64 LIBC=glibc ./build-and-test.sh
ARCH=x86_64 LIBC=glibc ./build-and-test.sh
ARCH=arm64 LIBC=musl ./build-and-test.sh
ARCH=x86_64 LIBC=musl ./build-and-test.sh

