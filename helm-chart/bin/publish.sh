#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/..

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
  --version $version \
  --app-version $version \
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

echo "git add, commit & push"
git add "dash0-operator-$version.tgz" index.yaml
git commit -m"feat(chart): publish version $version"
git push --no-verify --set-upstream origin gh-pages
