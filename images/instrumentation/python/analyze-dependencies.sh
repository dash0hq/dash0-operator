#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# This script is not used for building the instrumentation image, it is merely a tool to gain insights about the
# (transitive) dependencies which result from the packages listed in requirements.txt.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

VENV_DIR=".venv-analysis"

python -m venv "$VENV_DIR"
# shellcheck disable=SC1091
source "$VENV_DIR/bin/activate"
pip install --quiet pipdeptree
pip install --quiet -r requirements.txt

echo ""
echo "Writing reverse dependency tree to reverse-dependency-tree.txt"
pipdeptree --reverse --exclude pipdeptree > "reverse-dependency-tree.txt"

deactivate

rm -rf "$VENV_DIR"

echo ""
echo "Done!"
