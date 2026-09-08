#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"
# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file

# Nukes the kind cluster and the local registry (container plus its storage volume) and recreates both from scratch. Use
# this to reclaim disk space when the registry volume has accumulated stale image layers, or to start over with a clean
# cluster. Since the volume is wiped, all images need to be pushed again afterwards (e.g. via
# `make all-images push-all-images`, or implicitly by running any of the test-scenario-*.sh scripts).
recreate_cluster_and_registry() {
  local reg_volume_path="${LOCAL_REGISTRY_VOLUME_PATH:-}"

  # Safety guard: never operate on an empty or root path, which would wipe unexpected data.
  if [[ -z "$reg_volume_path" || "$reg_volume_path" == "/" ]]; then
    echo "error: refusing to operate on registry volume path [path=$reg_volume_path]"
    exit 1
  fi

  "$scripts_bin/delete_cluster_and_registry.sh"

  # Wipe the registry storage volume to reclaim disk space and drop all stale image data. Delete the contents instead of
  # the directory itself, so that a symlinked volume path keeps working and the directory keeps its ownership.
  echo "Removing registry volume contents at [path=$reg_volume_path]..."
  mkdir -p "$reg_volume_path"
  find "${reg_volume_path:?}" -mindepth 1 -delete
  echo "Done."

  "$scripts_bin/create_cluster_and_registry.sh"

  echo
  echo "Cluster and registry recreated. The registry is empty."
  if [[ "${BUILD_AND_PUSH_ALL_IMAGES:-}" != "true" ]]; then
    echo "Run \"make all-images push-all-images\" to build and push all images."
  fi
}

# Builds all container images and pushes them to the registry, if BUILD_AND_PUSH_ALL_IMAGES is true.
rebuild_and_push_all_images() {
  if [[ "${BUILD_AND_PUSH_ALL_IMAGES:-}" = "true" ]]; then
    # Without a repository prefix, the images are tagged without a registry host and pushing them would target Docker
    # Hub instead of the local registry.
    if [[ -z "${IMAGE_REPOSITORY_PREFIX:-}" ]]; then
      echo "error: IMAGE_REPOSITORY_PREFIX is not set, please set it in test-resources/.env or via other means"
      exit 1
    fi
    make all-images push-all-images
  fi
}

recreate_cluster_and_registry
rebuild_and_push_all_images
