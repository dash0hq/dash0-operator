#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"


echo undeploying
./undeploy.sh

echo deploying cert-manager
helm install \
  cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.14.5 \
  --set installCRDs=true \
  --timeout 10s
