#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

core_versions_yaml=core_versions.yaml
contrib_versions_yaml=contrib_versions.yaml
builder_config=src/builder/config.yaml

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
}

current_beta_version=$(yq '.dist.otelcol_version' "$builder_config" /dev/null) \
provider_module=$(yq '.providers[0]' "$builder_config")
current_stable_version="${provider_module#gomod: go\.opentelemetry\.io\/* v}"

curl -s https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector/refs/heads/main/versions.yaml > "$core_versions_yaml"
curl -s https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/heads/main/versions.yaml > "$contrib_versions_yaml"

trap "{ rm -f ""$core_versions_yaml""; rm -f ""$contrib_versions_yaml""; }" EXIT

new_stable_version=$(yq '.module-sets.stable.version' "$core_versions_yaml")
new_stable_version="${new_stable_version#v}"
new_beta_version=$(yq '.module-sets.beta.version' "$core_versions_yaml")
new_beta_version="${new_beta_version#v}"
new_contrib_version=$(yq '.module-sets.contrib-base.version' "$contrib_versions_yaml")
echo "Currently using versions:  $current_stable_version/$current_beta_version."
echo "Latest available versions: core: $new_stable_version/$new_beta_version, contrib: $new_contrib_version."

if [[ "$current_stable_version" != "$new_stable_version" || "$current_beta_version" != "$new_beta_version" ]]; then
  update_components
  echo
  new_beta_version="$new_beta_version" \
    yq -i \
    '.dist.otelcol_version=strenv(new_beta_version)' \
    "$builder_config"

  echo git diff:
  git --no-pager diff -- "$builder_config"
else
  echo "No update necessary, components are up to date."
fi
