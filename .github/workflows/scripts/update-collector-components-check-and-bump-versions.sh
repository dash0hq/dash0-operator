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

# The version of the 0.x (beta) core module set that the builder config currently uses. The connectors section starts
# with a core component, hence its first entry carries the beta version.
function beta_version_from_builder_config {
  yq \
  '.connectors[0].gomod | match(" v(\d+\.\d+\.\d+)$"; "g") | .captures[0].string' \
  "$builder_config"
}

# The version of the 1.x (stable) core module set that the builder config currently uses. The providers section only
# contains core components, hence its first entry carries the stable version.
function stable_version_from_builder_config {
  yq \
  '.providers[0].gomod | match(" v(\d+\.\d+\.\d+)$"; "g") | .captures[0].string' \
  "$builder_config"
}

function update_components {
  echo "Updating components to new version:"
  echo "- new_stable_version: $new_stable_version"
  echo "- new_beta_version: $new_beta_version"
  echo "- new_contrib_version: $new_contrib_version"
  echo

  for component_type in "${component_types[@]}"; do
    modules=$( \
      type=".$component_type" \
      yq \
      'eval(strenv(type))[] | .gomod | sub(" v\d+\.\d+\.\d+", "")' "$builder_config"
    )

    while IFS= read -r module; do

      if [[ -n $( \
        module="$module" \
        yq \
        '.module-sets.contrib-base.modules[] | select(. == strenv(module))' \
        "$contrib_versions_yaml"
      ) ]]; then
        new_version_for_this_module="$new_contrib_version"
        echo "module $module is from contrib, updating to $new_version_for_this_module"
      fi

      if [[ -n $( \
        module="$module" \
        yq \
        '.module-sets.beta.modules[] | select(. == strenv(module))' \
        "$core_versions_yaml"
      ) ]]; then
        new_version_for_this_module="$new_beta_version"
        echo "module $module is from core/beta, updating to $new_version_for_this_module"
      fi

      if [[ -n $( \
        module="$module" \
        yq \
        '.module-sets.stable.modules[] | select(. == strenv(module))' \
        "$core_versions_yaml"
      ) ]]; then
        new_version_for_this_module="$new_stable_version"
        echo "module $module is from core/stable, updating to $new_version_for_this_module"
      fi

      type=".$component_type" \
        module="$module" \
        new_version="$new_version_for_this_module" \
        yq -i \
        '(eval(strenv(type))[] | .gomod | select(test(strenv(module)))) |= strenv(module) + " v" + strenv(new_version)' \
        "$builder_config"

    done <<< "$modules"

  done

  new_version="$new_beta_version" \
    yq -i \
    '.dist.version |= strenv(new_version)' \
    "$builder_config"

  return 0
}

# update_telemetry_module updates the collector modules required by the go.mod file of the custom internal-telemetry
# factory to the component versions of the builder config. The factory is built into the collector image via the
# "telemetry" section of the builder config.
function update_telemetry_module {
  local stable_version
  local beta_version
  stable_version=$(stable_version_from_builder_config)
  beta_version=$(beta_version_from_builder_config)

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

  echo "Updating the collector modules required by $telemetry_module_dir/go.mod:"
  printf -- "- %s\n" "${module_args[@]}"
  (
    cd "$telemetry_module_dir"
    go get "${module_args[@]}"
    go mod tidy
  )

  echo
  echo "git diff:"
  git --no-pager diff --stat -- "$telemetry_module_dir"

  return 0
}

current_beta_version=$(beta_version_from_builder_config)
current_stable_version=$(stable_version_from_builder_config)

curl -s https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector/refs/heads/main/versions.yaml > "$core_versions_yaml"
curl -s https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/heads/main/versions.yaml > "$contrib_versions_yaml"

trap "{ rm -f ""$core_versions_yaml""; rm -f ""$contrib_versions_yaml""; }" EXIT

# new_stable_version is the 1.x version of stable components
# new_beta_version is the 0.x versions of components from the opentelemetry-collector repository
# new_contrib_version is the 0.x versions of components from the opentelemetry-collector-contrib repository
new_stable_version=$(yq '.module-sets.stable.version' "$core_versions_yaml")
new_stable_version="${new_stable_version#v}"
new_beta_version=$(yq '.module-sets.beta.version' "$core_versions_yaml")
new_beta_version="${new_beta_version#v}"
new_contrib_version=$(yq '.module-sets.contrib-base.version | sub("v", "")' "$contrib_versions_yaml")
echo "currently using versions:  core: $current_stable_version/$current_beta_version"
echo "latest available versions: core: $new_stable_version/$new_beta_version (contrib: $new_contrib_version)"

semver_regex='^([0-9]+)\.([0-9]+)\.[0-9]+$'
if [[ ! "$new_beta_version" =~ $semver_regex ]]; then
  echo "Error: cannot parse new_beta_version \"$new_beta_version\" as a semver string." >&2
  exit 1
fi
new_beta_major="${BASH_REMATCH[1]}"
new_beta_minor="${BASH_REMATCH[2]}"
if [[ ! "$new_contrib_version" =~ $semver_regex ]]; then
  echo "Error: cannot parse new_contrib_version \"$new_contrib_version\" as a semver string." >&2
  exit 1
fi
new_contrib_major="${BASH_REMATCH[1]}"
new_contrib_minor="${BASH_REMATCH[2]}"
if [[ "$new_beta_major" != "$new_contrib_major" || "$new_beta_minor" != "$new_contrib_minor" ]]; then
  echo "The major/minor version of new_beta_version ($new_beta_version) and new_contrib_version ($new_contrib_version) do not match, skipping update for now. This usually means that the core components have already been released, but the contrib components have not been released yet."
  exit 0
fi

components_updated=false
if [[ "$current_stable_version" != "$new_stable_version" || "$current_beta_version" != "$new_beta_version" ]]; then
  require_published_release "open-telemetry/opentelemetry-collector" "$new_beta_version"
  require_published_release "open-telemetry/opentelemetry-collector-contrib" "$new_contrib_version"
  update_components
  components_updated=true
  echo
  echo git diff:
  git --no-pager diff -- "$builder_config"
else
  echo "No update necessary, components are up to date."
fi

echo

# Runs on both paths, and always against the versions the builder config has now: when the components have just been
# updated, the module follows them, and when they were already up to date, drift that was introduced elsewhere is
# corrected.
update_telemetry_module

if [[ -f "${COLLECTOR_VERSIONS_OUTPUT:-}" ]]; then
  {
    echo "components_updated=$components_updated"
    echo "new_stable_version=$new_stable_version"
    echo "new_beta_version=$new_beta_version"
    echo "new_contrib_version=$new_contrib_version"
  } >> "$COLLECTOR_VERSIONS_OUTPUT"
fi
