#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Generates the OLM bundle (bundle/ and bundle.Dockerfile) from the Helm chart. All arguments are passed on to
# helm template, for example --set operator.image.tag=1.2.3. Use "make olm-bundle" instead of calling this directly.

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
cd "$project_root"

bundle_version="${BUNDLE_VERSION:?BUNDLE_VERSION is required}"
bundle_channels="${BUNDLE_CHANNELS:-stable}"
bundle_default_channel="${BUNDLE_DEFAULT_CHANNEL:-stable}"
operator_sdk="${OPERATOR_SDK:-operator-sdk}"

bundle_dir=bundle
work_dir=olm/build
csv_base=config/manifests/bases/dash0-operator.clusterserviceversion.yaml
# OLM installs the operator into the namespace of the Subscription. The chart is rendered into this namespace only to
# check that no namespace reaches the bundle.
namespace_sentinel=olm-bundle-namespace-sentinel

rm -rf "$bundle_dir" bundle.Dockerfile "$work_dir"
mkdir -p "$work_dir"

echo "rendering the Helm chart"
helm template dash0-operator helm-chart/dash0-operator \
  --namespace "$namespace_sentinel" \
  --values olm/values-olm.yaml \
  "$@" \
  > "$work_dir/chart.yaml"

echo "transforming the rendered chart and the CSV base"
{
  cat "$work_dir/chart.yaml"
  echo "---"
  cat "$csv_base"
} | go run ./olm/manifest-transformer --version "$bundle_version" > "$work_dir/manifests.yaml"

echo "generating the bundle"
{
  cat "$work_dir/manifests.yaml"
  for f in config/samples/*.yaml config/scorecard/config.yaml; do
    echo "---"
    cat "$f"
  done
} | tee "$work_dir/bundle-input.yaml" | "$operator_sdk" generate bundle \
  --package dash0-operator \
  --version "$bundle_version" \
  --channels "$bundle_channels" \
  --default-channel "$bundle_default_channel" \
  --output-dir "$bundle_dir"

grep_status=0
grep -rn -- "$namespace_sentinel" "$bundle_dir" bundle.Dockerfile || grep_status=$?
if [[ "$grep_status" != 1 ]]; then
  echo "error: the bundle must not contain the namespace the chart is rendered into (grep exit code $grep_status)"
  exit 1
fi

echo "the bundle has been written to $bundle_dir/ and bundle.Dockerfile, intermediate files are in $work_dir/"
