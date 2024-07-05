#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${0}")"/..

# Use DRY_RUN=true to verify that the helm chart can be successfully packaged -- all steps will be executed except for
# the final git push to the gh-pages branch.
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

echo "packaging Helm chart as version $version"
helm package \
  dash0-operator \
  --version "$version" \
  --app-version "$version" \
  --dependency-update \
  --destination ..
echo "packaging Helm version $version has been packaged"

cd ..

echo "switching to gh-pages branch"
git fetch origin gh-pages:gh-pages
git switch gh-pages

# clean up potential left-overs from the --dependency-update flag
rm -rf helm-chart

echo "creating Helm chart index"
helm repo index .

echo "git add & commit"
git add "dash0-operator-$version.tgz" index.yaml
git commit -m"feat(chart): publish version $version"

if [[ "${DRY_RUN:-}" == "true" ]]; then
  echo "executing git push (--dry-run)"
  git push --no-verify --dry-run origin gh-pages
else
  echo "executing git push"
  git push --no-verify --set-upstream origin gh-pages
fi

