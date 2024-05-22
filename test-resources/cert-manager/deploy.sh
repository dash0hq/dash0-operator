#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

echo "removing any left-overs from previous cert-manager installations (if any)"
./undeploy.sh

echo "deploying cert-manager and waiting for it to become ready, this might take up to five minutes"
helm install \
  cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.14.5 \
  --set installCRDs=true \
  --timeout 5m