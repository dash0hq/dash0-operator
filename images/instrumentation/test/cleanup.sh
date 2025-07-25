#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

find . -type f -name \*.o -delete
find . -type f -name \*.class -delete
find . -type f -name \*.jar -delete

