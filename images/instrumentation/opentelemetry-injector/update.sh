#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

repo=open-telemetry/opentelemetry-injector
version_file=version
version_variable=OPENTELEMETRY_INJECTOR_VERSION

echo "Checking for new opentelemetry-injector releases..."

# Fetch the latest release tag from GitHub (including pre-releases)
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

# GitHub computes the SHA-256 digest of every release asset at upload time; the image build verifies the downloaded
# binary against the digest pinned here.
release_asset_digests=$(gh api "repos/${repo}/releases/tags/${latest_version}" --jq '.assets[] | "\(.name) \(.digest)"')

asset_sha256() {
  local digest
  digest=$(awk -v name="$1" '$1 == name { print $2 }' <<< "$release_asset_digests")
  if [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "Error: Release $latest_version has no SHA-256 digest for asset $1 (got: '$digest')" >&2
    exit 1
  fi
  echo "${digest#sha256:}"
}

amd64_sha256=$(asset_sha256 libotelinject_amd64.so)
arm64_sha256=$(asset_sha256 libotelinject_arm64.so)

cat > "$version_file" <<EOF
${version_variable}=${latest_version}
OPENTELEMETRY_INJECTOR_SHA256_AMD64=${amd64_sha256}
OPENTELEMETRY_INJECTOR_SHA256_ARM64=${arm64_sha256}
EOF

echo "Version file updated successfully"
