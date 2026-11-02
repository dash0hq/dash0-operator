#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Checks that the committed OLM bundle in bundle/ is up to date: generates it into a temporary directory and compares
# both, ignoring the annotation createdAt. Use "make olm-bundle-check" instead of calling this directly.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

if ! make --no-print-directory olm-bundle BUNDLE_DIR="$tmp_dir/bundle" > "$tmp_dir/generate.log" 2>&1; then
  cat "$tmp_dir/generate.log"
  echo "error: generating the OLM bundle has failed"
  exit 1
fi

if ! diff -r -I '^    createdAt: ' bundle "$tmp_dir/bundle"; then
  echo "error: the committed OLM bundle in bundle/ is not up to date, run make olm-bundle and commit the result"
  exit 1
fi

echo "${BASH_SOURCE[0]}: the committed OLM bundle in bundle/ is up to date."
