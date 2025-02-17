#!/usr/bin/env sh
# shellcheck disable=SC2059

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo noglob

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

# Runs one test case. Usage:
#
#   run_test_case $test_case_label $test_app_command $expected_output $env_vars
#
# - test_case_label: a human readable phrase describing the test case
# - test_app_command: will be passed on to app/index.js as a command line argument and determines the app's behavior
# - expected_output: The app's output will be compared to this string, the test case is deemed successful if the exit
#   code is zero and the app's output matches this string
# - env_vars (optional): Set additional environment variables like NODE_OPTIONS or OTEL_RESOURCE_ATTRIBUTES when running
#   the test
run_test_case() {
  test_case_label=$1
  test_app_command=$2
  expected=$3
  env_vars=${4:-}

  if [ -n "${TEST_CASES:-}" ]; then
    IFS=,
    # shellcheck disable=SC2086
    set -- $TEST_CASES""

    run_this_test_case="false"
    for selected_test_case in "$@"; do
      set +e
      match=$(expr "$test_case_label" : ".*$selected_test_case.*")
      set -e
      if [ "$match" -gt 0 ]; then
        run_this_test_case="true"
      fi
    done
    if [ "$run_this_test_case" != "true" ]; then
      echo "- skipping test case \"$test_case_label\""
      return
    fi
  fi

  full_command="LD_PRELOAD=""$injector_binary"" TEST_VAR=value DASH0_NAMESPACE_NAME=my-namespace DASH0_POD_NAME=my-pod DASH0_POD_UID=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff DASH0_CONTAINER_NAME=test-app"
  if [ "$env_vars" != "" ]; then
    full_command=" $full_command $env_vars"
  fi
  full_command=" $full_command node index.js $test_app_command"
  echo "Running test command: $full_command"
  set +e
  test_output=$(eval "$full_command")
  test_exit_code=$?
  set -e
  if [ $test_exit_code != 0 ]; then
    printf "${RED}test \"%s\" crashed:${NC}\n" "$test_case_label"
    echo "received exit code: $test_exit_code"
    echo "output: $test_output"
    exit_code=1
  elif [ "$test_output" != "$expected" ]; then
    printf "${RED}test \"%s\" failed:${NC}\n" "$test_case_label"
    echo "expected: $expected"
    echo "actual:   $test_output"
    exit_code=1
  else
    printf "${GREEN}test \"%s\" successful${NC}\n" "$test_case_label"
  fi
}

exit_code=0

cd app

run_test_case "getenv: returns undefined for non-existing environment variable" non-existing "DOES_NOT_EXIST: -"
run_test_case "getenv: returns environment variable unchanged" existing "TEST_VAR: value"
run_test_case "getenv: overrides NODE_OPTIONS if it is not present" node_options "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
run_test_case "getenv: ask for NODE_OPTIONS (unset) twice" node_options_twice "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry; NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
run_test_case "getenv: prepends to NODE_OPTIONS if it is present" node_options "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --no-deprecation" "NODE_OPTIONS=--no-deprecation"
run_test_case "getenv: ask for NODE_OPTIONS (set) twice" node_options_twice "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --no-deprecation; NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --no-deprecation" "NODE_OPTIONS=--no-deprecation"
run_test_case "getenv: sets k8s.pod.uid and k8s.container.name via OTEL_RESOURCE_ATTRIBUTES" otel_resource_attributes "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app"
run_test_case "getenv: sets k8s.pod.uid and k8s.container.name via OTEL_RESOURCE_ATTRIBUTES with pre-existing value" otel_resource_attributes "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app,foo=bar" "OTEL_RESOURCE_ATTRIBUTES=foo=bar"
run_test_case "getenv: use mapped app.kubernetes.io labels for service.name and friends" otel_resource_attributes "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app,service.name=service-name,service.version=service-version,service.namespace=service-namespace" "DASH0_SERVICE_NAME=service-name DASH0_SERVICE_VERSION=service-version DASH0_SERVICE_NAMESPACE=service-namespace"
run_test_case "getenv: use mapped resource.opentelemetry.io labels as additional resource attributes" otel_resource_attributes "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app,aaa=bbb,ccc=ddd" "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd"
run_test_case "getenv: combine mapped app.kubernetes.io and resource.opentelemetry.io labels" otel_resource_attributes "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app,service.name=service-name,service.version=service-version,service.namespace=service-namespace,aaa=bbb,ccc=ddd" "DASH0_SERVICE_NAME=service-name DASH0_SERVICE_VERSION=service-version DASH0_SERVICE_NAMESPACE=service-namespace DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd"

exit $exit_code

