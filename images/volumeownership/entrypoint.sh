#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# See README.md for an explanation of this script and its purpose.

# MAINTENANCE NOTE: THIS SCRIPT *DELIBERATELY* DOES NOT RUN WITH set -e! See README.md for more details.
set -x

for dir in "$@"; do
  /bin/mkdir -p "$dir"
  /bin/chown -R 65532:0 "$dir"

  # log directory ownership and permission information for troubleshooting purposes
  ls -lad "$dir"
  ls -la "$dir/"
done
