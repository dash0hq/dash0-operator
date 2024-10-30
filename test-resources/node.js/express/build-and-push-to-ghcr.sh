#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

if [[ -z "${GH_PAT:-}" ]]; then
  echo "Error: Provide a personal access token (classic) with write_package permission via GH_PAT."
  exit 1
fi
if [[ -z "${GH_USER:-}" ]]; then
  echo "Error: Provide your Github handle via GH_USER."
  exit 1
fi

echo "$GH_PAT" | docker login ghcr.io -u "$GH_USER" --password-stdin

# When pushing to ghcr.io, we build for x86_64, not arm.
docker build \
  --platform linux/amd64 \
  . \
  -t ghcr.io/dash0hq/dash0-operator-nodejs-20-express-test-app:latest

docker push ghcr.io/dash0hq/dash0-operator-nodejs-20-express-test-app:latest

