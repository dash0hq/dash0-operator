#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

repo=dash0hq/opentelemetry-dotnet-distribution
version_file=dash0-dotnet-distribution-version
version_variable=DASH0_DOTNET_DISTRIBUTION_VERSION
artifact_base_name=dash0-opentelemetry-dotnet-instrumentation

echo "Checking for new opentelemetry-dotnet-distribution releases..."

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
# archives against the digests pinned here.
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

linux_x64_sha256=$(asset_sha256 "${artifact_base_name}-linux-x64.tar.gz")
linux_arm64_sha256=$(asset_sha256 "${artifact_base_name}-linux-arm64.tar.gz")
linux_musl_x64_sha256=$(asset_sha256 "${artifact_base_name}-linux-musl-x64.tar.gz")
linux_musl_arm64_sha256=$(asset_sha256 "${artifact_base_name}-linux-musl-arm64.tar.gz")

cat > "$version_file" <<EOF
${version_variable}=${latest_version}
DASH0_DOTNET_DISTRIBUTION_SHA256_LINUX_X64=${linux_x64_sha256}
DASH0_DOTNET_DISTRIBUTION_SHA256_LINUX_ARM64=${linux_arm64_sha256}
DASH0_DOTNET_DISTRIBUTION_SHA256_LINUX_MUSL_X64=${linux_musl_x64_sha256}
DASH0_DOTNET_DISTRIBUTION_SHA256_LINUX_MUSL_ARM64=${linux_musl_arm64_sha256}
EOF

echo "Version file updated successfully"
