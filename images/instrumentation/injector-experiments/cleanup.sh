#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

find . -type d -name .zig-cache -not -path "./third-party/*" -exec rm -rf {} \;
find . -type d -name zig-out -not -path "./third-party/*" -exec rm -rf {} \;
find . -type f -name \*.so -not -path "./third-party/*" -delete
find . -type f -name \*.o -not -path "./third-party/*" -delete
find . -type f -name core -not -path "./third-party/*" -delete

