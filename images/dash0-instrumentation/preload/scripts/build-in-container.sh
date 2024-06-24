#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

cd "$(dirname "$0")"/..

# TODO build multi platform image

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

dockerfile_name=docker/Dockerfile-build
image_name=dash0-env-hook-builder
container_name=$image_name

docker_run_extra_arguments=""
if [ "${INTERACTIVE:-}" = "true" ]; then
  docker_run_extra_arguments=/bin/bash
fi

echo
echo
echo ">>> Building the library on $ARCH <<<"

docker rm -f "$container_name"
docker build \
  --platform "$docker_platform" \
  . \
  -f "$dockerfile_name" \
  -t "$image_name"

# note: building one image for both platforms is not supported by Docker desktop's standard "docker build" command.
# docker build --platform linux/amd64,linux/arm64 . -f $dockerfile_name -t dash0-env-hook-builder-all-$LIBC

docker run \
  --platform "$docker_platform" \
  --name "$container_name" \
  -it \
  --volume "$(pwd):/usr/src/dash0/preload/" \
  "$image_name" \
  $docker_run_extra_arguments

