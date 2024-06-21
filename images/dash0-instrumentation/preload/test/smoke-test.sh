#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

relative_directory="$(dirname ${BASH_SOURCE})"/..
directory="$(realpath $relative_directory)"
cd "$directory"

run_test_case() {
  local test_case=$1
  local command=$2
  local expected=$3
  local existing_node_options_value=${4:-}
  set +xe
  if [[ "$existing_node_options_value" != "" ]]; then
    local test_output=$(LD_PRELOAD="$directory/lib/libdash0envhook.so" NODE_OPTIONS="$existing_node_options_value" ./testbin/appundertest.so "$command")
  else
    local test_output=$(LD_PRELOAD="$directory/lib/libdash0envhook.so" ./testbin/appundertest.so "$command")
  fi
  local test_exit_code=$?
  if [[ $test_exit_code != 0 ]]; then
    printf "${RED}test \"$test_case\" crashed:${NC}\n"
    echo "received exit code: $test_exit_code"
    echo "output: $test_output"
    exit_code=1
  elif [[ "$test_output" != "$expected" ]]; then
    printf "${RED}test \"$test_case\" failed:${NC}\n"
    echo "expected: $expected"
    echo "actual:   $test_output"
    exit_code=1
  else
    printf "${GREEN}test \"$test_case\" successful${NC}\n"
  fi
}

make

exit_code=0

run_test_case "getenv: returns NULL for non-existing environment variable" non-existing "DOES_NOT_EXIST: NULL"
run_test_case "getenv: returns environment variable unchanged" term "TERM: xterm"
run_test_case "getenv: overrides NODE_OPTIONS if it is not present" node_options "NODE_OPTIONS: --require /dash0"
run_test_case "getenv: appends to NODE_OPTIONS if it is present" node_options "NODE_OPTIONS: --require /dash0 --existing-node-options" "--existing-node-options"

run_test_case "secure_getenv: returns NULL for non-existing environment variable" non-existing "DOES_NOT_EXIST: NULL"
run_test_case "secure_getenv: returns environment variable unchanged" term-gnu-secure "TERM: xterm"
run_test_case "secure_getenv: overrides NODE_OPTIONS if it is not present" node_options-gnu-secure "NODE_OPTIONS: --require /dash0"
run_test_case "secure_getenv: appends to NODE_OPTIONS if it is present" node_options-gnu-secure "NODE_OPTIONS: --require /dash0 --existing-node-options" "--existing-node-options"

exit $exit_code

