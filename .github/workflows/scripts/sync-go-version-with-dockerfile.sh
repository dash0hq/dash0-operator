#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Synchronizes the `go` directive in each go.mod file with the golang base image tag of its associated Dockerfile.
# The Dockerfile's golang:<version> base image is treated as the source of truth; whenever the `go` directive in the
# associated go.mod differs, it is rewritten to match. If anything changed, the result is committed and pushed to the
# branch given via the HEAD_REF environment variable.
#
# This is invoked from the sync-go-version-with-dockerfile.yaml workflow. HEAD_REF must be set to the name of the
# branch to push the synchronizing commit to (the pull request's head branch).

set -euo pipefail

if [[ -z "${HEAD_REF:-}" ]]; then
  echo "Error: the HEAD_REF environment variable is not set." >&2
  exit 1
fi

# Mapping of "Dockerfile:go.mod" pairs. Each Dockerfile's golang base image tag is the source of truth; the `go`
# directive in the associated go.mod is updated to match it. The list intentionally only contains Dockerfiles that
# build from a golang:* base image and have an associated go.mod file. images/collector has no tracked go.mod (the
# collector is built via the OpenTelemetry collector builder), so it is not listed.
mappings=(
  "Dockerfile:go.mod"
  "images/configreloader/Dockerfile:images/configreloader/src/go.mod"
  "images/filelogoffsetsync/Dockerfile:images/filelogoffsetsync/src/go.mod"
  "test/e2e/otlp-sink/telemetrymatcher/Dockerfile:test/e2e/otlp-sink/telemetrymatcher/go.mod"
  "test/e2e/dash0-api-mock/Dockerfile:test/e2e/dash0-api-mock/go.mod"
  "test/e2e/control-plane-mock/Dockerfile:test/e2e/control-plane-mock/go.mod"
  "test/e2e/decision-maker-mock/Dockerfile:test/e2e/decision-maker-mock/go.mod"
)

changed=false

for mapping in "${mappings[@]}"; do
  dockerfile="${mapping%%:*}"
  gomod="${mapping##*:}"

  if [[ ! -f "$dockerfile" ]]; then
    echo "skipping ${dockerfile}: file not found"
    continue
  fi
  if [[ ! -f "$gomod" ]]; then
    echo "skipping ${gomod}: file not found"
    continue
  fi

  # Extract the version from the first golang:<version> reference in the Dockerfile, dropping any suffix such as
  # -alpine3.23. Matches both "golang:1.26.4" and "golang:1.26.4-alpine3.23".
  docker_go_version=$(grep -oE 'golang:[0-9]+\.[0-9]+(\.[0-9]+)?' "$dockerfile" | head -n 1 | sed 's/^golang://')
  if [[ -z "$docker_go_version" ]]; then
    echo "skipping ${dockerfile}: no golang base image version found"
    continue
  fi

  # Extract the current value of the `go` directive (e.g. "1.26.4") from go.mod.
  gomod_go_version=$(grep -oE '^go [0-9]+\.[0-9]+(\.[0-9]+)?' "$gomod" | head -n 1 | awk '{print $2}')
  if [[ -z "$gomod_go_version" ]]; then
    echo "skipping ${gomod}: no go directive found"
    continue
  fi

  if [[ "$docker_go_version" == "$gomod_go_version" ]]; then
    echo "${gomod}: go directive (${gomod_go_version}) already matches ${dockerfile} (golang:${docker_go_version}), nothing to do"
    continue
  fi

  echo "${gomod}: updating go directive from ${gomod_go_version} to ${docker_go_version} to match ${dockerfile}"
  # Replace only the `go` directive line, leaving the rest of go.mod untouched for a minimal diff.
  sed -i.bak -E "s/^go [0-9]+\.[0-9]+(\.[0-9]+)?$/go ${docker_go_version}/" "$gomod"
  rm -f "${gomod}.bak"
  changed=true
done

if [[ "$changed" != "true" ]]; then
  echo "All go.mod files are already in sync with their Dockerfiles, no commit needed."
  exit 0
fi

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

if git diff --quiet; then
  echo "No file changes detected after processing, no commit needed."
  exit 0
fi

git add -- '*go.mod'
git commit --message "chore(deps): sync go directive in go.mod with golang base image"
git push origin "HEAD:${HEAD_REF}"
