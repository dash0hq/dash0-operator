#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

helm uninstall \
  cert-manager \
  --namespace cert-manager \
  --ignore-not-found
kubectl delete namespace cert-manager --ignore-not-found
