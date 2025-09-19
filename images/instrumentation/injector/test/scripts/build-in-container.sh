#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

cd "$(dirname "${BASH_SOURCE[0]}")"/../../..

if ! docker info > /dev/null 2>&1; then
  echo "This script uses docker, but it looks like Docker is not running. Please start docker and try again."
  exit 1
fi

# shellcheck source=images/instrumentation/injector/test/scripts/util
. injector/test/scripts/util

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

if [ -z "${GOARCH:-}" ]; then
  GOARCH=arm64
fi

source_file_name=${1:-}
if [[ -z "$source_file_name" ]]; then
  echo "error: mandatory argument \"source_file_name\" is missing"
  exit 1
fi
target_file_name=${2:-}
if [[ -z "$target_file_name" ]]; then
  echo "error: mandatory argument \"target_file_name\" is missing"
  exit 1
fi
dockerfile_name="${3:-injector/test/docker/Dockerfile-build}"
image_name="${4:-dash0-injector-builder-$ARCH}"
container_name=$image_name
context="${5:-.}"

echo
echo
echo ">>> Building on architecture $ARCH <<<"

docker rmi -f "$image_name" 2> /dev/null
docker rm -f "$container_name" 2> /dev/null

echo "$image_name" >> injector/test/.container_images_to_be_deleted_at_end
docker build \
  --platform "$docker_platform" \
  --build-arg base_image="${BASE_IMAGE:-not_set}" \
  "$context" \
  -f "$dockerfile_name" \
  -t "$image_name"

copy_binary_from_container_image \
  "$image_name" \
  "$docker_platform" \
  "$source_file_name" \
  "$target_file_name"
