#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

target_namespace=${1:-dash0-operator-system}

if [[ ! $(helm repo list | grep open-telemetry) ]]; then
  echo "The helm repo for open-telemetry has not been found, adding it now."
  helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts --force-update
  echo "Running helm repo update."
  helm repo update
fi

if [[ ! -f manual.values.yaml ]]; then
  ../bin/render-templates.sh
fi

./undeploy.sh ${target_namespace}

helm install \
  dash0-opentelemetry-collector-daemonset \
  open-telemetry/opentelemetry-collector \
  --namespace ${target_namespace} \
  --create-namespace \
  --values manual.values.yaml \
  --set image.repository="otel/opentelemetry-collector-k8s"
