#!/usr/bin/env bash
# shellcheck disable=SC2059

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail
shopt -s lastpipe

start_time_build=$(date +%s)

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

cd "$(dirname "${BASH_SOURCE[0]}")"/..

# shellcheck source=images/instrumentation/test/build_time_profiling
source ./test/build_time_profiling

trap print_total_build_time_info EXIT

# shellcheck source=images/instrumentation/injector/test/scripts/util
source injector/test/scripts/util

instrumentation_image="dash0-instrumentation:latest"
all_docker_platforms=linux/arm64,linux/amd64
script_dir="test"
exit_code=0
summary=""

build_or_pull_instrumentation_image() {
  # shellcheck disable=SC2155
  local start_time_step=$(date +%s)
  if [[ -n "${INSTRUMENTATION_IMAGE:-}" ]]; then
    instrumentation_image="$INSTRUMENTATION_IMAGE"

    if is_remote_image "$instrumentation_image"; then
      echo ----------------------------------------
      echo "fetching instrumentation image from remote repository: $instrumentation_image"
      echo ----------------------------------------
      docker pull "$instrumentation_image"
    else
      echo ----------------------------------------
      echo "using existing local instrumentation image: $instrumentation_image"
      echo ----------------------------------------
    fi
    store_build_step_duration "pull instrumentation image" "$start_time_step"
  else
    echo ----------------------------------------
    echo "building multi-arch instrumentation image for platforms ${all_docker_platforms} from local sources"
    echo ----------------------------------------

    if ! build_output=$(
      docker build \
      --platform "$all_docker_platforms" \
      . \
      -t "${instrumentation_image}" \
      2>&1
    ); then
      echo "${build_output}"
      exit 1
    fi

    store_build_step_duration "build instrumentation image" "$start_time_step"
  fi
  echo
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
    # shellcheck disable=SC2155
    local start_time_test_case=$(date +%s)
    test=$(basename "$(realpath "${t}")")

    case "$runtime" in

      jvm)
        test_cmd=(java -jar "/test-cases/${test}/target/app.jar")
        ;;

      node)
        test_cmd=(node "/test-cases/${test}")
        ;;

      *)
        echo "Error: Test handler for runtime \"$runtime\" is not implemented. Please update $(basename "$0"), function run_tests_for_runtime."
        exit 1
        ;;
    esac

    if docker_run_output=$(docker run \
      --env-file="${script_dir}/${runtime}/test-cases/${test}/.env" \
      "$image_name_test" \
      "${test_cmd[@]}" \
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
    store_build_step_duration "test case $image_name_test/$base_image/$test" "$start_time_test_case"
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

  echo ----------------------------------------
  echo "running tests for architecture $arch"
  echo ----------------------------------------

  for r in "${script_dir}"/*/ ; do
    runtime=$(basename "$(realpath "${r}")")
    echo "- runtime: '${runtime}'"
    echo
    grep '^[^#;]' "${script_dir}/${runtime}/base-images" | while read -r base_image ; do
      echo "- base image: '${base_image}'"
      image_name_test="test-${runtime}-${arch}:latest"
      echo "building test image for ${arch}/${runtime}/${base_image} with instrumentation image ${instrumentation_image}"
      # shellcheck disable=SC2155
      local start_time_docker_build=$(date +%s)
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
      store_build_step_duration "docker build $arch/$runtime/$base_image" "$start_time_docker_build"
      run_tests_for_runtime "${runtime}" "$image_name_test" "$base_image"
      echo
    done
  done
  echo
  echo
}

if [[ "${CI:-false}" != true ]]; then
  dockerDriver="$(docker info -f '{{ .DriverStatus }}')"
  if [[ "$dockerDriver" != *"io.containerd."* ]]; then
    echo "Error: This script requires that the containerd image store is enabled for Docker, since the script needs to build and use multi-arch images locally. You driver is $dockerDriver. Please see https://docs.docker.com/desktop/containerd/#enable-the-containerd-image-store for instructions on enabling the containerd image store."
    exit 1
  fi
fi

build_or_pull_instrumentation_image

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

