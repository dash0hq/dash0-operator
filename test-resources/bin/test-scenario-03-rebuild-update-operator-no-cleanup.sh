#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

operator_namespace="${OPERATOR_NAMESPACE:-$default_operator_ns}"
target_namespace="${1:-$default_target_ns}"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file
verify_kubectx
setup_test_environment

step_counter=1

echo "STEP $step_counter: rebuild images"
build_all_images
finish_step

echo "STEP $step_counter: push images"
push_all_images
finish_step

echo "STEP $step_counter: update the Dash0 operator using helm"
update_via_helm
finish_step

finish_scenario
