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

pip install --quiet --root-user-action ignore -r requirements.txt
pip install --quiet --root-user-action ignore pipdeptree
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
    # Write the package requirement to the output file (via echo, we pipe the output of the while loop into the file).
    echo "${package_name} ${version_spec}"
  elif echo "$line" | grep -qE '[a-zA-Z0-9_-]+[=<>!~]+[0-9]'; then
    # Lines representing direct dependencies listed in requirements.txt look like this:
    # opentelemetry-exporter-otlp-proto-http==1.39.1.
    # Write this as-is to the output file (via echo, we pipe the output of the while loop into the file).
    echo "$line"
  fi
done < "$pipdeptree_output_file" | sort -u > "$output_file" # write the while loop's output (e.g. all echo commands) to "$output_file"

echo "converted $pipdeptree_output_file to $output_file"
echo "total unique requirements: $(wc -l < "$output_file" | tr -d ' ')"
