#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

for cmd in gh curl jq yq; do
  if ! command -v "$cmd" &> /dev/null; then
    echo "Error: the $cmd executable is not available." >&2
    exit 1
  fi
done

branch_name="update-sce-images"
values_file="helm-chart/dash0-operator/values.yaml"
test_file="helm-chart/dash0-operator/tests/operator/deployment-and-webhooks_test.yaml"

# image (ghcr repository name) -> Helm chart values key under .operator
image_specs=(
  "signal-control-collector:signalControlCollectorImage"
  "edge-proxy:edgeProxyImage"
)

# Resolve the highest published vMAJOR.MINOR.PATCH tag for a ghcr.io/dash0hq image via the OCI
# registry tags/list endpoint. Prints the tag to stdout; all diagnostics go to stderr.
resolve_latest_tag() {
  local img="$1"
  local token
  token=$(curl -fsSL "https://ghcr.io/token?scope=repository:dash0hq/${img}:pull" | jq -r '.token')
  if [[ -z "$token" || "$token" == "null" ]]; then
    echo "Error: could not obtain a pull token for ${img}." >&2
    exit 1
  fi

  # The registry returns tags in lexicographic order and paginates via the RFC 5988 Link header, so
  # all pages are collected before sorting.
  local url="https://ghcr.io/v2/dash0hq/${img}/tags/list?n=1000"
  local all_tags=""
  while [[ -n "$url" ]]; do
    local headers_file body next
    headers_file=$(mktemp)
    body=$(curl -fsSL -H "Authorization: Bearer ${token}" -D "$headers_file" "$url")
    all_tags+=$'\n'$(echo "$body" | jq -r '.tags[]?')
    next=$(grep -i '^link:' "$headers_file" | sed -n 's/.*<\([^>]*\)>; *rel="next".*/\1/p' || true)
    rm -f "$headers_file"
    if [[ -n "$next" && "$next" == /* ]]; then
      url="https://ghcr.io${next}"
    else
      url="$next"
    fi
  done

  local latest
  latest=$(echo "$all_tags" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)
  if [[ -z "$latest" ]]; then
    echo "Error: no tag matching vMAJOR.MINOR.PATCH found for ${img}." >&2
    exit 1
  fi
  echo "$latest"
}

# Replace the tag value within a single "<key>:" block, leaving comments and formatting untouched.
# The block spans from the key line to its trailing "pullPolicy:" line.
update_tag() {
  local yaml_key="$1" new_tag="$2"
  sed -i.bak "/^  ${yaml_key}:/,/pullPolicy:/ s|^\([[:space:]]*tag:[[:space:]]*\).*|\1\"${new_tag}\"|" "$values_file"
  rm -f "${values_file}.bak"
}

# Rewrite the expected image references in the Helm chart unit tests, which assert the pinned tags from values.yaml.
# The whole quoted value is replaced, so a tag that does not look like vMAJOR.MINOR.PATCH cannot end up half-rewritten.
# Aborts if the test file does not reference the image at all, otherwise values.yaml would be bumped while the tests
# keep asserting the previous tag.
update_expected_tag_in_tests() {
  local img="$1" new_tag="$2"
  if ! grep -q "ghcr\.io/dash0hq/${img}:" "$test_file"; then
    echo "Error: no expected image reference for ${img} found in ${test_file}." >&2
    exit 1
  fi
  sed -i.bak "s|\"ghcr\.io/dash0hq/${img}:[^\"]*\"|\"ghcr.io/dash0hq/${img}:${new_tag}\"|g" "$test_file"
  rm -f "${test_file}.bak"
}

# Aborts if any file other than values.yaml and the Helm chart unit tests pins one of the image tags, since this script
# would leave such a reference behind and the bump would break the build.
assert_no_other_pinned_references() {
  local img="$1"
  local other_files
  other_files=$(git grep -lE "ghcr\.io/dash0hq/${img}:v[0-9]+" -- . ":!${values_file}" ":!${test_file}" || true)
  if [[ -n "$other_files" ]]; then
    echo "Error: ${img} is pinned to a tag in unexpected files, update this script to rewrite them, too:" >&2
    echo "$other_files" >&2
    exit 1
  fi
}

# Base commit that the new branch will be based on.
base_sha=$(git rev-parse HEAD)

changes=()
for spec in "${image_specs[@]}"; do
  img="${spec%%:*}"
  key="${spec#*:}"
  new_tag=$(resolve_latest_tag "$img")
  current_tag=$(yq ".operator.${key}.tag" "$values_file")
  # Strip surrounding quotes so the comparison works regardless of the yq flavour's scalar output.
  current_tag="${current_tag%\"}"
  current_tag="${current_tag#\"}"
  echo "${img}: current=${current_tag}, latest=${new_tag}"
  assert_no_other_pinned_references "$img"
  # Both files are always rewritten to the latest tag, so that an earlier manual edit which touched only one of them
  # cannot leave the two out of sync.
  update_tag "$key" "$new_tag"
  update_expected_tag_in_tests "$img" "$new_tag"
  if [[ "$current_tag" != "$new_tag" ]]; then
    changes+=("\`${img}\` to \`${new_tag}\`")
  fi
done

# Both files are rewritten unconditionally above, so their stat information always differs from the index. The porcelain
# "git diff" is used on purpose: it ignores stat-only changes (diff.autoRefreshIndex, on by default), while the plumbing
# "git diff-files" would report a change and produce an empty commit.
# git diff --quiet exits with 1 if there were differences, exit code 0 means no differences.
if git diff --quiet -- "$values_file" "$test_file"; then
  echo "There are no changes, everything up to date."
  exit 0
fi

echo "There are changes, creating a pull request."
commit_message="chore(deps): update Signal Control and Edge Proxy images"
if [[ ${#changes[@]} -gt 0 ]]; then
  pr_body="Updates the following image tags in ${values_file}:"
  for change in "${changes[@]}"; do
    pr_body+=$'\n'"- ${change}"
  done
else
  # The commit message is a fixed string, .github/workflows/scripts/update-sce-images-check-if-pr-exists.sh matches on
  # it, so the body has to spell out that only the tests changed.
  pr_body="The image tags in ${values_file} are already up to date, this only realigns the expected image tags in ${test_file}."
fi

# Remove any branch lingering from a previous failed run (no-op if it does not exist). Note: We abort early if an open
# PR still exists, see .github/workflows/scripts/update-sce-images-check-if-pr-exists.sh.
gh api --method DELETE "repos/${GITHUB_REPOSITORY}/git/refs/heads/${branch_name}" >/dev/null 2>&1 || true

# createCommitOnBranch can only commit onto a branch that already exists. Create the PR branch at the base commit.
gh api --method POST "repos/${GITHUB_REPOSITORY}/git/refs" \
  -f ref="refs/heads/${branch_name}" \
  -f sha="${base_sha}" >/dev/null

# Let "gh api graphql"/createCommitOnBranch create the commit via the GitHub API rather than "git commit"/"git push", so
# commits are automatically signed.
# Note: expectedHeadOid is an optimistic lock: the branch tip must still be at base_sha (it is, we just created it).
# Note: both files are always sent; the git diff check above guarantees that at least one of them differs.
jq -n \
  --arg repo "$GITHUB_REPOSITORY" \
  --arg branch "$branch_name" \
  --arg headline "$commit_message" \
  --arg body "$pr_body" \
  --arg oid "$base_sha" \
  --arg valuesPath "$values_file" \
  --arg valuesContents "$(base64 < "$values_file" | tr -d '\n')" \
  --arg testPath "$test_file" \
  --arg testContents "$(base64 < "$test_file" | tr -d '\n')" \
  '{
    query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
    variables: {
      input: {
        branch:          { repositoryNameWithOwner: $repo, branchName: $branch },
        message:         { headline: $headline, body: $body },
        expectedHeadOid: $oid,
        fileChanges:     { additions: [
                             { path: $valuesPath, contents: $valuesContents },
                             { path: $testPath,   contents: $testContents }
                           ] }
      }
    }
  }' | gh api graphql --input - >/dev/null

gh pr create \
  -B main \
  -H "$branch_name" \
  --title "$commit_message" \
  --body "$pr_body"
