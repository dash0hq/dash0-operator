#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

target_namespace=${1:-test-namespace}
kind=${2:-deployment}

kubectl delete -n "${target_namespace}" -f "${kind}".yaml --ignore-not-found || true
kubectl delete -f https://kind.sigs.k8s.io/examples/ingress/deploy-ingress-nginx.yaml --ignore-not-found || true

