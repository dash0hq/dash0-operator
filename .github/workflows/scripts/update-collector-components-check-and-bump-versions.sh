#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

if ! command -v curl &> /dev/null; then
  echo "Error: the curl executable is not available."
  exit 1
fi
if ! command -v git &> /dev/null; then
  echo "Error: the git executable is not available."
  exit 1
fi
if ! command -v yq &> /dev/null; then
  echo "Error: the yq executable is not available."
  exit 1
fi

cd "$(dirname "${BASH_SOURCE[0]}")/../../.."

core_versions_yaml=core_versions.yaml
contrib_versions_yaml=contrib_versions.yaml
builder_config=images/collector/src/builder/config.yaml

component_types=( \
  connectors \
  extensions \
  exporters \
  receivers \
  processors \
  providers \
)

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
}

current_beta_version=$(\
  yq \
  '.connectors[0].gomod | match(" v(\d+\.\d+\.\d+)$"; "g") | .captures[0].string' \
  "$builder_config")
current_stable_version=$(\
    yq \
    '.providers[0].gomod | match(" v(\d+\.\d+\.\d+)$"; "g") | .captures[0].string' \
    "$builder_config")

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
echo "Currently using versions:  $current_stable_version/$current_beta_version."
echo "Latest available versions: core: $new_stable_version/$new_beta_version, contrib: $new_contrib_version."

semver_regex='^([0-9]+)\.([0-9]+)\.[0-9]+$'
if [[ ! "$new_beta_version" =~ $semver_regex ]]; then
  echo "Error: cannot parse new_beta_version \"$new_beta_version\" as a semver string."
  exit 1
fi
new_beta_major="${BASH_REMATCH[1]}"
new_beta_minor="${BASH_REMATCH[2]}"
if [[ ! "$new_contrib_version" =~ $semver_regex ]]; then
  echo "Error: cannot parse new_contrib_version \"$new_contrib_version\" as a semver string."
  exit 1
fi
new_contrib_major="${BASH_REMATCH[1]}"
new_contrib_minor="${BASH_REMATCH[2]}"
if [[ "$new_beta_major" != "$new_contrib_major" || "$new_beta_minor" != "$new_contrib_minor" ]]; then
  echo "The major/minor version of new_beta_version ($new_beta_version) and new_contrib_version ($new_contrib_version) do not match, skipping update for now. This usually means that the core components have already been released, but the contrib components have not been released yet."
  exit 0
fi

if [[ "$current_stable_version" != "$new_stable_version" || "$current_beta_version" != "$new_beta_version" ]]; then
  update_components
  echo
  echo git diff:
  git --no-pager diff -- "$builder_config"
  if [[ -f "${COLLECTOR_VERSIONS_OUTPUT:-}" ]]; then
    {
      echo "new_stable_version=$new_stable_version"
      echo "new_beta_version=$new_beta_version"
      echo "new_contrib_version=$new_contrib_version"
    } >> "$COLLECTOR_VERSIONS_OUTPUT"
  fi

else
  echo "No update necessary, components are up to date."
fi
