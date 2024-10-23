#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

command -v act >/dev/null 2>&1 || {
  cat <<EOF >&2
The executable act needs to be installed but it isn't.
See https://nektosact.com/installation/index.html for instructions.

Aborting.
EOF
  exit 1
}

workflow="${1:-.github/workflows/ci.yaml}"
job="${2:-verify}"
trigger="${3:-push}"

set -x
act \
  --container-architecture linux/arm64 \
  -W "$workflow" \
  -j "$job" \
  "$trigger"
set +x

