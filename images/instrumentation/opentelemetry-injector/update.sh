#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

echo "Checking for new opentelemetry-injector releases..."

# Fetch the latest release tag from GitHub (including pre-releases)
latest_version=$(gh release list --repo open-telemetry/opentelemetry-injector --limit 1 --json tagName --jq '.[0].tagName')

if [[ -z "$latest_version" ]] || [[ "$latest_version" = "null" ]]; then
  echo "Error: Could not fetch latest release from GitHub" >&2
  exit 1
fi

echo "Latest release: $latest_version"

if [[ ! "$latest_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
  echo "Error: Invalid version format: $latest_version" >&2
  exit 1
fi

# Read current version
current_version=$(cat version)
echo "Current version: $current_version"

# Compare versions
if [[ "$current_version" = "$latest_version" ]]; then
  echo "Already up to date"
  exit 0
fi

echo "Updating version from $current_version to $latest_version"
echo "$latest_version" > version

echo "Version file updated successfully"
