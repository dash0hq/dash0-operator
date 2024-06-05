#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

target_namespace=${1:-test-namespace}

if [[ ! $(helm repo list | grep open-telemetry) ]]; then
  echo "The helm repo for open-telemetry has not been found, adding it now."
  helm helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts --force-update
  echo "Running helm repo update."
  helm repo update
fi

if [[ ! -f manual.values.yaml ]]; then
  ../bin/render-templates.sh manual-testing
fi

./undeploy.sh ${target_namespace}

helm install \
  dash0-opentelemetry-collector-daemonset \
  open-telemetry/opentelemetry-collector \
  --namespace ${target_namespace} \
  --values manual.values.yaml \
  --set image.repository="otel/opentelemetry-collector-k8s"
