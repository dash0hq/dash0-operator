#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

if ! command -v gh &> /dev/null; then
  echo "Error: the gh executable is not available." >&2
  exit 1
fi

branch_name="update-dash0-collector-components"
config_file="images/collector/src/builder/config.yaml"
telemetry_go_mod="images/collector/src/telemetry/go.mod"
telemetry_module_files=("$telemetry_go_mod" "images/collector/src/telemetry/go.sum")
# All files that update-collector-components-check-and-bump-versions.sh can modify.
changed_files=("$config_file" "${telemetry_module_files[@]}")

# Base commit that the new branch will be based on.
base_sha=$(git rev-parse HEAD)

COLLECTOR_VERSIONS_OUTPUT=$(mktemp)
export COLLECTOR_VERSIONS_OUTPUT
trap 'rm -f "$COLLECTOR_VERSIONS_OUTPUT"' EXIT
.github/workflows/scripts/update-collector-components-check-and-bump-versions.sh
# shellcheck source=/dev/null
source "$COLLECTOR_VERSIONS_OUTPUT"

# git diff-files --quiet exits with 1 if there were differences, exit code 0 means no differences.
if git diff-files --quiet -- "${changed_files[@]}"; then
  echo "There are no changes, everything up to date."
  exit 0
fi

echo "There are changes, creating a pull request."

commit_message="chore(deps): bump Dash0 collector components"
pr_body=""
# Note: new_stable_version etc. are sourced from $COLLECTOR_VERSIONS_OUTPUT, which is populated by
# .github/workflows/scripts/update-collector-components-check-and-bump-versions.sh.
# shellcheck disable=SC2154
if [[ -n "${new_stable_version:-}" && -n "${new_beta_version:-}" && -n "${new_contrib_version:-}" ]]; then
  commit_message="chore(deps): bump Dash0 collector components (${new_stable_version}/${new_beta_version}/${new_contrib_version})"
  pr_body=$(printf 'Update to:\n- core stable version: %s\n- core beta version: v%s\n- contrib version: v%s' \
    "$new_stable_version" "$new_beta_version" "$new_contrib_version")
fi

if ! git diff-files --quiet -- "${telemetry_module_files[@]}"; then
  telemetry_module_note=$(printf \
    'Also aligns the collector modules required by %s with these versions.' \
    "$telemetry_go_mod")
  if [[ -n "$pr_body" ]]; then
    pr_body="${pr_body}"$'\n\n'"${telemetry_module_note}"
  else
    pr_body="$telemetry_module_note"
  fi
fi

# Remove any branch lingering from a previous failed run (no-op if it does not exist). Note: We abort early if an open
# PR still exists, see .github/workflows/scripts/update-collector-components-check-if-pr-exists.sh.
gh api --method DELETE "repos/${GITHUB_REPOSITORY}/git/refs/heads/${branch_name}" >/dev/null 2>&1 || true

# createCommitOnBranch can only commit onto a branch that already exists. Create the PR branch at the base commit.
gh api --method POST "repos/${GITHUB_REPOSITORY}/git/refs" \
  -f ref="refs/heads/${branch_name}" \
  -f sha="${base_sha}" >/dev/null

# Let "gh api graphql"/createCommitOnBranch create the commit via the GitHub API rather than "git commit"/"git push", so
# commits are automatically signed.
additions=$(
  for file in "${changed_files[@]}"; do
    jq -n \
      --arg path "$file" \
      --arg contents "$(base64 < "$file" | tr -d '\n')" \
      '{ path: $path, contents: $contents }'
  done | jq -s '.'
)

# Note: expectedHeadOid is an optimistic lock: the branch tip must still be at base_sha (it is, we just created it).
jq -n \
  --arg repo "$GITHUB_REPOSITORY" \
  --arg branch "$branch_name" \
  --arg headline "$commit_message" \
  --arg body "$pr_body" \
  --arg oid "$base_sha" \
  --argjson additions "$additions" \
  '{
    query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
    variables: {
      input: {
        branch:          { repositoryNameWithOwner: $repo, branchName: $branch },
        message:         { headline: $headline, body: $body },
        expectedHeadOid: $oid,
        fileChanges:     { additions: $additions }
      }
    }
  }' | gh api graphql --input - >/dev/null

gh pr create \
  -B main \
  -H "$branch_name" \
  --title "$commit_message" \
  --body "$pr_body"
