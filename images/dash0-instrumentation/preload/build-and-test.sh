#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

cd "$(dirname "$0")"

if [ -z "${ARCH:-}" ]; then
  ARCH=arm64
fi
if [ "$ARCH" = arm64 ]; then
  docker_platform=linux/arm64
  expected_cpu_architecture=aarch64
elif [ "$ARCH" = x86_64 ]; then
  docker_platform=linux/amd64
  expected_cpu_architecture=x86_64
else
  echo "The architecture $ARCH is not supported."
  exit 1
fi

if [ -z "${LIBC:-}" ]; then
  LIBC=glibc
fi

echo
echo
echo ">>> Building and testing for $ARCH and $LIBC <<<"

dockerfile_name="Dockerfile-$LIBC"
if [ ! -f "$dockerfile_name" ]; then
  echo "The file \"$dockerfile_name\" does not exist, the libc flavor $LIBC is not supported."
  exit 1
fi

image_name=dash0-env-hook-builder-$ARCH-$LIBC
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

docker rm -f "$container_name"
docker build --platform "$docker_platform" . -f "$dockerfile_name" -t "$image_name"

# note: building one image for both platforms is not suppored on Docker desktop
# docker build --platform linux/amd64,linux/arm64 . -f $dockerfile_name -t dash0-env-hook-builder-all-$LIBC

docker run \
  --platform "$docker_platform" \
  --env EXPECTED_CPU_ARCHITECTURE="$expected_cpu_architecture" \
  --name "$container_name" \
  -it \
  --volume "$(pwd):/usr/src/dash0/preload/" \
  "$image_name" \
  $docker_run_extra_arguments

