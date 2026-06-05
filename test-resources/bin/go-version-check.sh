#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..

cd "$project_root"

# This script verifies that the Go version declared in a go.mod file matches the Go version that is used elsewhere,
# that is, either:
#   * the Go version of the base image of an associated Dockerfile (golang:<version>[-<suffix>]), or
#   * the Go version declared in another go.mod file.
#
# The pairs to check are passed as command line arguments. Each argument describes one pair and has the form
#   dockerfile:<go.mod path>:<Dockerfile path>
#   gomod:<go.mod path>:<go.mod path>
# The list of pairs is maintained in the Makefile (target go-version-check).

errors=0

# Parses the Go version (e.g. "1.26.4") from the "go" directive of a go.mod file.
get_go_version_from_go_mod() {
  local go_mod_file="$1"
  if [[ ! -f "$go_mod_file" ]]; then
    echo "Error: the go.mod file \"$go_mod_file\" does not exist." >&2
    exit 1
  fi
  local version
  version=$(grep -E '^go [0-9]' "$go_mod_file" | head -n 1 | awk '{print $2}')
  if [[ -z "$version" ]]; then
    echo "Error: cannot determine the Go version from \"$go_mod_file\"." >&2
    exit 1
  fi
  echo "$version"
}

# Parses the Go version (e.g. "1.26.4") from the golang base image tag of a Dockerfile, that is, from a line like
# "FROM --platform=${BUILDPLATFORM} golang:1.26.4-alpine3.23 AS builder". The optional image tag suffix (e.g.
# "-alpine3.23") is stripped.
get_go_version_from_dockerfile() {
  local dockerfile="$1"
  if [[ ! -f "$dockerfile" ]]; then
    echo "Error: the Dockerfile \"$dockerfile\" does not exist." >&2
    exit 1
  fi
  local version
  version=$(grep -E '^FROM .*golang:[0-9]' "$dockerfile" | head -n 1 | sed -E 's/.*golang:([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/')
  if [[ -z "$version" ]]; then
    echo "Error: cannot determine the Go version from the golang base image in \"$dockerfile\"." >&2
    exit 1
  fi
  echo "$version"
}

check_go_mod_against_dockerfile() {
  local go_mod_file="$1"
  local dockerfile="$2"
  local go_mod_version dockerfile_version
  go_mod_version=$(get_go_version_from_go_mod "$go_mod_file")
  dockerfile_version=$(get_go_version_from_dockerfile "$dockerfile")
  if [[ "$go_mod_version" != "$dockerfile_version" ]]; then
    echo "Mismatch: $go_mod_file declares Go $go_mod_version, but $dockerfile uses golang base image Go $dockerfile_version." >&2
    errors=$((errors + 1))
  else
    echo "OK: $go_mod_file and $dockerfile both use Go $go_mod_version."
  fi
}

check_go_mod_against_go_mod() {
  local go_mod_file_1="$1"
  local go_mod_file_2="$2"
  local version_1 version_2
  version_1=$(get_go_version_from_go_mod "$go_mod_file_1")
  version_2=$(get_go_version_from_go_mod "$go_mod_file_2")
  if [[ "$version_1" != "$version_2" ]]; then
    echo "Mismatch: $go_mod_file_1 declares Go $version_1, but $go_mod_file_2 declares Go $version_2." >&2
    errors=$((errors + 1))
  else
    echo "OK: $go_mod_file_1 and $go_mod_file_2 both use Go $version_1."
  fi
}

if [[ $# -eq 0 ]]; then
  echo "Error: no pairs to check have been provided." >&2
  exit 1
fi

for pair in "$@"; do
  IFS=':' read -r kind path_1 path_2 <<< "$pair"
  case "$kind" in
    dockerfile)
      check_go_mod_against_dockerfile "$path_1" "$path_2"
      ;;
    gomod)
      check_go_mod_against_go_mod "$path_1" "$path_2"
      ;;
    *)
      echo "Error: unknown pair kind \"$kind\" in argument \"$pair\"." >&2
      exit 1
      ;;
  esac
done

if [[ "$errors" -gt 0 ]]; then
  echo "${BASH_SOURCE[0]}": "$errors" Go version mismatch\(es\) found. >&2
  exit 1
fi

echo "${BASH_SOURCE[0]}": All checks have passed.
