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

branch_name="update-synthetics-worker-image"
values_file="helm-chart/dash0-operator/values.yaml"
test_file="helm-chart/dash0-operator/tests/operator/deployment-and-webhooks_test.yaml"
snapshot_file="helm-chart/dash0-operator/tests/operator/__snapshot__/deployment-and-webhooks_test.yaml.snap"

img="dash0-synthetics-worker"
yaml_key="syntheticsWorkerImage"

# Resolve the highest published MAJOR.MINOR.PATCH tag for a ghcr.io/dash0hq image via the OCI
# registry tags/list endpoint. Prints the tag to stdout; all diagnostics go to stderr.
# Legacy v-prefixed build tags (e.g. v2.0.3005) are ignored; only unprefixed semver releases count.
# Keep in sync with the copy in update-sce-images-create-pr.sh.
resolve_latest_tag() {
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
  latest=$(echo "$all_tags" | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)
  if [[ -z "$latest" ]]; then
    echo "Error: no tag matching MAJOR.MINOR.PATCH found for ${img}." >&2
    exit 1
  fi
  echo "$latest"
}

# Replace the tag value within the "<key>:" block, leaving comments and formatting untouched.
# The block spans from the key line to its trailing "pullPolicy:" line.
update_tag() {
  local new_tag="$1"
  sed -i.bak "/^  ${yaml_key}:/,/pullPolicy:/ s|^\([[:space:]]*tag:[[:space:]]*\).*|\1\"${new_tag}\"|" "$values_file"
  rm -f "${values_file}.bak"
}

# Rewrite the expected image references in the Helm chart unit tests and their snapshots, which assert the pinned tag
# from values.yaml. The snapshot leaves the reference unquoted, so the match stops at whitespace rather than a quote.
# Aborts if the test file does not reference the image at all, otherwise values.yaml would be bumped while the tests
# keep asserting the previous tag.
update_expected_tag_in_tests() {
  local new_tag="$1"
  if ! grep -q "ghcr\.io/dash0hq/${img}:" "$test_file"; then
    echo "Error: no expected image reference for ${img} found in ${test_file}." >&2
    exit 1
  fi
  local f
  for f in "$test_file" "$snapshot_file"; do
    sed -i.bak "s|ghcr\.io/dash0hq/${img}:[^\"[:space:]]*|ghcr.io/dash0hq/${img}:${new_tag}|g" "$f"
    rm -f "${f}.bak"
  done
}

# Aborts if any file other than values.yaml and the Helm chart unit tests pins the image tag, since this script would
# leave such a reference behind and the bump would break the build. The one Go test excluded below pins a fixture tag it
# asserts against; it never reads values.yaml. Excluded by path rather than by *_test.go, so that a future Go test which
# does pin the real tag still trips this guard.
assert_no_other_pinned_references() {
  local other_files
  other_files=$(git grep -lE "ghcr\.io/dash0hq/${img}:v?[0-9]+" -- . ":!${values_file}" ":!${test_file}" \
    ":!${snapshot_file}" ":!internal/syntheticsworker/swresources/desired_state_test.go" || true)
  if [[ -n "$other_files" ]]; then
    echo "Error: ${img} is pinned to a tag in unexpected files, update this script to rewrite them, too:" >&2
    echo "$other_files" >&2
    exit 1
  fi
}

# Base commit that the new branch will be based on.
base_sha=$(git rev-parse HEAD)

new_tag=$(resolve_latest_tag)
current_tag=$(yq ".operator.${yaml_key}.tag" "$values_file")
# Strip surrounding quotes so the comparison works regardless of the yq flavour's scalar output.
current_tag="${current_tag%\"}"
current_tag="${current_tag#\"}"
echo "${img}: current=${current_tag}, latest=${new_tag}"

assert_no_other_pinned_references
# All files are always rewritten to the latest tag, so that an earlier manual edit which touched only one of them
# cannot leave them out of sync.
update_tag "$new_tag"
update_expected_tag_in_tests "$new_tag"

