#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

helm uninstall \
  cert-manager \
  --namespace cert-manager \
  --ignore-not-found

kubectl delete namespace cert-manager --ignore-not-found
