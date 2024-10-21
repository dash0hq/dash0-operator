#!/usr/bin/env sh
# shellcheck disable=SC2059

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

if [ -z "${EXPECTED_CPU_ARCHITECTURE:-}" ]; then
  echo "EXPECTED_CPU_ARCHITECTURE is not set for $0."
  exit 1
fi

arch=$(uname -m)
arch_exit_code=$?
if [ $arch_exit_code != 0 ]; then
  printf "${RED}verifying CPU architecture failed:${NC}\n"
  echo "exit code: $arch_exit_code"
  echo "output: $arch"
  exit 1
elif [ "$arch" != "$EXPECTED_CPU_ARCHITECTURE" ]; then
  printf "${RED}verifying CPU architecture failed:${NC}\n"
  echo "expected: $EXPECTED_CPU_ARCHITECTURE"
  echo "actual:   $arch"
  exit 1
else
  printf "${GREEN}verifying CPU architecture %s successful${NC}\n" "$EXPECTED_CPU_ARCHITECTURE"
fi

injector_binary=/dash0-init-container/injector/dash0_injector.so
if [ ! -f $injector_binary ]; then
  printf "${RED}error: %s does not exist, not running any tests.${NC}\n" "$injector_binary"
  exit 1
fi

run_test_case() {
  test_case=$1
  command=$2
  expected=$3
  existing_node_options_value=${4:-}
  set +e
  if [ "$existing_node_options_value" != "" ]; then
    test_output=$(LD_PRELOAD="$injector_binary" NODE_OPTIONS="$existing_node_options_value" node index.js "$command")
  else
    test_output=$(LD_PRELOAD="$injector_binary" node index.js "$command")
  fi
  test_exit_code=$?
  set -e
  if [ $test_exit_code != 0 ]; then
    printf "${RED}test \"%s\" crashed:${NC}\n" "$test_case"
    echo "received exit code: $test_exit_code"
    echo "output: $test_output"
    exit_code=1
  elif [ "$test_output" != "$expected" ]; then
    printf "${RED}test \"%s\" failed:${NC}\n" "$test_case"
    echo "expected: $expected"
    echo "actual:   $test_output"
    exit_code=1
  else
    printf "${GREEN}test \"%s\" successful${NC}\n" "$test_case"
  fi
}

exit_code=0

cd app

run_test_case "getenv: returns undefined for non-existing environment variable" non-existing "DOES_NOT_EXIST: -"
run_test_case "getenv: returns environment variable unchanged" term "TERM: xterm"
run_test_case "getenv: overrides NODE_OPTIONS if it is not present" node_options "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
run_test_case "getenv: ask for NODE_OPTIONS (unset) twice" node_options_twice "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry; NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
run_test_case "getenv: prepends to NODE_OPTIONS if it is present" node_options "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --no-deprecation" "--no-deprecation"
run_test_case "getenv: ask for NODE_OPTIONS (set) twice" node_options_twice "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --no-deprecation; NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --no-deprecation" "--no-deprecation"

exit $exit_code

