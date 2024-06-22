#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

if [[ -z ${ARCH:-} ]]; then
  ARCH=x86_64
fi
if [[ -z ${LIBC:-} ]]; then
  LIBC=glibc
fi

dockerfile_name="Dockerfile-$ARCH-$LIBC"
if [[ ! -f $dockerfile_name ]]; then
  echo "The file \"$dockerfile_name\" does not exist, this combination of CPU architecture and libc flavor is not supported."
  exit 1
fi

image_name=dash0-env-hook-builder-$ARCH-$LIBC
container_name=$image_name

docker_run_extra_arguments=""
if [[ "${INTERACTIVE:-}" == "true" ]]; then
  docker_run_extra_arguments=/bin/bash
fi

docker rm -f $container_name
docker build . -f $dockerfile_name -t $image_name
docker run \
  --name $container_name \
  -it \
  --volume $(pwd):/usr/src/dash0/preload/ \
  $image_name \
  $docker_run_extra_arguments

