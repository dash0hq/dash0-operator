#!/usr/bin/env bash
# shellcheck disable=SC2059

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'
dockerfile_injector_build=injector/test/docker/Dockerfile-build

cd "$(dirname "${BASH_SOURCE[0]}")"/../../..

# shellcheck source=images/instrumentation/injector/test/scripts/util
source injector/test/scripts/util

# remove all outdated injector binaries
rm -rf injector/test/bin/*

architectures=""
if [[ -n "${ARCHITECTURES:-}" ]]; then
  architectures=("${ARCHITECTURES//,/ }")
  echo Only testing a subset of architectures: "${architectures[@]}"
fi
libc_flavors=""
if [[ -n "${LIBC_FLAVORS:-}" ]]; then
  libc_flavors=("${LIBC_FLAVORS//,/ }")
  echo Only testing a subset of libc flavors: "${libc_flavors[@]}"
fi
if [[ -n "${TEST_CASES:-}" ]]; then
  echo Only running a subset of test cases : "$TEST_CASES"
else
  TEST_CASES=""
fi

exit_code=0
summary=""
run_tests_for_architecture_and_libc_flavor() {
  arch=$1
  libc=$2
  set +e
  ARCH="$arch" LIBC="$libc" TEST_CASES="$TEST_CASES" injector/test/scripts/run-tests-for-container.sh
  test_exit_code=$?
  set -e
  echo
  echo ----------------------------------------
  if [ $test_exit_code != 0 ]; then
    printf "${RED}tests for %s/%s failed (see above for details)${NC}\n" "$arch" "$libc"
    exit_code=1
    summary="$summary\n$arch/$libc:\t${RED}failed${NC}"
  else
    printf "${GREEN}tests for %s/%s were successful${NC}\n" "$arch" "$libc"
    summary="$summary\n$arch/$libc:\t${GREEN}ok${NC}"
  fi
  echo ----------------------------------------
  echo
}

# Create a Dockerfile for building the injector in a container by re-using an excerpt from the ../Dockerfile. This makes
# sure we keep the way we build the injector binary here for this test suite and for actual production usage in the
# instrumentation image in sync.
copy_lines=false
rm -f "$dockerfile_injector_build"
while IFS= read -r line; do
  if [[ "$line" =~ ^.*injector_build_start.*$ ]]; then
    copy_lines=true
    continue
  fi
  if [[ $copy_lines == true && "$line" =~ ^.*injector_build_end.*$ ]]; then
    copy_lines=false
    continue
  fi
  if [[ "$line" =~ ^[[:space:]]*#.* ]]; then
    # skip comments
    continue
  fi
  if [[ $copy_lines == true ]]; then
    echo "$line" >> "$dockerfile_injector_build"
  fi
done < Dockerfile

if [[ ! -e "$dockerfile_injector_build" ]]; then
  printf "\nError: The file $dockerfile_injector_build has not been generated, stopping."
  exit 1
fi

declare -a all_architectures=(
  "arm64"
  "x86_64"
)
declare -a all_libc_flavors=(
  "glibc"
  "musl"
)

instrumentation_image=${INSTRUMENTATION_IMAGE:-}
if [[ -z "$instrumentation_image" ]]; then
  # build injector binary for both architectures
  echo ----------------------------------------
  echo building the injector binary locally from source
  echo ----------------------------------------
  for arch in "${all_architectures[@]}"; do
    if [[ -n "${architectures[0]}" ]]; then
      if [[ $(echo "${architectures[@]}" | grep -o "$arch" | wc -w) -eq 0 ]]; then
        echo ----------------------------------------
        echo "skipping build for CPU architecture $arch"
        echo ----------------------------------------
        continue
      fi
    fi

    ARCH="$arch" injector/test/scripts/build-in-container.sh
  done
else
  if is_remote_image "$instrumentation_image"; then
    echo ----------------------------------------
    printf "using injector binary from existing remote image:\n$instrumentation_image\n"
    echo ----------------------------------------
    docker pull --platform linux/arm64 "$instrumentation_image"
    copy_injector_binary_from_container_image "$instrumentation_image" arm64 linux/arm64
    docker pull --platform linux/amd64 "$instrumentation_image"
    copy_injector_binary_from_container_image "$instrumentation_image" x86_64 linux/amd64
  else
    echo ----------------------------------------
    printf "using injector binary from existing local image:\n$instrumentation_image\n"
    echo ----------------------------------------
    for arch in "${all_architectures[@]}"; do
      if [[ -n "${architectures[0]}" ]]; then
        if [[ $(echo "${architectures[@]}" | grep -o "$arch" | wc -w) -eq 0 ]]; then
          echo ----------------------------------------
          echo "skipping copying injector binary from container for CPU architecture $arch"
          echo ----------------------------------------
          continue
        fi
      fi

      if [[ "$arch" = "arm64" ]]; then
        docker_platform="linux/arm64"
      elif [[ "$arch" = "x86_64" ]]; then
        docker_platform="linux/amd64"
      else
        echo "The architecture $arch is not supported."
        exit 1
      fi
      copy_injector_binary_from_container_image "$instrumentation_image" "$arch" "$docker_platform"
    done
  fi
fi
echo

for arch in "${all_architectures[@]}"; do
  if [[ -n "${architectures[0]}" ]]; then
    if [[ $(echo "${architectures[@]}" | grep -o "$arch" | wc -w) -eq 0 ]]; then
      echo ----------------------------------------
      echo "skipping tests on CPU architecture $arch"
      echo ----------------------------------------
      continue
    fi
  fi

  for libc_flavor in "${all_libc_flavors[@]}"; do
    if [[ -n "${libc_flavors[0]}" ]]; then
      if [[ $(echo "${libc_flavors[@]}" | grep -o "$libc_flavor" | wc -w) -eq 0 ]]; then
        echo ----------------------------------------
        echo "skipping tests for libc flavor $libc_flavor"
        echo ----------------------------------------
        continue
      fi
    fi

    run_tests_for_architecture_and_libc_flavor "$arch" "$libc_flavor"
  done
done

printf "$summary\n\n"
exit $exit_code
