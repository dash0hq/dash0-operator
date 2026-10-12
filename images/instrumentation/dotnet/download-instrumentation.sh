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
  x86_64)
    ARCHITECTURE="x64"
    GLIBC_SHA256="${DASH0_DOTNET_DISTRIBUTION_SHA256_LINUX_X64:?}"
    MUSL_SHA256="${DASH0_DOTNET_DISTRIBUTION_SHA256_LINUX_MUSL_X64:?}"
    ;;
  aarch64)
    ARCHITECTURE="arm64"
    GLIBC_SHA256="${DASH0_DOTNET_DISTRIBUTION_SHA256_LINUX_ARM64:?}"
    MUSL_SHA256="${DASH0_DOTNET_DISTRIBUTION_SHA256_LINUX_MUSL_ARM64:?}"
    ;;
  *)
    echo "Unsupported architecture: $(uname -m). Supported values: x86_64, aarch64." >&2
    exit 1
    ;;
esac

download_verify_and_extract() {
  libc_flavor="$1"
  platform_name="$2"
  expected_sha256="$3"
  archive_name="$ARTIFACT_BASE_NAME-$platform_name-$ARCHITECTURE.tar.gz"
  archive_url="$DOWNLOAD_BASE_URL/$DASH0_DOTNET_DISTRIBUTION_VERSION/$archive_name"
  curl -sSfL "$archive_url" -O
  echo "$expected_sha256  $archive_name" | sha256sum -c -
  mkdir "$libc_flavor"
  tar -xzf "$archive_name" -C "$libc_flavor"
  # remove unnecessary debug symbols files
  find "$libc_flavor" -type f \( -name '*.debug' -o -name '*.pdb' \) -delete
  rm -f "$libc_flavor/instrument.sh"
}

download_verify_and_extract musl "$OS_NAME-musl" "$MUSL_SHA256"
download_verify_and_extract glibc "$OS_NAME" "$GLIBC_SHA256"

set +x
