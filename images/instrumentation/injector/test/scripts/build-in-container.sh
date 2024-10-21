#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

cd "$(dirname "$0")"/../../..

if [ -z "${ARCH:-}" ]; then
  ARCH=arm64
fi
if [ "$ARCH" = arm64 ]; then
  docker_platform=linux/arm64
elif [ "$ARCH" = x86_64 ]; then
  docker_platform=linux/amd64
else
  echo "The architecture $ARCH is not supported."
  exit 1
fi

dockerfile_name=injector/test/docker/Dockerfile-build
image_name=dash0-injector-builder-$ARCH
container_name=$image_name

docker_run_extra_arguments=""
if [ "${INTERACTIVE:-}" = "true" ]; then
  docker_run_extra_arguments=/bin/bash
fi

echo
echo
echo ">>> Building the library on $ARCH <<<"

docker rmi -f "$image_name" 2> /dev/null
docker rm -f "$container_name" 2> /dev/null

docker build \
  --platform "$docker_platform" \
  . \
  -f "$dockerfile_name" \
  -t "$image_name"

container_id=$(docker create "$image_name")
docker container cp \
  $container_id:/dash0-init-container/injector/bin/dash0_injector.so \
  injector/test/bin/dash0_injector_$ARCH.so
docker rm -v $container_id

