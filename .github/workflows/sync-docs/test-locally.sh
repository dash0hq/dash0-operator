#!/usr/bin/env bash

# Runs apply-transformations.py locally against the operator's Helm chart docs, in a virtual environment, and writes the
# transformed result to a temporary directory so it can be inspected. This mirrors what the sync-docs-to-website
# workflow does, without checking out or pushing to the documentation repository.
#
# Modes:
#   (no arguments)  Create the virtual environment, install the dependencies, run apply-transformations.py, and remove
#                   the virtual environment again afterwards.
#   create-venv     Create the virtual environment and install the dependencies. The virtual environment is left
#                   in place so that subsequent apply-in-venv runs can reuse it.
#   remove-venv     Remove the virtual environment.
#   apply-in-venv   Assume that the virtual environment already exists, then run apply-transformations.py in it.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

source_root="../../../helm-chart/dash0-operator"
output_dir="../../../helm-chart/dash0-operator/.transformed-docs"

create_venv() {
  echo "(re)creating Python venv in $(pwd)/venv"
  rm -rf venv
  python -m venv venv
  # shellcheck disable=SC1091
  . venv/bin/activate
  pip install -r requirements.txt
}

remove_venv() {
  echo "cleaning up Python venv in $(pwd)/venv"
  # Leave the virtual environment (if it is active) and remove it again.
  if [[ "$(type -t deactivate)" == "function" ]]; then
    deactivate
  fi
  rm -rf venv
}

apply_transformations() {
  echo "transforming docs now"
  python apply-transformations.py "$source_root" transformations.yaml "$output_dir"

  echo
  echo "Transformed documents written to: $output_dir"
  find "$output_dir" -type f | sort
}

mode="${1:-default}"

case "$mode" in
  default)
    trap remove_venv EXIT
    create_venv
    apply_transformations
    ;;
  create-venv)
    create_venv
    echo "Python venv is left in place, call apply-in-venv to run transformations, call remove-venv to remove the venv."
    ;;
  remove-venv)
    remove_venv
    ;;
  apply-in-venv)
    if [[ ! -d venv ]]; then
      echo "Error: the virtual environment does not exist. Run '$0 create-venv' first." >&2
      exit 1
    fi
    # shellcheck disable=SC1091
    . venv/bin/activate
    apply_transformations
    ;;
  *)
    echo "Error: unknown mode '$mode'. Valid modes: create-venv, remove-venv, apply-in-venv (or no argument)." >&2
    exit 1
    ;;
esac
