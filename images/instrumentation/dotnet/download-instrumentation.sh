#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo noglob

set -x

DOWNLOAD_BASE_URL=https://github.com/dash0hq/opentelemetry-dotnet-distribution/releases/download
ARTIFACT_BASE_NAME=dash0-opentelemetry-dotnet-instrumentation
OS_NAME=linux

# shellcheck source=images/instrumentation/dotnet/dash0-dotnet-distribution-version
. ./dash0-dotnet-distribution-version

case $(uname -m) in
  x86_64)  ARCHITECTURE="x64" ;;
  aarch64) ARCHITECTURE="arm64" ;;
esac

case "$ARCHITECTURE" in
  "x64"|"arm64")
    ;;
  *)
    echo "Set the architecture type using the ARCHITECTURE environment variable. Supported values: x64, arm64." >&2
    exit 1
    ;;
esac

download_and_extract() {
  libc_flavor="$1"
  platform_name="$2"
  archive_name="$ARTIFACT_BASE_NAME-$platform_name-$ARCHITECTURE.tar.gz"
  archive_url="$DOWNLOAD_BASE_URL/$DASH0_DOTNET_DISTRIBUTION_VERSION/$archive_name"
  curl -sSfL "$archive_url" -O
  mkdir "$libc_flavor"
  tar -xzf "$archive_name" -C "$libc_flavor"
}

download_and_extract musl "$OS_NAME-musl"
download_and_extract glibc "$OS_NAME"

set +x
