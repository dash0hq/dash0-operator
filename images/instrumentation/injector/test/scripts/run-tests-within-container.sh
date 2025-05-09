#!/usr/bin/env sh
# shellcheck disable=SC2059

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo noglob

home_directory=$(pwd)

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
#   run_test_case $test_case_label $working_dir $test_app_command $expected_output $env_vars
#
# - test_case_label: a human readable phrase describing the test case
# - working_dir: the working directory for the test case
# - test_app_command: the test app executable to run, plus (optionally) additional command line arguments that will be passed on to
#   the test app
# - expected_output: The app's output will be compared to this string, the test case is deemed successful if the exit
#   code is zero and the app's output matches this string
# - env_vars (optional): Set additional environment variables like NODE_OPTIONS or OTEL_RESOURCE_ATTRIBUTES when running
#   the test
# - sdks_exist: Whether to create dummy SDK files.
run_test_case() {
  test_case_label=$1
  working_dir=$2
  test_app_command=$3
  expected=$4
  env_vars=${5:-}
  sdks_exist=${6:-"true"}

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

  if [ "${test_case_label#*"__environ"}" != "$test_case_label" ] && [ "${ARCH_UNDER_TEST:-}" = "x86_64" ] && [ "${LIBC_UNDER_TEST:-}" = "musl" ]; then
    echo "- skipping test case \"$test_case_label\": tests for no __environ are currently disabled for x86_64/musl"
    return
  fi

  # Create a dummy OTel distro/SDK files which actually do nothing but pass the "file exists" check in the injector.
  # These directories and files will be owned by root:root while the application under test runs as user node.
  if [ "$sdks_exist" = "false" ]; then
    echo "- removing dummy OTel SDK files"
    sudo rm -rf /__dash0__/instrumentation
  else
    sudo rm -rf /__dash0__/instrumentation
    sudo mkdir -p /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry
    sudo touch    /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry/index.js
    sudo mkdir -p /__dash0__/instrumentation/jvm
    sudo touch    /__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar
    if [ "$sdks_exist" = "no_permissions" ]; then
      # Simulate a situation where the OTel SDK/distro exists but the user executing the proess does not have access.
      sudo chmod -R 600 /__dash0__/instrumentation
    fi
  fi

  cd "$working_dir"
  # Note: add DASH0_INJECTOR_DEBUG=true to the list of env vars to see debug output from the injector.
  full_command="LD_PRELOAD=""$injector_binary"" DASH0_NAMESPACE_NAME=my-namespace DASH0_POD_NAME=my-pod DASH0_POD_UID=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff DASH0_CONTAINER_NAME=test-app"
  if [ "$env_vars" != "" ]; then
    full_command=" $full_command $env_vars"
  fi
  full_command=" $full_command $test_app_command"
  set +e
  test_output=$(eval "$full_command")
  test_exit_code=$?
  cd "$home_directory"
  set -e
  if [ $test_exit_code != 0 ]; then
    printf "${RED}test \"%s\" crashed:${NC}\n" "$test_case_label"
    echo "test command: $full_command"
    echo "received exit code: $test_exit_code"
    echo "output: $test_output"
    exit_code=1
  elif [ "$test_output" != "$expected" ]; then
    printf "${RED}test \"%s\" failed:${NC}\n" "$test_case_label"
    echo "test command: $full_command"
    echo "expected: $expected"
    echo "actual:   $test_output"
    exit_code=1
  else
    printf "${GREEN}test \"%s\" successful${NC}\n" "$test_case_label"
  fi
}

exit_code=0

# Unrelated env vars
run_test_case \
  "getenv: returns undefined for non-existing environment variable" \
  "app" \
  "node index.js non-existing" \
  "DOES_NOT_EXIST: -"
run_test_case \
  "getenv: returns environment variable unchanged" \
  "app" \
  "node index.js existing" \
  "TEST_VAR: value" \
  "TEST_VAR=value "

# NODE_OPTIONS
run_test_case \
  "getenv: does not add NODE_OPTIONS if it the Dash0 Node.js OTel distro is not present" \
  "app" \
  "node index.js node_options" \
  "NODE_OPTIONS: -" \
  "" \
  "false"
run_test_case \
  "getenv: does not modify existing NODE_OPTIONS if it the Dash0 Node.js OTel distro is not present" \
  "app" \
  "node index.js node_options" \
  "NODE_OPTIONS: --no-deprecation" \
  "NODE_OPTIONS=--no-deprecation" \
  "false"
run_test_case \
  "getenv: does not add NODE_OPTIONS if it the Dash0 Node.js OTel distro is present but cannot be accessed" \
  "app" \
  "node index.js node_options" \
  "NODE_OPTIONS: -" \
  "" \
  "no_permissions"
run_test_case \
  "getenv: overrides NODE_OPTIONS if it is not present" \
  "app" \
  "node index.js node_options" \
  "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
