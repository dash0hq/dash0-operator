#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

if ! command -v curl &> /dev/null; then
  echo "Error: the curl executable is not available." >&2
  exit 1
fi
if ! command -v gh &> /dev/null; then
  echo "Error: the gh executable is not available." >&2
  exit 1
fi
if ! command -v git &> /dev/null; then
  echo "Error: the git executable is not available." >&2
  exit 1
fi
if ! command -v go &> /dev/null; then
  echo "Error: the go executable is not available." >&2
  exit 1
fi
if ! command -v jq &> /dev/null; then
  echo "Error: the jq executable is not available." >&2
  exit 1
fi
if ! command -v yq &> /dev/null; then
  echo "Error: the yq executable is not available." >&2
  exit 1
fi

cd "$(dirname "${BASH_SOURCE[0]}")/../../.."

core_versions_yaml=core_versions.yaml
contrib_versions_yaml=contrib_versions.yaml
builder_config=images/collector/src/builder/config.yaml
telemetry_module_dir=images/collector/src/telemetry

component_types=( \
  connectors \
  extensions \
  exporters \
  receivers \
  processors \
  providers \
)

# versions.yaml on the main branch of the collector repositories is sometimes bumped several hours before the
# corresponding GitHub release (and thus the Go module tags) is actually published. This function verifies that the
# release matching the target version has actually been published before we proceed.
function require_published_release {
  local repo="$1"
  local expected_version="$2"
  local latest_release_tag
  latest_release_tag=$(gh api "repos/$repo/releases/latest" --jq '.tag_name' 2>/dev/null || true)
  if [[ -z "$latest_release_tag" ]]; then
    echo "Could not determine the latest release for $repo, skipping update for now."
    exit 0
  fi
  if [[ "$latest_release_tag" != "v$expected_version" ]]; then
    echo "Latest published release of $repo is $latest_release_tag, but versions.yaml points to v$expected_version. The release has likely not been published yet, skipping update for now."
    exit 0
  fi
}

# Checks whether the given Go module belongs to the given module set of the given versions.yaml file.
function module_is_in_module_set {
  local module="$1"
  local versions_yaml="$2"
  local module_set="$3"
  [[ -n $( \
    module="$module" \
    modules_path=".module-sets.$module_set.modules" \
    yq \
    'eval(strenv(modules_path))[] | select(. == strenv(module))' \
    "$versions_yaml"
  ) ]]
}

# Prints every gomod entry (module path plus version) of the builder config.
function gomods_from_builder_config {
  local component_type
  for component_type in "${component_types[@]}"; do
    type=".$component_type" \
      yq \
      'eval(strenv(type))[] | .gomod' \
      "$builder_config"
  done
}

# Prints the version that the builder config currently uses for the given module set. All modules of a set share one
# version, so the first module of the builder config that belongs to the set is representative. Prints nothing when the
# builder config uses no module of that set.
function version_from_builder_config {
  local versions_yaml="$1"
  local module_set="$2"
  local gomod
  while IFS= read -r gomod; do
    if module_is_in_module_set "${gomod% v*}" "$versions_yaml" "$module_set"; then
      echo "${gomod##* v}"
      return 0
    fi
  done < <(gomods_from_builder_config)
  return 0
}

# Checks whether an update is required for a module set, that is, the builder config uses modules of that set and their
# version is not the latest one.
function version_differs {
  local current_version="$1"
  local new_version="$2"
  [[ -n "$current_version" && "$current_version" != "$new_version" ]]
}

function update_components {
  echo "Updating components to new versions:"
  echo "- new_core_stable_version:    $new_core_stable_version"
  echo "- new_core_beta_version:      $new_core_beta_version"
  echo "- new_contrib_stable_version: $new_contrib_stable_version"
  echo "- new_contrib_beta_version:   $new_contrib_beta_version"
  echo

  local component_type
  local modules
  local module
  local new_version_for_this_module

  for component_type in "${component_types[@]}"; do
    modules=$( \
      type=".$component_type" \
      yq \
      'eval(strenv(type))[] | .gomod | sub(" v\d+\.\d+\.\d+", "")' "$builder_config"
    )

    while IFS= read -r module; do

      if module_is_in_module_set "$module" "$core_versions_yaml" stable; then
        new_version_for_this_module="$new_core_stable_version"
        echo "module $module is from core/stable, updating to $new_version_for_this_module"
      elif module_is_in_module_set "$module" "$core_versions_yaml" beta; then
        new_version_for_this_module="$new_core_beta_version"
        echo "module $module is from core/beta, updating to $new_version_for_this_module"
      elif module_is_in_module_set "$module" "$contrib_versions_yaml" stable-base; then
        new_version_for_this_module="$new_contrib_stable_version"
        echo "module $module is from contrib/stable, updating to $new_version_for_this_module"
      elif module_is_in_module_set "$module" "$contrib_versions_yaml" contrib-base; then
        new_version_for_this_module="$new_contrib_beta_version"
        echo "module $module is from contrib/beta, updating to $new_version_for_this_module"
      else
        echo "Error: the module $module, used in $builder_config, is in none of the module sets of the OpenTelemetry collector and collector-contrib repositories, so the version to update it to cannot be determined. This usually means the module has been moved to a module set that is not known here, for example because it has become stable. Please extend update_components in $(basename "${BASH_SOURCE[0]}")." >&2
        exit 1
      fi

      type=".$component_type" \
        module="$module" \
        new_version="$new_version_for_this_module" \
        yq -i \
        '(eval(strenv(type))[] | .gomod | select(test(strenv(module)))) |= strenv(module) + " v" + strenv(new_version)' \
        "$builder_config"

    done <<< "$modules"

  done

  new_version="$new_core_beta_version" \
    yq -i \
    '.dist.version |= strenv(new_version)' \
    "$builder_config"

  return 0
}

