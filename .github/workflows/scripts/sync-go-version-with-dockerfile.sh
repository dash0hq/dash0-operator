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

if ! command -v gh &> /dev/null; then
  echo "Error: the gh executable is not available." >&2
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

# Determine which go.mod files actually changed on disk.
mapfile -t changed_files < <(git diff --name-only -- '*go.mod')
if [[ ${#changed_files[@]} -eq 0 ]]; then
  echo "No file changes detected after processing, no commit needed."
  exit 0
fi

commit_message="chore(deps): sync go directive in go.mod with golang base image"

# The tip of the pull request's head branch, which the signed commit will be based on. We checked out this branch
# (github.head_ref), so HEAD points at its tip.
base_sha=$(git rev-parse HEAD)

# Create the commit via the GitHub API rather than "git commit"/"git push", so commits are automatically signed.
# Build the fileChanges.additions array, one entry per changed go.mod with its contents base64-encoded.
additions=$(
  for f in "${changed_files[@]}"; do
    jq -n --arg path "$f" --arg contents "$(base64 -w0 "$f")" '{path: $path, contents: $contents}'
  done | jq -s '.'
)

# expectedHeadOid is an optimistic lock: if Dependabot force-pushed the branch in the meantime, its tip no longer
# matches base_sha and the mutation fails instead of clobbering that push (the concurrency group also cancels
# superseded runs).
jq -n \
  --arg repo "$GITHUB_REPOSITORY" \
  --arg branch "$HEAD_REF" \
  --arg headline "$commit_message" \
  --arg oid "$base_sha" \
  --argjson additions "$additions" \
  '{
    query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
    variables: {
      input: {
        branch:          { repositoryNameWithOwner: $repo, branchName: $branch },
        message:         { headline: $headline },
        expectedHeadOid: $oid,
        fileChanges:     { additions: $additions }
      }
    }
  }' | gh api graphql --input - >/dev/null
