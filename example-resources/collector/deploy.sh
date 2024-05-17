#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

if [[ ! -f values.yaml ]]; then
  echo Please copy $(pwd)/values.yaml.template to $(pwd)/values.yaml and provide the auth token and the ingress endpoint.
  exit 1
fi

# TODO deploy to a Dash0-specific namespace

# helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm uninstall dash0-opentelemetry-collector-daemonset --ignore-not-found
helm install \
  dash0-opentelemetry-collector-daemonset \
  open-telemetry/opentelemetry-collector \
  --values values.yaml \
  --set image.repository="otel/opentelemetry-collector-k8s"
