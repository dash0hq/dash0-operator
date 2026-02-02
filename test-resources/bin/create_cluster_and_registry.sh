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

create_cluster_and_registry() {
  local kind_cluster="${DASH0_KIND_CLUSTER:-$default_kind_cluster}"
  local kind_config="${DASH0_KIND_CONFIG:-$default_kind_config}"

  start_kind "$kind_cluster" "$kind_config"
  create_registry "$local_reg_name" "$local_reg_port" "$LOCAL_REGISTRY_VOLUME_PATH"
  add_registry_config_to_nodes "$kind_cluster" "$local_reg_name" "$local_reg_port"
  connect_kind_network "$local_reg_name"
}

create_cluster_and_registry
