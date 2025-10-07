#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

# See https://ziglang.org/download/ for the correct strings for zig_architecture. (Needs to match the architecture part
# of the download URL.)
ARCHITECTURE="${ARCHITECTURE:-arm64}"
if [[ "$ARCHITECTURE" = arm64 ]]; then
  docker_platform=linux/arm64
  zig_architecture=aarch64
elif [[ "$ARCHITECTURE" = x86_64 ]]; then
  docker_platform=linux/amd64
  zig_architecture=x86_64
else
  echo "The architecture $ARCHITECTURE is not supported."
  exit 1
fi

# Debian/Ubuntu and Alpine base images are supported, for example:
# - BASE_IMAGE=ubuntu:noble
# - BASE_IMAGE=debian:11-slim
# - BASE_IMAGE=alpine:3.22.1
base_image="${BASE_IMAGE:-ubuntu:noble}"
base_image_for_image_name="${base_image//:/-}"
base_image_for_image_name="${base_image_for_image_name//./-}"
base_image_for_image_name="${base_image_for_image_name//\//-}"
image_name="dash0-injector-dev-$ARCHITECTURE-$base_image_for_image_name"
container_name="$image_name"

if docker container inspect "$container_name" > /dev/null 2>&1; then
   # shellcheck disable=SC2086
  if [[ "$( docker container inspect -f '{{.State.Running}}' $container_name )" = "true" ]]; then
  echo "Container $container_name is already running, joining existing container."
  docker exec \
    -it \
    "$container_name" \
    /bin/bash
 fi
 exit 0
fi

docker rmi -f "$image_name" 2> /dev/null || true
docker build \
  --platform "$docker_platform" \
  --build-arg "base_image=$base_image" \
  --build-arg "zig_architecture=${zig_architecture}" \
  -f Dockerfile-injector-development \
  -t "$image_name" \
  .

container_name="$image_name"
docker rm -f "$container_name" 2> /dev/null || true
docker run \
  --rm \
  -it \
  --platform "$docker_platform" \
  --name "$container_name" \
  --volume "$(pwd):/home/dash0/instrumentation" \
  "$image_name" \
  /bin/bash
