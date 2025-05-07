#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A small helper script intended to be used with entr, like so:
# cd images/instrumentation/injector
# fd | entr ./zig-build.sh
# ...to get fast feedback on compile errors when working on the Zig code.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

if zig build; then
  echo "$(date) build successful"
  echo
else
  echo "$(date) build failed"
  echo
fi