curl -s https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector/refs/heads/main/versions.yaml > "$core_versions_yaml"
curl -s https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/heads/main/versions.yaml > "$contrib_versions_yaml"

trap "{ rm -f ""$core_versions_yaml""; rm -f ""$contrib_versions_yaml""; }" EXIT

# Both repositories maintain a stable (1.x) and a beta (0.x) module set, and each set is versioned independently of the
# others. A contrib component that becomes stable moves from contrib-base to stable-base and starts over at v1.x.x -
# whatever the version for the stable group is by that time. All stable modules in one repo share the same version,
# no matter when the respective module graduated to stable. Also all beta modules in one repo share the same version.
# The beta version is the same across both repos.
# - new_core_stable_version is the 1.x version of the stable components of the opentelemetry-collector repository
# - new_core_beta_version is the 0.x version of the beta components of the opentelemetry-collector repository
# - new_contrib_stable_version is the 1.x version of the stable components of the opentelemetry-collector-contrib
#   repository
# - new_contrib_beta_version is the 0.x version of the beta components of the opentelemetry-collector-contrib repository
new_core_stable_version=$(yq '.module-sets.stable.version' "$core_versions_yaml")
new_core_stable_version="${new_core_stable_version#v}"
new_core_beta_version=$(yq '.module-sets.beta.version' "$core_versions_yaml")
new_core_beta_version="${new_core_beta_version#v}"
new_contrib_stable_version=$(yq '.module-sets.stable-base.version' "$contrib_versions_yaml")
new_contrib_stable_version="${new_contrib_stable_version#v}"
new_contrib_beta_version=$(yq '.module-sets.contrib-base.version' "$contrib_versions_yaml")
new_contrib_beta_version="${new_contrib_beta_version#v}"

current_core_stable_version=$(version_from_builder_config "$core_versions_yaml" stable)
current_core_beta_version=$(version_from_builder_config "$core_versions_yaml" beta)
current_contrib_stable_version=$(version_from_builder_config "$contrib_versions_yaml" stable-base)
current_contrib_beta_version=$(version_from_builder_config "$contrib_versions_yaml" contrib-base)

echo "currently using versions:  core stable: $current_core_stable_version, core beta: $current_core_beta_version, contrib stable: $current_contrib_stable_version, contrib beta: $current_contrib_beta_version"
echo "latest available versions: core stable: $new_core_stable_version, core beta: $new_core_beta_version, contrib stable: $new_contrib_stable_version, contrib beta: $new_contrib_beta_version"

for version_variable in new_core_stable_version new_core_beta_version new_contrib_stable_version new_contrib_beta_version; do
  if [[ -z "${!version_variable}" || "${!version_variable}" == "null" ]]; then
    echo "Error: cannot determine $version_variable from the versions.yaml files of the collector repositories, a module set has probably been renamed or removed." >&2
    exit 1
  fi
done

semver_regex='^([0-9]+)\.([0-9]+)\.[0-9]+$'
if [[ ! "$new_core_beta_version" =~ $semver_regex ]]; then
  echo "Error: cannot parse new_core_beta_version \"$new_core_beta_version\" as a semver string." >&2
  exit 1
fi
new_core_beta_major="${BASH_REMATCH[1]}"
new_core_beta_minor="${BASH_REMATCH[2]}"
if [[ ! "$new_contrib_beta_version" =~ $semver_regex ]]; then
  echo "Error: cannot parse new_contrib_beta_version \"$new_contrib_beta_version\" as a semver string." >&2
  exit 1
fi
new_contrib_beta_major="${BASH_REMATCH[1]}"
new_contrib_beta_minor="${BASH_REMATCH[2]}"
if [[ "$new_core_beta_major" != "$new_contrib_beta_major" || "$new_core_beta_minor" != "$new_contrib_beta_minor" ]]; then
  echo "The major/minor version of new_core_beta_version ($new_core_beta_version) and new_contrib_beta_version ($new_contrib_beta_version) do not match, skipping update for now. This usually means that the core components have already been released, but the contrib components have not been released yet."
  exit 0
