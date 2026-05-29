#!/usr/bin/env bash

# Runs apply-transformations.py locally against the operator's Helm chart docs, in a throwaway virtual environment,
# and writes the transformed result to a temporary file so it can be inspected. This mirrors what the
# sync-docs-to-website workflow does, without checking out or pushing to the documentation repository.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

source="../../../helm-chart/dash0-operator/README.md"
target="../../../README_transformed.md"

cleanup() {
  # Leave the virtual environment (if it is active) and remove it again.
  if [[ "$(type -t deactivate)" == "function" ]]; then
    deactivate
  fi
  rm -rf venv
}
trap cleanup EXIT

rm -rf venv

python -m venv venv
# shellcheck disable=SC1091
. venv/bin/activate
pip install -r requirements.txt

python apply-transformations.py "$source" transformations.yaml "$target"

echo
echo "Transformed document written to: $target"
