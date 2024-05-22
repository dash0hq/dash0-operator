#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

if [[ ! -f values.yaml ]]; then
  echo Please copy $(pwd)/values.yaml.template to $(pwd)/values.yaml and provide the auth token and the ingress endpoint.
  exit 1
fi

target_namespace=${1:-default}

# helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts

./undeploy.sh ${target_namespace}

helm install \
  dash0-opentelemetry-collector-daemonset \
  open-telemetry/opentelemetry-collector \
  --namespace ${target_namespace} \
  --values values.yaml \
  --set image.repository="otel/opentelemetry-collector-k8s"