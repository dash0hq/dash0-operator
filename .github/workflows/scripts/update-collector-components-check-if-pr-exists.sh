#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

if ! command -v gh &> /dev/null; then
  echo "Error: the gh executable is not available."
  exit 1
fi

#gh pr list --json title | grep "chore(deps): bump Dash0 collector components" || true
#if gh pr list --json title | grep "chore(deps): bump Dash0 collector components"; then
#  echo pr_exists=true >> "$GITHUB_OUTPUT"
#  echo "There is already an open pull request to update the Dash0 collector components, remaining steps will be skipped."
#else
echo pr_exists=false >> "$GITHUB_OUTPUT"
#  echo "No open pull request to update the Dash0 collector components exists, continuing with the workflow."
#fi
