#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

# shellcheck source=test-resources/bin/util
source ../../bin/util

target_namespace=${1:-test-namespace}
kind=${2:-deployment}
check_if_kubectx_is_kind_cluster

if [[ -z ${SKIP_DOCKER_BUILD:-} ]]; then
  docker build . -t dash0-operator-nodejs-20-express-test-app

  if [[ "$is_kind_cluster" = "true" ]]; then
    echo loading test image into Kind cluster
    kind load docker-image \
      --name "$cluster" \
      dash0-operator-nodejs-20-express-test-app:latest
  fi
fi

if [[ -f ${kind}.yaml ]]; then
  ../../bin/render-templates.sh
fi

./undeploy.sh "${target_namespace}" "${kind}"
kubectl apply -n "${target_namespace}" -f "${kind}".yaml

if [[ "$is_kind_cluster" = "true" ]]; then
  echo
  echo "Note: The test application is running on a kind cluster, make sure cloud-provider-kind is running."
  echo
  echo "checking if ingress-nginx is already running..."
  if ! kubectl wait --namespace ingress-nginx --for=condition=ready pod --selector=app.kubernetes.io/component=controller --timeout=5s; then
    echo "deploying ingress-nginx..."
    echo
    kubectl apply -f https://kind.sigs.k8s.io/examples/ingress/deploy-ingress-nginx.yaml
    echo
    echo "waiting for ingress-nginx..."
    kubectl wait \
      --namespace ingress-nginx \
      --for=condition=ready \
      pod \
      --selector=app.kubernetes.io/component=controller \
      --timeout=90s
  else
    echo ingress-nginx is already running
  fi
  ingress_ip=$(kubectl get services \
    --namespace ingress-nginx \
    ingress-nginx-controller \
    --output jsonpath='{.status.loadBalancer.ingress[0].ip}'
  )
  echo
  echo "Use $ingress_ip/$kind/dash0-k8s-operator-test to access the test application"
fi