fi

components_updated=false
if version_differs "$current_core_stable_version" "$new_core_stable_version" \
  || version_differs "$current_core_beta_version" "$new_core_beta_version" \
  || version_differs "$current_contrib_stable_version" "$new_contrib_stable_version" \
  || version_differs "$current_contrib_beta_version" "$new_contrib_beta_version"; then
  require_published_release "open-telemetry/opentelemetry-collector" "$new_core_beta_version"
  require_published_release "open-telemetry/opentelemetry-collector-contrib" "$new_contrib_beta_version"
  update_components
  components_updated=true
  echo
  echo git diff:
  git --no-pager diff -- "$builder_config"
else
  echo "No update necessary, components are up to date."
fi

echo

# update_telemetry_module updates the collector modules required by the go.mod file of the custom internal-telemetry
# factory to the component versions of the builder config. The factory is built into the collector image via the
# "telemetry" section of the builder config.
function update_telemetry_module {
  local stable_version
  local beta_version
  stable_version=$(version_from_builder_config "$core_versions_yaml" stable)
  beta_version=$(version_from_builder_config "$core_versions_yaml" beta)

  local modules
  modules=$( \
    go mod edit -json "$telemetry_module_dir/go.mod" \
    | jq -r '.Require[] | select(.Indirect != true) | .Path | select(startswith("go.opentelemetry.io/collector/"))'
  )

  local module_args=()
  local new_version_for_this_module
  while IFS= read -r module; do
    if [[ -z "$module" ]]; then
      continue
    fi

    if [[ -n $( \
      module="$module" \
      yq \
      '.module-sets.stable.modules[] | select(. == strenv(module))' \
      "$core_versions_yaml"
    ) ]]; then
      new_version_for_this_module="$stable_version"
    elif [[ -n $( \
      module="$module" \
      yq \
      '.module-sets.beta.modules[] | select(. == strenv(module))' \
      "$core_versions_yaml"
    ) ]]; then
      new_version_for_this_module="$beta_version"
    else
      echo "Error: the module $module, required by $telemetry_module_dir/go.mod, is in neither the stable nor the beta module set of the OpenTelemetry collector, so the version to update it to cannot be determined. Please extend update_telemetry_module in $(basename "${BASH_SOURCE[0]}")." >&2
      exit 1
    fi

    module_args+=("$module@v$new_version_for_this_module")
  done <<< "$modules"

  if [[ ${#module_args[@]} -eq 0 ]]; then
    echo "The go.mod file of $telemetry_module_dir does not require any collector module, nothing to update."
    return 0
  fi

  # Every indirect requirement of the module comes from one of the collector modules, so the whole set is dropped and
  # re-derived from them by go mod tidy below. Without that, an indirect version that ended up above what the
  # components require - a stray "go get -u", a hand edit - would stay there forever: go mod tidy raises versions but
  # never lowers them, and the module takes part in the module resolution of the collector binary, so that version
  # would silently become the minimum for the whole binary.
  local indirect_requirements
  indirect_requirements=$( \
    go mod edit -json "$telemetry_module_dir/go.mod" \
    | jq -r '.Require[] | select(.Indirect == true) | .Path'
  )

  local drop_args=()
  while IFS= read -r module; do
    if [[ -n "$module" ]]; then
      drop_args+=("-droprequire=$module")
    fi
  done <<< "$indirect_requirements"

  echo "Updating the collector modules required by $telemetry_module_dir/go.mod:"
  printf -- "- %s\n" "${module_args[@]}"
  (
    cd "$telemetry_module_dir"
    if [[ ${#drop_args[@]} -gt 0 ]]; then
      go mod edit "${drop_args[@]}"
    fi
    go get "${module_args[@]}"
    go mod tidy
  )

  echo
  echo "git diff:"
  git --no-pager diff --stat -- "$telemetry_module_dir"

  return 0
}

# update_telemetry_module:
# - when the collector components have just been updated, the telemetry module's dependency versions are updated with
#   them
# - when the collector components were already up to date, and but the telemetry module's dependency versions are not
#   aligned, this will be reconciled
update_telemetry_module

if [[ -f "${COLLECTOR_VERSIONS_OUTPUT:-}" ]]; then
  {
    echo "components_updated=$components_updated"
    echo "new_core_stable_version=$new_core_stable_version"
    echo "new_core_beta_version=$new_core_beta_version"
    echo "new_contrib_stable_version=$new_contrib_stable_version"
    echo "new_contrib_beta_version=$new_contrib_beta_version"
  } >> "$COLLECTOR_VERSIONS_OUTPUT"
fi
