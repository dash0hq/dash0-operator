#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

pipdeptree_output_file=dependency-tree.txt
output_file="${2:-all-dependencies.txt}"
venv_dir=".flatten-dependencies"

trap "{ rm -f ""$pipdeptree_output_file""; deactivate; rm -rf ""$venv_dir""; }" EXIT

export PIP_DISABLE_PIP_VERSION_CHECK=1

python -m venv "$venv_dir"
# shellcheck disable=SC1091
. "$venv_dir/bin/activate"

# The Dash0 Python distribution's workspace packages are copied into /tmp/dash0-python-distribution by the Dockerfile
# stage that runs this script. The dash0-opentelemetry-distro package pins the full instrumentation set as regular
# dependencies, so installing it (together with the pure-Python OTLP exporter packages it depends on) gives us the
# complete injected tree.
dash0_distribution_dir="/tmp/dash0-python-distribution"
if [ ! -d "$dash0_distribution_dir" ]; then
  echo "$dash0_distribution_dir does not exist; this script must run inside the instrumentation image build" >&2
  exit 1
fi

pip install --quiet --root-user-action ignore \
  "$dash0_distribution_dir/packages/opentelemetry-pyproto" \
  "$dash0_distribution_dir/packages/opentelemetry-exporter-otlp-pyproto-common" \
  "$dash0_distribution_dir/packages/opentelemetry-exporter-otlp-pyproto-grpc" \
  "$dash0_distribution_dir/packages/opentelemetry-exporter-otlp-pyproto-http" \
  "$dash0_distribution_dir/packages/dash0-opentelemetry-distro"
# Pin pipdeptree to the last version that ships prebuilt wheels; 4.0.0+ is sdist-only and requires a Rust toolchain
# (cargo) to build from source, which the Python build stages do not have.
pip install --quiet --root-user-action ignore pipdeptree==2.27.0
pipdeptree --exclude pipdeptree > "$pipdeptree_output_file"

while IFS= read -r line; do
  if echo "$line" | grep -q "\[required: "; then
    # Lines that specify a transitive/indirect dependency look like this:
    # │       └── protobuf [required: >=5.0,<7.0, installed: 6.33.5]
    # Remove everything up to the first alphabetic character (this includes the tree structure characters) to normalize
    # the line.
    normalized=$(echo "$line" | sed 's/^[^[:alpha:]]*//')
    # Next, extract the package name (everything before [required:)
    package_name=$(echo "$normalized" | sed 's/ \[required:.*//')
    # Extract the version spec (between "required: " and ", installed:")
    version_spec=$(echo "$normalized" | sed 's/.*\[required: \(.*\), installed:.*/\1/')
    # pipdeptree emits the literal string "Any" when a dependency has no version constraint. That is not a valid PEP
    # 440 specifier, so packaging.requirements.Requirement would raise on it. Drop the spec in that case: an empty
    # specifier matches any installed version, which is exactly what we want.
    if [ "$version_spec" = "Any" ]; then
      echo "${package_name}"
    else
      echo "${package_name} ${version_spec}"
    fi
  elif echo "$line" | grep -qE '[a-zA-Z0-9_-]+[=<>!~]+[0-9]'; then
    # Lines representing top-level direct dependencies look like this:
    # dash0-opentelemetry-distro==0.2.0
    # Write this as-is to the output file (via echo, we pipe the output of the while loop into the file).
    echo "$line"
  fi
done < "$pipdeptree_output_file" | sort -u > "$output_file" # write the while loop's output (e.g. all echo commands) to "$output_file"

echo "converted $pipdeptree_output_file to $output_file"
echo "total unique requirements: $(wc -l < "$output_file" | tr -d ' ')"
