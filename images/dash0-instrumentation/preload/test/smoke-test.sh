#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

relative_directory="$(dirname "$0")"/..
directory="$(realpath "$relative_directory")"
cd "$directory"

if [ -z "${EXPECTED_CPU_ARCHITECTURE:-}" ]; then
  echo "EXPECTED_CPU_ARCHITECTURE is not set for $0."
  exit 1
fi

arch_output=$(uname -m)
arch_exit_code=$?
if [ $arch_exit_code != 0 ]; then
  printf "${RED}verifying CPU architecture failed:${NC}\n"
  echo "exit code: $arch_exit_code"
  echo "output: $arch_output"
  exit 1
elif [ "$arch_output" != "$EXPECTED_CPU_ARCHITECTURE" ]; then
  printf "${RED}verifying CPU architecture failed:${NC}\n"
  echo "expected: $EXPECTED_CPU_ARCHITECTURE"
  echo "actual:   $arch_output"
  exit 1
else
  printf "${GREEN}verifying CPU architecture %s successful${NC}\n" "$EXPECTED_CPU_ARCHITECTURE"
fi

run_test_case() {
  test_case=$1
  command=$2
  expected=$3
  existing_node_options_value=${4:-}
  set +e
  if [ "$existing_node_options_value" != "" ]; then
    test_output=$(LD_PRELOAD="$directory/lib/libdash0envhook.so" NODE_OPTIONS="$existing_node_options_value" ./testbin/appundertest.so "$command")
  else
    test_output=$(LD_PRELOAD="$directory/lib/libdash0envhook.so" ./testbin/appundertest.so "$command")
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

make clean
make

exit_code=0

run_test_case "getenv: returns NULL for non-existing environment variable" non-existing "DOES_NOT_EXIST: NULL"
run_test_case "getenv: returns environment variable unchanged" term "TERM: xterm"
run_test_case "getenv: overrides NODE_OPTIONS if it is not present" node_options "NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js"
run_test_case "getenv: ask for NODE_OPTIONS (unset) twice" node_options_twice "NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js; NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js"
run_test_case "getenv: appends to NODE_OPTIONS if it is present" node_options "NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --existing-node-options" "--existing-node-options"
run_test_case "getenv: ask for NODE_OPTIONS (set) twice" node_options_twice "NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --existing-node-options; NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --existing-node-options" "--existing-node-options"

run_test_case "secure_getenv: returns NULL for non-existing environment variable" non-existing "DOES_NOT_EXIST: NULL"
run_test_case "secure_getenv: returns environment variable unchanged" term-gnu-secure "TERM: xterm"
run_test_case "secure_getenv: overrides NODE_OPTIONS if it is not present" node_options-gnu-secure "NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js"
run_test_case "secure_getenv: ask for NODE_OPTIONS (unset) twice" node_options_twice-gnu-secure "NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js; NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js"
run_test_case "secure_getenv: appends to NODE_OPTIONS if it is present" node_options-gnu-secure "NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --existing-node-options" "--existing-node-options"
run_test_case "secure_getenv: ask for NODE_OPTIONS (set) twice" node_options_twice-gnu-secure "NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --existing-node-options; NODE_OPTIONS: --require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --existing-node-options" "--existing-node-options"

exit $exit_code
