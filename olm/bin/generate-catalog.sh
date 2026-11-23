#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Renders the file-based OLM catalog of the package dash0-operator with the bundle images of CATALOG_BUNDLE_IMAGES
# (space-separated, preferably by digest) into CATALOG_DIR/catalog and validates it. The catalog is rendered from an
# olm.semver template with the major channel stable-v0. If CATALOG_UPGRADE_FROM_BUNDLE_IMAGE is set,
# CATALOG_BUNDLE_IMAGES must hold exactly one bundle image, and the catalog has an upgrade edge in the channel stable-v0
# from the bundle of CATALOG_UPGRADE_FROM_BUNDLE_IMAGE to that bundle (olm.template.basic, needs jq). Use
# "make olm-catalog" instead of calling this directly. Set CATALOG_REGISTRY_USE_HTTP=true if the registry of the bundle
# images only serves plain HTTP, like the local registry of the kind setup.

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
upgrade_from_image="${CATALOG_UPGRADE_FROM_BUNDLE_IMAGE:-}"
if [[ -n "$upgrade_from_image" && ${#bundle_images[@]} -ne 1 ]]; then
  echo "error: CATALOG_BUNDLE_IMAGES must contain exactly one bundle image reference if" \
    "CATALOG_UPGRADE_FROM_BUNDLE_IMAGE is set, found ${#bundle_images[@]}"
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
      catalog | semver-template.yaml | basic-template.yaml) ;;
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

# Prints the name of the bundle in the bundle image $1, which is the name of its ClusterServiceVersion.
bundle_name() {
  local args=(render "$1" -o json)
  if [[ "${CATALOG_REGISTRY_USE_HTTP:-}" = "true" ]]; then
    args+=(--use-http)
  fi
  local name
  name="$("$opm" "${args[@]}" | jq -r 'select(.schema == "olm.bundle") | .name')"
  if [[ -z "$name" ]]; then
    echo "error: the bundle name of the image $1 could not be determined" >&2
    return 1
  fi
  echo "$name"
}

if [[ -z "$upgrade_from_image" ]]; then
  template_type=semver
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
else
  # The olm.semver template only creates upgrade edges to higher versions, but dev bundles (0.0.0-sha-<short sha>) are
  # lower than every release. OLM follows a replaces edge regardless of the versions.
  template_type=basic
  template="$catalog_dir/basic-template.yaml"
  upgrade_from_name="$(bundle_name "$upgrade_from_image")"
  upgrade_to_name="$(bundle_name "${bundle_images[0]}")"
  echo "adding the upgrade edge from $upgrade_from_name to $upgrade_to_name"
  cat > "$template" << EOF
schema: olm.template.basic
entries:
  - schema: olm.package
    name: dash0-operator
    defaultChannel: stable-v0
  - schema: olm.channel
    package: dash0-operator
    name: stable-v0
    entries:
      - name: $upgrade_from_name
      - name: $upgrade_to_name
        replaces: $upgrade_from_name
  - schema: olm.bundle
    image: $upgrade_from_image
  - schema: olm.bundle
    image: ${bundle_images[0]}
EOF
fi

render_args=(alpha render-template "$template_type" "$template" -o yaml --migrate-level bundle-object-to-csv-metadata)
if [[ "${CATALOG_REGISTRY_USE_HTTP:-}" = "true" ]]; then
  render_args+=(--use-http)
fi
echo "rendering the catalog from the $template_type template $template"
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
