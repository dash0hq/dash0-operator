#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A small helper script intended to be used with entr, like so:
# cd images/instrumentation/injector
# fd | entr ./zig-test.sh
# ...to get fast feedback on test errors when working on the Zig code.
# The point is that `zig build test` does not print anything when there have been no changes since the last successful
# build, which makes it hard to tell (when using entr) whether the build is still ongoing or there simply was nothing
# to do. This script will simply print a message indicating the successful build to circumvent this.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

if zig build test --prominent-compile-errors --summary none; then
  echo "$(date) tests successful"
  echo
else
  echo "$(date) tests failed"
  echo
fi