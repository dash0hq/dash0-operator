#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

if ! command -v gh &> /dev/null; then
  echo "Error: the gh executable is not available." >&2
  exit 1
fi

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
branch_name="update-dash0-collector-components"
git checkout -b $branch_name

COLLECTOR_VERSIONS_OUTPUT=$(mktemp)
export COLLECTOR_VERSIONS_OUTPUT
trap 'rm -f "$COLLECTOR_VERSIONS_OUTPUT"' EXIT
.github/workflows/scripts/update-collector-components-check-and-bump-versions.sh
# shellcheck source=/dev/null
source "$COLLECTOR_VERSIONS_OUTPUT"

# git diff-files --quiet exits with 1 if there were differences, exit code 0 means no differences.
if git diff-files --quiet images/collector/src/builder/config.yaml; then
  echo "There are no changes, everything up to date."
else
  echo "There are changes, creating a pull request."
  git add images/collector/src/builder/config.yaml

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
  git commit --message "$commit_message"

  git push -u origin "$branch_name"
  gh pr create \
    -B main \
    -H $branch_name \
    --title "$commit_message" \
    --body "$pr_body"
fi