# All files are rewritten unconditionally above, so their stat information always differs from the index. The porcelain
# "git diff" is used on purpose: it ignores stat-only changes (diff.autoRefreshIndex, on by default), while the plumbing
# "git diff-files" would report a change and produce an empty commit.
# git diff --quiet exits with 1 if there were differences, exit code 0 means no differences.
if git diff --quiet -- "$values_file" "$test_file" "$snapshot_file"; then
  echo "There are no changes, everything up to date."
  exit 0
fi

echo "There are changes, creating a pull request."
commit_message="chore(deps): update the synthetics-worker image"
if [[ "$current_tag" != "$new_tag" ]]; then
  pr_body="Updates the \`${yaml_key}\` tag in ${values_file} to \`${new_tag}\`."
else
  # The commit message is a fixed string,
  # .github/workflows/scripts/update-synthetics-worker-image-check-if-pr-exists.sh matches on it, so the body has to
  # spell out that only the tests changed.
  pr_body="The image tag in ${values_file} is already up to date, this only realigns the expected image tags in the Helm chart unit tests."
fi

# Remove any branch lingering from a previous failed run (no-op if it does not exist). Note: We abort early if an open
# PR still exists, see .github/workflows/scripts/update-synthetics-worker-image-check-if-pr-exists.sh.
gh api --method DELETE "repos/${GITHUB_REPOSITORY}/git/refs/heads/${branch_name}" >/dev/null 2>&1 || true

# createCommitOnBranch can only commit onto a branch that already exists. Create the PR branch at the base commit.
gh api --method POST "repos/${GITHUB_REPOSITORY}/git/refs" \
  -f ref="refs/heads/${branch_name}" \
  -f sha="${base_sha}" >/dev/null

# The base64-encoded file contents are passed to jq via --rawfile and not via --arg: they can exceed the maximum length
# of a single command line argument (MAX_ARG_STRLEN, 128 KiB).
values_base64=$(mktemp)
test_base64=$(mktemp)
snapshot_base64=$(mktemp)
trap 'rm -f "$values_base64" "$test_base64" "$snapshot_base64"' EXIT
base64 < "$values_file" | tr -d '\n' > "$values_base64"
base64 < "$test_file" | tr -d '\n' > "$test_base64"
base64 < "$snapshot_file" | tr -d '\n' > "$snapshot_base64"

# Let "gh api graphql"/createCommitOnBranch create the commit via the GitHub API rather than "git commit"/"git push", so
# commits are automatically signed.
# Note: expectedHeadOid is an optimistic lock: the branch tip must still be at base_sha (it is, we just created it).
# Note: all files are always sent; the git diff check above guarantees that at least one of them differs.
payload=$(jq -n \
  --arg repo "$GITHUB_REPOSITORY" \
  --arg branch "$branch_name" \
  --arg headline "$commit_message" \
  --arg body "$pr_body" \
  --arg oid "$base_sha" \
  --arg valuesPath "$values_file" \
  --rawfile valuesContents "$values_base64" \
  --arg testPath "$test_file" \
  --rawfile testContents "$test_base64" \
  --arg snapshotPath "$snapshot_file" \
  --rawfile snapshotContents "$snapshot_base64" \
  '{
    query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
    variables: {
      input: {
        branch:          { repositoryNameWithOwner: $repo, branchName: $branch },
        message:         { headline: $headline, body: $body },
        expectedHeadOid: $oid,
        fileChanges:     { additions: [
                             { path: $valuesPath,   contents: $valuesContents },
                             { path: $testPath,     contents: $testContents },
                             { path: $snapshotPath, contents: $snapshotContents }
                           ] }
      }
    }
  }')
echo "$payload" | gh api graphql --input - >/dev/null

gh pr create \
  -B main \
  -H "$branch_name" \
  --title "$commit_message" \
  --body "$pr_body"
