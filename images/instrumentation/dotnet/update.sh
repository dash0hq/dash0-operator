#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

version_file=dash0-dotnet-distribution-version
version_variable=DASH0_DOTNET_DISTRIBUTION_VERSION

echo "Checking for new opentelemetry-dotnet-distribution releases..."

latest_version=$(gh release list --repo dash0hq/opentelemetry-dotnet-distribution --limit 1 --json tagName --jq '.[0].tagName')

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
echo "${version_variable}=${latest_version}" > "$version_file"

echo "Version file updated successfully"
