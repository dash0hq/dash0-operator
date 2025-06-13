#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

target_namespace=${1:-test-namespace}

source test-resources/bin/util
load_env_file
verify_kubectx
setup_test_environment

step_counter=1

echo "STEP $step_counter: rebuild images"
build_all_images
finish_step

echo "STEP $step_counter: deploy the Dash0 operator using helm"
update_via_helm
finish_step

finish_scenario

