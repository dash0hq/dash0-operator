#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Renders the file-based OLM catalog of the package dash0-operator with the bundle images of CATALOG_BUNDLE_IMAGES
# (space-separated, preferably by digest) into CATALOG_DIR/catalog and validates it. The catalog is rendered from an
# olm.semver template with the major channel stable-v0. Use "make olm-catalog" instead of calling this directly. Set
# CATALOG_REGISTRY_USE_HTTP=true if the registry of the bundle images only serves plain HTTP, like the local registry of
# the kind setup.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

opm="${OPM:?OPM is required}"
catalog_dir="${CATALOG_DIR:?CATALOG_DIR is required}"
bundle_images=()
read -r -d '' -a bundle_images <<< "${CATALOG_BUNDLE_IMAGES:-}" || true
if [[ ${#bundle_images[@]} -eq 0 ]]; then
  echo "error: CATALOG_BUNDLE_IMAGES must contain at least one bundle image reference"
  exit 1
fi

# CATALOG_DIR is replaced as a whole, so an existing CATALOG_DIR must only hold the files of a previous catalog.
if [[ -L "$catalog_dir" ]]; then
  echo "error: CATALOG_DIR $catalog_dir is a symbolic link"
  exit 1
fi
if [[ -e "$catalog_dir" ]]; then
  if [[ ! -d "$catalog_dir" ]]; then
    echo "error: CATALOG_DIR $catalog_dir is not a directory"
    exit 1
  fi
  for entry in "$catalog_dir"/* "$catalog_dir"/.[!.]* "$catalog_dir"/..?*; do
    if [[ ! -e "$entry" && ! -L "$entry" ]]; then
      continue
    fi
    case "$(basename -- "$entry")" in
      catalog | semver-template.yaml) ;;
      *)
        echo "error: CATALOG_DIR $catalog_dir contains $(basename -- "$entry"), which is not part of an OLM catalog"
        exit 1
        ;;
    esac
  done
fi
rm -rf "$catalog_dir"
package_dir="$catalog_dir/catalog/dash0-operator"
mkdir -p "$package_dir"

template="$catalog_dir/semver-template.yaml"
{
  echo "Schema: olm.semver"
  echo "GenerateMajorChannels: true"
  echo "GenerateMinorChannels: false"
  echo "Stable:"
  echo "  Bundles:"
  for image in "${bundle_images[@]}"; do
    echo "  - Image: $image"
  done
} > "$template"

render_args=(alpha render-template semver "$template" -o yaml --migrate-level bundle-object-to-csv-metadata)
if [[ "${CATALOG_REGISTRY_USE_HTTP:-}" = "true" ]]; then
  render_args+=(--use-http)
fi
echo "rendering the catalog with ${#bundle_images[@]} bundle image(s)"
"$opm" "${render_args[@]}" > "$package_dir/catalog.yaml"

echo "validating the catalog"
"$opm" validate "$catalog_dir/catalog"

# opm ignores --migrate-level for a bundle that it renders from a directory instead of an image. Without the migration,
# the catalog holds every manifest of every bundle (olm.bundle.object) and grows by about 700 KB per release.
grep_status=0
grep -n -- 'olm\.bundle\.object' "$package_dir/catalog.yaml" || grep_status=$?
if [[ "$grep_status" != 1 ]]; then
  echo "error: the catalog must not contain olm.bundle.object properties (grep exit code $grep_status)"
  exit 1
fi

echo "the catalog has been written to $catalog_dir/catalog/, the template to $template"