run_test_case \
  "getenv: ask for NODE_OPTIONS (unset) twice" \
  "app" \
  "node index.js node_options_twice" \
  "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry; NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
run_test_case \
  "getenv: prepends to NODE_OPTIONS if it is present" \
  "app" \
  "node index.js node_options" \
  "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --no-deprecation" \
  "NODE_OPTIONS=--no-deprecation"
run_test_case \
  "getenv: ask for NODE_OPTIONS (set) twice" \
  "app" \
  "node index.js node_options_twice" \
  "NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --no-deprecation; NODE_OPTIONS: --require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --no-deprecation" \
  "NODE_OPTIONS=--no-deprecation"

# JAVA_TOOL_OPTIONS
run_test_case \
  "getenv: does not add JAVA_TOOL_OPTIONS if it the Java agent is not present" \
  "app" \
  "node index.js java_tool_options" \
  "JAVA_TOOL_OPTIONS: -" \
  "" \
  "false"
run_test_case \
  "getenv: does not modify existing JAVA_TOOL_OPTIONS if it the Java agent is not present" \
  "app" \
  "node index.js java_tool_options" \
  "JAVA_TOOL_OPTIONS: existing-value" \
  "JAVA_TOOL_OPTIONS=existing-value" \
  "false"
run_test_case \
  "getenv: does not add JAVA_TOOL_OPTIONS if it the Java agent is present but cannot be accessed" \
  "app" \
  "node index.js java_tool_options" \
  "JAVA_TOOL_OPTIONS: -" \
  "" \
  "no_permissions"
run_test_case \
  "getenv: adds JAVA_TOOL_OPTIONS if it the Java agent is present" \
  "app" \
  "node index.js java_tool_options" \
  "JAVA_TOOL_OPTIONS: -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app"
run_test_case \
  "getenv: adds the -javaagent to existing JAVA_TOOL_OPTIONS" \
  "app" \
  "node index.js java_tool_options" \
  "JAVA_TOOL_OPTIONS: -some-option -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app" \
  "JAVA_TOOL_OPTIONS=-some-option"
run_test_case \
  "getenv: merges existing -Dotel.resource.attributes" \
  "app" \
  "node index.js java_tool_options" \
  "JAVA_TOOL_OPTIONS: -Dotel.resource.attributes=my.attr1=value1,my.attr2=value2,k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar" \
  "JAVA_TOOL_OPTIONS=-Dotel.resource.attributes=my.attr1=value1,my.attr2=value2"

# OTEL_RESOURCE_ATTRIBUTES
run_test_case \
  "getenv: sets k8s.pod.uid and k8s.container.name via OTEL_RESOURCE_ATTRIBUTES" \
  "app" \
  "node index.js otel_resource_attributes" \
  "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app"
run_test_case \
  "getenv: sets k8s.pod.uid and k8s.container.name via OTEL_RESOURCE_ATTRIBUTES with pre-existing value" \
  "app" \
  "node index.js otel_resource_attributes" \
  "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app,foo=bar" \
  "OTEL_RESOURCE_ATTRIBUTES=foo=bar"
run_test_case \
  "getenv: use mapped app.kubernetes.io labels for service.name and friends" \
  "app" \
  "node index.js otel_resource_attributes" \
  "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app,service.name=service-name,service.version=service-version,service.namespace=service-namespace" \
  "DASH0_SERVICE_NAME=service-name DASH0_SERVICE_VERSION=service-version DASH0_SERVICE_NAMESPACE=service-namespace"
run_test_case \
  "getenv: use mapped resource.opentelemetry.io labels as additional resource attributes" \
  "app" \
  "node index.js otel_resource_attributes" \
  "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app,aaa=bbb,ccc=ddd" \
  "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd"
run_test_case \
  "getenv: combine mapped app.kubernetes.io and resource.opentelemetry.io labels" \
  "app" \
  "node index.js otel_resource_attributes" \
  "OTEL_RESOURCE_ATTRIBUTES: k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app,service.name=service-name,service.version=service-version,service.namespace=service-namespace,aaa=bbb,ccc=ddd" \
  "DASH0_SERVICE_NAME=service-name DASH0_SERVICE_VERSION=service-version DASH0_SERVICE_NAMESPACE=service-namespace DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd"

if [ "${MISSING_ENVIRON_SYMBOL_TESTS:-}" = "true" ]; then
  # Regression tests for missing __environ symbol:
  run_test_case \
    "no __environ symbol: read unset environment variable" \
    "no_environ_symbol" \
    "./noenviron" \
    "The environmet variable \"NO_ENVIRON_TEST_VAR\" is not set."
  run_test_case \
    "no __environ symbol: read an environment variable that is set" \
    "no_environ_symbol" \
    "./noenviron" \
    "The environmet variable \"NO_ENVIRON_TEST_VAR\" had the value: \"some-value\"." \
    "NO_ENVIRON_TEST_VAR=some-value"
fi

exit $exit_code

