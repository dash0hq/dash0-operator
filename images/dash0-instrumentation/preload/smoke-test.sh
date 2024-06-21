#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

relative_directory="$(dirname ${BASH_SOURCE})"
directory="$(realpath $relative_directory)"
cd "$directory"

make clean
make

exit_code=0

test_output_1=$(LD_PRELOAD="$directory/libdash0envhook.so" ./appundertest.so)

if [[ "$test_output_1" == "TERM: xterm; NODE_OPTIONS: --require /dash0; " ]]; then
  printf "${GREEN}test 1 successful${NC}\n"
else
  printf "${RED}test 1 failed:${NC}\n"
  echo "$test_output_1"
  exit_code=1
fi

test_output_2=$(LD_PRELOAD="$directory/libdash0envhook.so" NODE_OPTIONS="existing NODE_OPTIONS" ./appundertest.so)
if [[ "$test_output_2" == "TERM: xterm; NODE_OPTIONS: --require /dash0 existing NODE_OPTIONS; " ]]; then
  printf "${GREEN}test 2 successful${NC}\n"
else
  printf "${RED}test 2 failed:${NC}\n"
  echo "$test_output_2"
  exit_code=1
fi

exit $exit_code

