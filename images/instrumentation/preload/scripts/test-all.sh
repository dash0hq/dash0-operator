#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

cd "$(dirname "$0")"/..

ARCH=arm64 scripts/build-in-container.sh
ARCH=x86_64 scripts/build-in-container.sh

exit_code=0
summary=""

run_tests_for_architecture_and_libc_flavor() {
  arch=$1
  libc=$2
  set +e
  ARCH=$arch LIBC=$libc scripts/run-tests-in-container.sh
  test_exit_code=$?
  set -e
  echo
  echo ---------------------------------------
  if [ $test_exit_code != 0 ]; then
    printf "${RED}tests for %s/%s failed (see above for details)${NC}\n" "$arch" "$libc"
    exit_code=1
    summary="$summary\n$arch/$libc:\tfailed"
  else
    printf "${GREEN}tests for %s/%s were successful${NC}\n" "$arch" "$libc"
    summary="$summary\n$arch/$libc:\tok"
  fi
  echo ---------------------------------------
  echo
}

run_tests_for_architecture_and_libc_flavor arm64 glibc
run_tests_for_architecture_and_libc_flavor x86_64 glibc
run_tests_for_architecture_and_libc_flavor arm64 musl
run_tests_for_architecture_and_libc_flavor x86_64 musl

echo "$summary"
exit $exit_code

