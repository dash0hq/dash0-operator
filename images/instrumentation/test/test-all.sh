#!/usr/bin/env bash
# shellcheck disable=SC2059

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail
shopt -s lastpipe

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

cd "$(dirname "$0")"/..

script_dir="test"
exit_code=0
summary=""

build_instrumentation_image() {
  instrumentation_image=${1:-}
  arch=${2:-}
  docker_platform=${3:-}

  if [[ -z ${instrumentation_image} ]]; then
    echo "missing mandatory argument: instrumentation_image"
    exit 1
  fi
  if [[ -z ${arch} ]]; then
    echo "missing mandatory argument: architecture"
    exit 1
  fi
  if [[ -z ${docker_platform} ]]; then
    echo "missing mandatory argument: docker_platform"
    exit 1
  fi

  echo "building instrumentation image \"${instrumentation_image} for architecture ${arch}"
  if ! build_output=$(
    docker build \
    --platform "$docker_platform" \
    . \
    -t "${instrumentation_image}" \
    2>&1
  ); then
    echo "${build_output}"
    exit 1
  fi
}

run_tests_for_runtime() {
  local runtime="${1:-}"
  local image_name_test="${2:-}"
  local base_image="${3:?}"

  if [[ -z $runtime ]]; then
    echo "missing parameter: runtime"
    exit 1
  fi
  if [[ -z $image_name_test ]]; then
    echo "missing parameter: image_name_test"
    exit 1
  fi

  for t in "${script_dir}"/"${runtime}"/test-cases/*/ ; do
    test=$(basename "$(realpath "${t}")")
    if docker_run_output=$(docker run \
      --env-file="${script_dir}/${runtime}/test-cases/${test}/.env" \
      "${image_name_test}" \
      node "/test-cases/${test}" \
      2>&1
    ); then
      printf "${GREEN}test case \"${test}\": OK${NC}\n"
    else
      printf "${RED}test case \"${test}\": FAIL\n"
      printf "test output:${NC}\n"
      echo "$docker_run_output"
      exit_code=1
      summary="$summary\n${runtime}/${base_image}\t- ${test}:\tfailed"
    fi
  done
}

run_tests_for_architecture() {
  arch="${1:-}"
  if [[ -z ${arch} ]]; then
    echo "missing mandatory argument: architecture"
    exit 1
  fi
  if [ "$arch" = arm64 ]; then
    docker_platform=linux/arm64
  elif [ "$arch" = x86_64 ]; then
    docker_platform=linux/amd64
  else
    echo "The architecture $arch is not supported."
    exit 1
  fi

  instrumentation_image="dash0-instrumentation-$arch:latest"
  build_instrumentation_image "$instrumentation_image" "$arch" "$docker_platform"
  for r in "${script_dir}"/*/ ; do
    runtime=$(basename "$(realpath "${r}")")
    echo "runtime '${runtime}'"
    grep '^[^#;]' "${script_dir}/${runtime}/base-images" | while read -r base_image ; do
      echo "base image '${base_image}'"
      image_name_test="test-${runtime}-${arch}:latest"
      echo "building test image for ${arch}/${runtime}/${base_image} with instrumentation image ${instrumentation_image}"
      if ! build_output=$(
        docker build \
          --platform "$docker_platform" \
          --build-arg "instrumentation_image=${instrumentation_image}" \
          --build-arg "base_image=${base_image}" \
          "${script_dir}/${runtime}" \
          -t "$image_name_test" \
          2>&1
      ); then
        echo "${build_output}"
        exit 1
      fi
      run_tests_for_runtime "${runtime}" "$image_name_test" "$base_image"
    done
  done
}

run_tests_for_architecture arm64
run_tests_for_architecture x86_64

if [[ $exit_code -ne 0 ]]; then
  printf "\n${RED}There have been failing test cases:"
  printf "$summary\n"
  printf "\nSee above for details.${NC}\n"
else
  printf "\n${GREEN}All test cases have passed.${NC}\n"
fi
exit $exit_code

