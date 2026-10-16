#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

repo=dash0hq/opentelemetry-python-distribution
version_file=dash0-python-distribution-version
version_variable=DASH0_PYTHON_DISTRIBUTION_VERSION

echo "Checking for new opentelemetry-python-distribution releases..."

latest_version=$(gh release list --repo "$repo" --limit 1 --json tagName --jq '.[0].tagName')

if [[ -z "$latest_version" ]] || [[ "$latest_version" = "null" ]]; then
  echo "Error: Could not fetch latest release from GitHub" >&2
  exit 1
fi

echo "Latest release: $latest_version"

if [[ ! "$latest_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
  echo "Error: Invalid version format: $latest_version" >&2
  exit 1
fi

current_version=$(sed -n "s/^${version_variable}=//p" "$version_file")
if [[ -z "$current_version" ]]; then
  echo "Error: Could not read the current version from $version_file" >&2
  exit 1
fi
echo "Current version: $current_version"

if [[ "$current_version" = "$latest_version" ]]; then
  echo "Already up to date"
  exit 0
fi

echo "Updating version from $current_version to $latest_version"

# GitHub publishes no digest for auto-generated source archives, so the pin is the SHA-256 of the archive as served
# right now, fetched from the same URL the image build downloads.
archive_sha256=$(curl -sSfL "https://github.com/${repo}/archive/${latest_version}.tar.gz" | shasum -a 256 | cut -d ' ' -f 1)
if [[ ! "$archive_sha256" =~ ^[0-9a-f]{64}$ ]]; then
  echo "Error: Could not compute the SHA-256 of the source archive for $latest_version" >&2
  exit 1
fi

cat > "$version_file" <<EOF
${version_variable}=${latest_version}
DASH0_PYTHON_DISTRIBUTION_SHA256=${archive_sha256}
EOF

echo "Version file updated successfully"
