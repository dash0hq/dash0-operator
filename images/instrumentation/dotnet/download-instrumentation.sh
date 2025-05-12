#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo noglob

set -x

DOWNLOAD_BASE_URL=https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/releases/download
ARTIFACE_BASE_NAME=opentelemetry-dotnet-instrumentation
OS_NAME=linux

# shellcheck source=images/instrumentation/dotnet/opentelemetry-dotnet-instrumentation-version
. opentelemetry-dotnet-instrumentation-version

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

download_and_unzip() {
  libc_flavor="$1"
  archive_name="$ARTIFACE_BASE_NAME-$OS_NAME-$libc_flavor-$ARCHITECTURE.zip"
  archive_url="$DOWNLOAD_BASE_URL/$OPENTELEMETRY_DOTNET_INSTRUMENTATION_VERSION/$archive_name"
  curl -sSfL "$archive_url" -O
  mkdir "$libc_flavor"
  unzip "$archive_name" -d "$libc_flavor"
}

download_and_unzip musl
download_and_unzip glibc

set +x

