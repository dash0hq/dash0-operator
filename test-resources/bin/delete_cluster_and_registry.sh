#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"
# shellcheck source=./lib/kind
source "$scripts_lib/kind"
# shellcheck source=./lib/registry
source "$scripts_lib/registry"
# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file

delete_cluster_and_registry() {
  local kind_cluster="${DASH0_KIND_CLUSTER:-$default_kind_cluster}"
  delete_cluster "$kind_cluster"
  delete_registry "$local_reg_name"
}

delete_cluster_and_registry
