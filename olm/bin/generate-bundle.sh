#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Generates the OLM bundle, including its bundle.Dockerfile, into BUNDLE_DIR (default: bundle) from the Helm chart.
# All arguments are passed on to helm template, for example --set operator.image.tag=1.2.3. Use "make olm-bundle" or
# "make olm-test-bundle" instead of calling this directly. With OLM_TEST_BUNDLE=true, BUNDLE_DIR must be outside of
# bundle/, which holds the committed bundle.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

# Prints the absolute path of $1 with all symbolic links resolved, also when $1 or some of its parents do not exist yet.
absolute_path() {
  local path="$1"
  local rest=""
  local name
  if [[ "$path" != /* ]]; then
    # cd treats a path that is just "-" as the previous working directory.
    path="./$path"
  fi
  while [[ "$path" == */ && "$path" != / ]]; do
    path="${path%/}"
  done
  while [[ ! -d "$path" ]]; do
    name="$(basename -- "$path")"
    if [[ "$name" == . || "$name" == .. ]]; then
      echo "error: $1 contains . or .. after a directory that does not exist" >&2
      return 1
    fi
    rest="/$name$rest"
    path="$(dirname -- "$path")"
  done
  path="$(CDPATH='' cd -P -- "$path" && pwd -P)" || return 1
  if [[ "$path" == / && -n "$rest" ]]; then
    path=""
  fi
  echo "$path$rest"
}

# Succeeds if the absolute path $1 is the absolute path $2 or lies inside it, both as returned by absolute_path.
is_same_or_inside() {
  local path="$1"
  while true; do
    if [[ "$path" == "$2" || "$path" -ef "$2" ]]; then
      return 0
    fi
    if [[ "$path" == / ]]; then
      return 1
    fi
    path="$(dirname -- "$path")"
  done
}

bundle_version="${BUNDLE_VERSION:?BUNDLE_VERSION is required}"
bundle_channels="${BUNDLE_CHANNELS:-stable}"
bundle_default_channel="${BUNDLE_DEFAULT_CHANNEL:-stable}"
operator_sdk="${OPERATOR_SDK:-operator-sdk}"
if [[ "$operator_sdk" == */* ]]; then
  # operator-sdk runs in the staging directory, see below.
  operator_sdk_dir="$(CDPATH='' cd -- "$(dirname -- "$operator_sdk")" && pwd)"
  operator_sdk="$operator_sdk_dir/$(basename -- "$operator_sdk")"
fi

if [[ -z "${BUNDLE_DIR-bundle}" ]]; then
  echo "error: BUNDLE_DIR must not be empty"
  exit 1
fi
bundle_dir="$(absolute_path "${BUNDLE_DIR-bundle}")"
work_dir=olm/build
csv_base=config/manifests/bases/dash0-operator.clusterserviceversion.yaml
csv_file=manifests/dash0-operator.clusterserviceversion.yaml
# OLM installs the operator into the namespace of the Subscription. The chart is rendered into this namespace only to
# check that no namespace reaches the bundle.
namespace_sentinel=olm-bundle-namespace-sentinel

# The bundle is generated into a staging directory and only replaces BUNDLE_DIR when all steps have succeeded.
staging_dir="$work_dir/staging-bundle"
mkdir -p "$staging_dir"
# BUNDLE_DIR is replaced as a whole, so an existing BUNDLE_DIR must only hold the files of a previous bundle.
if [[ -L "$bundle_dir" ]]; then
  echo "error: BUNDLE_DIR $bundle_dir is a symbolic link to a file or to a path that does not exist"
  exit 1
fi
if [[ -e "$bundle_dir" ]]; then
  if [[ ! -d "$bundle_dir" ]]; then
    echo "error: BUNDLE_DIR $bundle_dir is not a directory"
    exit 1
  fi
  for entry in "$bundle_dir"/* "$bundle_dir"/.[!.]* "$bundle_dir"/..?*; do
    if [[ ! -e "$entry" && ! -L "$entry" ]]; then
      continue
    fi
    case "$(basename -- "$entry")" in
      bundle.Dockerfile | manifests | metadata | tests) ;;
      *)
        echo "error: BUNDLE_DIR $bundle_dir contains $(basename -- "$entry"), which is not part of an OLM bundle"
        exit 1
        ;;
    esac
  done
fi
staging_path="$(absolute_path "$staging_dir")"
committed_bundle_path="$(absolute_path bundle)"
if is_same_or_inside "$bundle_dir" "$staging_path"; then
  echo "error: BUNDLE_DIR must not be inside $staging_dir"
  exit 1
fi
if [[ "${OLM_TEST_BUNDLE:-}" == true ]] && is_same_or_inside "$bundle_dir" "$committed_bundle_path"; then
  echo "error: set BUNDLE_DIR to a directory outside of bundle/, which holds the committed bundle"
  exit 1
fi
rm -rf "$staging_dir"
mkdir -p "$staging_dir"

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
} > "$work_dir/bundle-input.yaml"
# operator-sdk writes bundle.Dockerfile into the working directory and reads the project layout for the bundle labels
# from the PROJECT file there.
cp PROJECT "$staging_dir/PROJECT"
# operator-sdk only accepts a pipe on stdin, not a redirected file.
cat "$work_dir/bundle-input.yaml" | (
  cd "$staging_dir"
  "$operator_sdk" generate bundle \
    --package dash0-operator \
    --version "$bundle_version" \
    --channels "$bundle_channels" \
    --default-channel "$bundle_default_channel" \
    --output-dir .
)
rm "$staging_dir/PROJECT"

grep_status=0
grep -rn -- "$namespace_sentinel" "$staging_dir" || grep_status=$?
if [[ "$grep_status" != 1 ]]; then
  echo "error: the bundle must not contain the namespace the chart is rendered into (grep exit code $grep_status)"
  exit 1
fi

# operator-sdk sets the annotation createdAt on every run. The previous value is kept when nothing else has changed, so
# that regenerating an unchanged bundle leaves no diff.
if [[ -d "$bundle_dir" ]] && diff -r -q -I '^    createdAt: ' "$bundle_dir" "$staging_dir" > /dev/null; then
  cp "$bundle_dir/$csv_file" "$staging_dir/$csv_file"
fi

rm -rf "$bundle_dir"
mkdir -p "$(dirname "$bundle_dir")"
mv "$staging_dir" "$bundle_dir"

echo "the bundle has been written to $bundle_dir/, intermediate files are in $work_dir/"
