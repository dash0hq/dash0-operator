#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..

cd "$project_root"

# This script verifies that every Go package directory which contains Ginkgo specs also contains a suite bootstrap (e.g.
# a Go test function which calls RunSpecs). Without such a bootstrap, go test compiles the specs and reports the
# package as passing, but the specs are not executed.

# Matches the Ginkgo DSL imports, that is, "github.com/onsi/ginkgo/v2" and its dsl sub-packages, but not the auxiliary
# packages like ginkgo/v2/types, which do not bring specs with them.
ginkgo_dsl_import_pattern='onsi/ginkgo/v2(/dsl/[a-z]+)?"'

directories_with_specs=()

while IFS= read -r -d '' test_file; do
  if grep -qE "$ginkgo_dsl_import_pattern" "$test_file"; then
    directories_with_specs+=("$(dirname "$test_file")")
  fi
done < <(
  find . \
    -type f \
    -name '*_test.go' \
    -not -path './images/collector/opentelemetry-collector/*' \
    -not -path './images/collector/opentelemetry-collector-contrib/*' \
    -not -path '*/node_modules/*' \
    -print0
)

if [[ "${#directories_with_specs[@]}" -eq 0 ]]; then
  echo "Error: no Go test file importing the Ginkgo DSL has been found, this check is most likely broken." >&2
  exit 1
fi

errors=0

while IFS= read -r directory; do
  if ! grep -qE '\bRunSpecs\(' "$directory"/*_test.go; then
    echo "Error: the directory $directory contains Ginkgo specs, but no suite bootstrap, hence none of its specs" \
      "are executed. Add a file with a Go test function which calls RunSpecs, see the *_suite_test.go files of the" \
      "other packages." >&2
    errors=$((errors + 1))
  fi
done < <(printf '%s\n' "${directories_with_specs[@]}" | sort -u)

if [[ "$errors" -gt 0 ]]; then
  echo "${BASH_SOURCE[0]}": "$errors" package\(s\) with Ginkgo specs but without a suite bootstrap found. >&2
  exit 1
fi

echo "${BASH_SOURCE[0]}": All checks have passed.
