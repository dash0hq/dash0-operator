#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

injector_zig_files=$(fd .zig ../injector)
test_files=$(fd -t f --exclude node_modules --exclude '*.o' --exclude '*.c' --exclude '*.class' --exclude '*.swp' --exclude '*.iml')

printf "$injector_zig_files\n$test_files" | entr npm run test-within-container
