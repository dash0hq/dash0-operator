#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

for executable in gh jq base64; do
  if ! command -v "$executable" &> /dev/null; then
    echo "Error: the $executable executable is not available." >&2
    exit 1
  fi
done

cd "$(dirname "${BASH_SOURCE[0]}")"/..

# Use DRY_RUN=true to verify that the helm chart can be successfully packaged -- all steps will be executed except for
# the final commit to the gh-pages branch.
#
# For testing the script locally, provide the relative path to a directory with a separate clone of the
# repository, as seen from the helm-chart directory via TEST_PUBLISH_DIR. For example:
# TEST_PUBLISH_DIR=../../dash0-operator-helm-chart-publish-test bin/publish.sh 9.9.9
# The main branch should be checked out initially in that clone.
# You could theoretically use your main working copy/clone for testing, but since the script switches the branch to
# gh-pages and that is an orphan branch it might leave the working copy in a slightly messy state. Therefore it is
# better to use another clone for that.
# Additionally, a non-empty TEST_PUBLISH_DIR implies DRY_RUN=true.
if [[ -n "${TEST_PUBLISH_DIR:-}" ]]; then
  cd  "$TEST_PUBLISH_DIR"/helm-chart
  DRY_RUN=true
fi

# Abort when there are local changes in the repository. This is mostly relevant for testing the script locally.
if ! git diff --quiet --exit-code; then
  echo "error: The repository has local changes, aborting."
  git --no-pager diff
  exit 1
fi

version=${1:-}

if [[ -z $version ]]; then
  echo "Mandatory parameter version is missing."
  echo "Usage: $0 <version>"
  exit 1
fi

# Replace relative links in the main chart README (e.g. "docs/installation.md" or "values.yaml") with absolute links to
# the GitHub repository, pinned to the version tag being released. Relative links do not resolve on
# https://artifacthub.io/packages/helm/dash0-operator/dash0-operator, which renders this README. The README is restored
# to its original state after packaging (see below), so this change is never committed back to the source branch.
readme="dash0-operator/README.md"
link_base="https://github.com/dash0hq/dash0-operator/blob/$version/helm-chart/dash0-operator/"
echo "rewriting relative links in $readme to absolute links pinned to version $version"
perl -i -pe "s{\]\((?!https?://|#|mailto:)([^)]+)\)}{](${link_base}\$1)}g" "$readme"

echo "packaging Helm chart as version $version"
helm package \
  dash0-operator \
  --version "$version" \
  --app-version "$version" \
  --dependency-update \
  --destination ..
echo "packaging Helm version $version has been packaged"

# Restore the README to its original (relative-link) state now that the chart has been packaged, so the modification
# does not interfere with the upcoming switch to the gh-pages branch.
echo "restoring $readme"
git checkout -- "$readme"

cd ..

echo "switching to gh-pages branch"
git fetch origin gh-pages:gh-pages
git switch gh-pages

# clean up potential left-overs from the --dependency-update flag
rm -rf helm-chart

echo "creating Helm chart index"
helm repo index .

chart_archive="dash0-operator-$version.tgz"

# Commit that the new commit will be based on: the current tip of gh-pages.
base_sha=$(git rev-parse HEAD)

# The base64-encoded file contents are passed to jq via --rawfile and not via --arg: they exceed the maximum length of a
# single command line argument (MAX_ARG_STRLEN, 128 KiB).
chart_base64=$(mktemp)
index_base64=$(mktemp)
trap 'rm -f "$chart_base64" "$index_base64"' EXIT
base64 -w0 "$chart_archive" > "$chart_base64"
base64 -w0 index.yaml > "$index_base64"

# Let "gh api graphql"/createCommitOnBranch create the commit via the GitHub API rather than "git commit"/"git push", so
# commits are automatically signed.
# Note: expectedHeadOid is an optimistic lock: gh-pages must still be at the commit we just fetched.
payload=$(jq -n \
  --arg repo "${GITHUB_REPOSITORY:-}" \
  --arg headline "feat(chart): publish version $version" \
  --arg oid "$base_sha" \
  --arg chartPath "$chart_archive" \
  --rawfile chartContents "$chart_base64" \
  --rawfile indexContents "$index_base64" \
  '{
    query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
    variables: {
      input: {
        branch:          { repositoryNameWithOwner: $repo, branchName: "gh-pages" },
        message:         { headline: $headline },
        expectedHeadOid: $oid,
        fileChanges:     { additions: [
          { path: $chartPath,   contents: $chartContents },
          { path: "index.yaml", contents: $indexContents }
        ] }
      }
    }
  }')

if [[ "${DRY_RUN:-}" = "true" ]]; then
  echo "skipping commit to gh-pages (dry run)"
else
  echo "committing $chart_archive and index.yaml to gh-pages"
  echo "$payload" | gh api graphql --input - > /dev/null
fi

