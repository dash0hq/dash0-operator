#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Prints the bundle images for the self-hosted OLM catalog as digest references, one per line, as input for
# CATALOG_BUNDLE_IMAGES of "make olm-catalog". Bundles are referenced by digest, so that a tag that is moved later
# (e.g. main-dev) does not change a catalog that has been built. Requires docker with buildx and jq.
#
# Usage:
#   olm/bin/resolve-catalog-bundles.sh image <image reference with tag>
#     Resolves a single bundle image, e.g. ghcr.io/dash0hq/dash0-operator-bundle:main-dev.
#   olm/bin/resolve-catalog-bundles.sh release <version>
#     Resolves the bundle images of all release tags (x.y.z) from OLM_CATALOG_FIRST_VERSION up to <version>. Prints no
#     image and only the reason on stderr, with exit code 0, if no release catalog is built for <version>: when
#     OLM_CATALOG_FIRST_VERSION is empty, when <version> is not a release version (e.g. a release candidate), when
#     <version> is not the highest release tag, or when it is lower than OLM_CATALOG_FIRST_VERSION.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

bundle_repository=ghcr.io/dash0hq/dash0-operator-bundle
release_pattern='^[0-9]+\.[0-9]+\.[0-9]+$'

usage() {
  echo "usage: $0 image <image reference with tag> | release <version>" >&2
  exit 1
}

skip() {
  echo "skip: $*" >&2
  exit 0
}

# Succeeds if the version $1 is lower than the version $2.
version_lower() {
  [[ "$1" != "$2" && "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n 1)" == "$1" ]]
}

# Prints the digest reference (<repository>@sha256:...) of the image reference with tag $1.
resolve_digest() {
  local reference="$1"
  local manifest
  local digest
  if [[ "$reference" == *@* || "${reference##*/}" != *:* ]]; then
    echo "error: $reference is not an image reference with a tag" >&2
    return 1
  fi
  if ! manifest="$(docker buildx imagetools inspect "$reference" --format '{{json .Manifest}}')"; then
    echo "error: the image $reference could not be resolved" >&2
    return 1
  fi
  digest="$(jq -r '.digest' <<< "$manifest")"
  if [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "error: the digest of the image $reference could not be determined, found \"$digest\"" >&2
    return 1
  fi
  echo "${reference%:*}@$digest"
}

case "${1:-}" in
  image)
    if [[ $# -ne 2 ]]; then
      usage
    fi
    resolve_digest "$2"
    ;;

  release)
    if [[ $# -ne 2 ]]; then
      usage
    fi
    version="$2"
    first_version="${OLM_CATALOG_FIRST_VERSION:-}"
    if [[ -z "$first_version" ]]; then
      skip "OLM_CATALOG_FIRST_VERSION is empty, the self-hosted catalog is not published yet"
    fi
    if [[ ! "$first_version" =~ $release_pattern ]]; then
      echo "error: OLM_CATALOG_FIRST_VERSION $first_version is not a release version (x.y.z)" >&2
      exit 1
    fi
    if [[ ! "$version" =~ $release_pattern ]]; then
      skip "$version is not a release version (x.y.z)"
    fi

    all_tags="$(git tag --list)"
    release_tags=()
    while IFS= read -r tag; do
      if [[ "$tag" =~ $release_pattern ]]; then
        release_tags+=("$tag")
      fi
    done < <(sort -V <<< "$all_tags")
    version_is_tagged=false
    for tag in ${release_tags[@]+"${release_tags[@]}"}; do
      if [[ "$tag" == "$version" ]]; then
        version_is_tagged=true
      fi
    done
    if [[ "$version_is_tagged" != true ]]; then
      echo "error: the git tag $version does not exist, the release tags are fetched with fetch-depth: 0" >&2
      exit 1
    fi
    highest_version="${release_tags[${#release_tags[@]}-1]}"
    if [[ "$version" != "$highest_version" ]]; then
      skip "$version is not the highest release tag, which is $highest_version"
    fi
    if version_lower "$version" "$first_version"; then
      skip "$version is lower than OLM_CATALOG_FIRST_VERSION $first_version"
    fi

    for tag in "${release_tags[@]}"; do
      if version_lower "$tag" "$first_version"; then
        continue
      fi
      if ! resolve_digest "$bundle_repository:$tag"; then
        echo "error: the bundle image of the release $tag could not be resolved. If it does not exist, re-run the CI" \
          "run of the tag $tag, which pushes it, and then this job, which publishes the catalog of $version." >&2
        exit 1
      fi
    done
    ;;

  *)
    usage
    ;;
esac
