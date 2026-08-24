#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

if ! command -v gh &> /dev/null; then
  echo "Error: the gh executable is not available." >&2
  exit 1
fi

branch_name="update-ruby-distribution"
version_file="images/instrumentation/ruby/dash0-ruby-distribution-version"

base_sha=$(git rev-parse HEAD)

images/instrumentation/ruby/update.sh

if git diff-files --quiet "$version_file"; then
  echo "There are no changes, everything up to date."
  exit 0
fi

echo "There are changes, creating a pull request."
new_version=$(cat "$version_file")
commit_message="chore(deps): update opentelemetry-ruby-distribution to ${new_version}"

gh api --method DELETE "repos/${GITHUB_REPOSITORY}/git/refs/heads/${branch_name}" >/dev/null 2>&1 || true

gh api --method POST "repos/${GITHUB_REPOSITORY}/git/refs" \
  -f ref="refs/heads/${branch_name}" \
  -f sha="${base_sha}" >/dev/null

jq -n \
  --arg repo "$GITHUB_REPOSITORY" \
  --arg branch "$branch_name" \
  --arg headline "$commit_message" \
  --arg oid "$base_sha" \
  --arg path "$version_file" \
  --arg contents "$(base64 < "$version_file" | tr -d '\n')" \
  '{
    query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
    variables: {
      input: {
        branch:          { repositoryNameWithOwner: $repo, branchName: $branch },
        message:         { headline: $headline },
        expectedHeadOid: $oid,
        fileChanges:     { additions: [ { path: $path, contents: $contents } ] }
      }
    }
  }' | gh api graphql --input - >/dev/null

gh pr create \
  -B main \
  -H "$branch_name" \
  --title "$commit_message" \
  --body "This PR updates the opentelemetry-ruby-distribution version to ${new_version}."
