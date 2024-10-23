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

# remove all outdated injector binaries
rm -rf injector/test/bin/*

exit_code=0
summary=""
run_tests_for_architecture_and_libc_flavor() {
  arch=$1
  libc=$2
  set +e
  ARCH=$arch LIBC=$libc injector/test/scripts/run-tests-for-container.sh
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
  if [[ "$line" =~ ^[[:space:]]*#.* ]]; then
    # skip comments
    continue
  fi
  if [[ $copy_lines == true && "$line" =~ ^FROM[[:space:]]+.*[[:space:]]+AS[[:space:]]+build-node.js$ ]]; then
    copy_lines=false
  fi
  if [[ "$line" =~ ^FROM[[:space:]]+.*[[:space:]]+AS[[:space:]]+build-injector$ ]]; then
    copy_lines=true
  fi
  if [[ $copy_lines == true ]]; then
    echo "$line" >> "$dockerfile_injector_build"
  fi
done < Dockerfile

if [[ ! -e "$dockerfile_injector_build" ]]; then
  printf "\nError: The file $dockerfile_injector_build has not been generated, stopping."
  exit 1
fi

# build injector binary for both architectures
ARCH=arm64 injector/test/scripts/build-in-container.sh
ARCH=x86_64 injector/test/scripts/build-in-container.sh

run_tests_for_architecture_and_libc_flavor arm64 glibc
run_tests_for_architecture_and_libc_flavor x86_64 glibc
run_tests_for_architecture_and_libc_flavor arm64 musl
run_tests_for_architecture_and_libc_flavor x86_64 musl

printf "$summary\n"
exit $exit_code

