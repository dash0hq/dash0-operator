#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

if ! command -v gh &> /dev/null; then
  echo "Error: the gh executable is not available." >&2
  exit 1
fi

branch_name="update-opentelemetry-injector"
version_file="images/instrumentation/opentelemetry-injector/version"

# Base commit that the new branch will be based on.
base_sha=$(git rev-parse HEAD)

images/instrumentation/opentelemetry-injector/update.sh

# git diff-files --quiet exits with 1 if there were differences, exit code 0 means no differences.
if git diff-files --quiet "$version_file"; then
  echo "There are no changes, everything up to date."
  exit 0
fi

echo "There are changes, creating a pull request."
new_version=$(cat "$version_file")
commit_message="chore(deps): update opentelemetry-injector to ${new_version}"

# Remove any branch lingering from a previous failed run (no-op if it does not exist). Note: We abort early if an open
# PR still exists, see .github/workflows/scripts/update-opentelemetry-injector-check-if-pr-exists.sh.
gh api --method DELETE "repos/${GITHUB_REPOSITORY}/git/refs/heads/${branch_name}" >/dev/null 2>&1 || true

# createCommitOnBranch can only commit onto a branch that already exists. Create the PR branch at the base commit.
gh api --method POST "repos/${GITHUB_REPOSITORY}/git/refs" \
  -f ref="refs/heads/${branch_name}" \
  -f sha="${base_sha}" >/dev/null

# Let "gh api graphql"/createCommitOnBranch create the commit via the GitHub API rather than "git commit"/"git push", so
# commits are automatically signed.
# Note: expectedHeadOid is an optimistic lock: the branch tip must still be at base_sha (it is, we just created it).
jq -n \
  --arg repo "$GITHUB_REPOSITORY" \
  --arg branch "$branch_name" \
  --arg headline "$commit_message" \
  --arg oid "$base_sha" \
  --arg path "$version_file" \
  --arg contents "$(base64 -w0 "$version_file")" \
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
  --body "This PR updates the opentelemetry-injector version to ${new_version}."
