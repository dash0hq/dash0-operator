#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

cd "$(dirname "${BASH_SOURCE[0]}")"/..

if [ -z "${ARCH:-}" ]; then
  ARCH=arm64
fi
if [ "$ARCH" = arm64 ]; then
  docker_platform=linux/arm64
  expected_cpu_architecture=aarch64
  injector_binary=dash0_injector_arm64.so
elif [ "$ARCH" = x86_64 ]; then
  docker_platform=linux/amd64
  expected_cpu_architecture=x86_64
  injector_binary=dash0_injector_x86_64.so
else
  echo "The architecture $ARCH is not supported."
  exit 1
fi

if [ -z "${LIBC:-}" ]; then
  LIBC=glibc
fi

if [ -z "${TEST_SET:-}" ]; then
  TEST_SET=default.tests
fi

base_image=node:22.15.0-bookworm-slim
if [[ "$LIBC" = "musl" ]]; then
  base_image=node:22.15.0-alpine3.21
fi

dockerfile_name="docker/Dockerfile-test"
image_name=dash0-injector-test-$ARCH-$LIBC

create_sdk_dummy_files_script="scripts/create-sdk-dummy-files.sh"
if [[ "$TEST_SET" = "sdk-does-not-exist.tests" ]]; then
  create_sdk_dummy_files_script="scripts/create-no-sdk-dummy-files.sh"
elif [[ "$TEST_SET" = "sdk-cannot-be-accessed.tests" ]]; then
  create_sdk_dummy_files_script="scripts/create-inaccessible-sdk-dummy-files.sh"
fi

docker rmi -f "$image_name" 2> /dev/null

echo "$image_name" >> .container_images_to_be_deleted_at_end
set -x
docker build \
  --platform "$docker_platform" \
  --build-arg "base_image=${base_image}" \
  --build-arg "injector_binary=${injector_binary}" \
  --build-arg "noenviron_binary=noenviron.${ARCH}.${LIBC}" \
  --build-arg "arch_under_test=${ARCH}" \
  --build-arg "libc_under_test=${LIBC}" \
  --build-arg "create_sdk_dummy_files_script=${create_sdk_dummy_files_script}" \
  . \
  -f "$dockerfile_name" \
  -t "$image_name"

docker_run_extra_arguments=""
if [ "${INTERACTIVE:-}" = "true" ]; then
  if [ "$LIBC" = glibc ]; then
    docker_run_extra_arguments=/bin/bash
  elif [ "$LIBC" = musl ]; then
    docker_run_extra_arguments=/bin/sh
  else
    echo "The libc flavor $LIBC is not supported."
    exit 1
  fi
fi

docker run \
  --rm \
  --platform "$docker_platform" \
  --env EXPECTED_CPU_ARCHITECTURE="$expected_cpu_architecture" \
  --env TEST_SET="$TEST_SET" \
  --env TEST_CASES="$TEST_CASES" \
  --env MISSING_ENVIRON_SYMBOL_TESTS="${MISSING_ENVIRON_SYMBOL_TESTS:-}" \
  --env PRINT_TEST_OUTPUT="${PRINT_TEST_OUTPUT:-}" \
  "$image_name" \
  $docker_run_extra_arguments
{ set +x; } 2> /dev/null

