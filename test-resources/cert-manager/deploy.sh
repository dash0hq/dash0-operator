#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

# shellcheck disable=SC2143
if [[ ! $(helm repo list | grep -q jetstack) ]]; then
  echo "The helm repo for cert-manager has not been found, adding it now."
  helm repo add jetstack https://charts.jetstack.io --force-update
  echo "Running helm repo update."
  helm repo update
fi

echo "removing any left-overs from previous cert-manager installations (if any)"
./undeploy.sh

echo "deploying cert-manager and waiting for it to become ready, this might take up to five minutes"
helm install \
  cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.18.2 \
  --set crds.enabled=true \
  --set securityContext.runAsNonRoot=false \
  --set webhook.securityContext.runAsNonRoot=false \
  --set cainjector.securityContext.runAsNonRoot=false \
  --set startupapicheck.securityContext.runAsNonRoot=false \
  --timeout 5m
