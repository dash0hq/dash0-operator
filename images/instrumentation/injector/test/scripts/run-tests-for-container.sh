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

base_image=node:20.18-bookworm
if [[ "$LIBC" == "musl" ]]; then
  base_image=node:20.18-alpine3.20
fi

dockerfile_name="docker/Dockerfile-test"
image_name=dash0-injector-test-$ARCH-$LIBC
container_name=$image_name

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

echo
echo ----------------------------------------
echo "testing the injector library on $ARCH and $LIBC"
echo ----------------------------------------

docker rm -f "$container_name" 2> /dev/null
docker rmi -f "$image_name" 2> /dev/null

set -x
docker build \
  --platform "$docker_platform" \
  --build-arg "base_image=${base_image}" \
  --build-arg "injector_binary=${injector_binary}" \
  . \
  -f "$dockerfile_name" \
  -t "$image_name"

docker run \
  --platform "$docker_platform" \
  --env EXPECTED_CPU_ARCHITECTURE="$expected_cpu_architecture" \
  --name "$container_name" \
  -it \
  "$image_name" \
  $docker_run_extra_arguments
{ set +x; } 2> /dev/null
