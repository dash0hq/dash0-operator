#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Installs the operator from the OLM bundle into the cluster of the current kube context (OLM must be installed, see
# deploy-olm.sh), then creates a Dash0OperatorConfiguration and a Dash0Monitoring resource. Like in the test scenarios,
# the operator images and the bundle image are built unless SKIP_IMAGE_BUILDS=true, and pushed if
# PUSH_BUILT_IMAGES=true. OLM pulls the images from the registry of IMAGE_REPOSITORY_PREFIX. Requires
# IMAGE_REPOSITORY_PREFIX, DASH0_INGRESS_ENDPOINT, DASH0_API_ENDPOINT, DASH0_AUTHORIZATION_TOKEN and ALLOWED_KUBECTXS.
# Usage: test-resources/bin/test-olm-bundle.sh [cleanup]

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

# shellcheck source=./lib/util
source "$scripts_lib/util"

verify_kubectx

bundle_version="${BUNDLE_VERSION:-0.0.1}"
operator_ns="${OPERATOR_NAMESPACE:-$default_operator_ns}"
monitored_ns="$default_target_ns"
operator_sdk="${OPERATOR_SDK:-bin/operator-sdk}"

cleanup() {
  # Delete the monitoring resource while the operator still runs, so that its finalizer uninstruments the workloads.
  if kubectl get crd dash0monitorings.operator.dash0.com > /dev/null 2>&1; then
    if ! kubectl delete --namespace "$monitored_ns" --ignore-not-found --timeout 2m \
      -f config/samples/operator_v1beta1_dash0monitoring.yaml; then
      echo "error: the Dash0Monitoring resource in $monitored_ns was not deleted, check that the operator is running."
      exit 1
    fi
  fi
  make operator-sdk
  "$operator_sdk" cleanup dash0-operator --namespace "$operator_ns"
}

if [[ "${1:-}" = "cleanup" ]]; then
  cleanup
  exit 0
fi

: "${IMAGE_REPOSITORY_PREFIX:?IMAGE_REPOSITORY_PREFIX is required}"
: "${DASH0_INGRESS_ENDPOINT:?DASH0_INGRESS_ENDPOINT is required}"
: "${DASH0_API_ENDPOINT:?DASH0_API_ENDPOINT is required}"
: "${DASH0_AUTHORIZATION_TOKEN:?DASH0_AUTHORIZATION_TOKEN is required}"
bundle_image="${BUNDLE_IMAGE_REPOSITORY:-${IMAGE_REPOSITORY_PREFIX}dash0-operator-bundle}:${BUNDLE_IMAGE_TAG:-v$bundle_version}"

determine_container_images
build_all_images
push_all_images

if [[ "${SKIP_IMAGE_BUILDS:-}" != "true" ]]; then
  make olm-test-bundle olm-bundle-validate olm-bundle-image \
    BUNDLE_VERSION="$bundle_version" \
    BUNDLE_DIR=olm/build/test-bundle
fi
if [[ "${PUSH_BUILT_IMAGES:-}" = "true" ]]; then
  make push-olm-bundle-image BUNDLE_VERSION="$bundle_version"
fi
make operator-sdk

kubectl create namespace "$operator_ns" --dry-run=client -o yaml | kubectl apply -f -
# operator-sdk run bundle fails if the catalog source of a previous run still exists.
previous_catalog=$(kubectl get catalogsource dash0-operator-catalog --namespace "$operator_ns" --ignore-not-found -o name)
if [[ -n "$previous_catalog" ]]; then
  echo "removing the previous installation"
  cleanup
fi
"$operator_sdk" run bundle "$bundle_image" \
  --namespace "$operator_ns" --install-mode AllNamespaces --timeout 10m
kubectl get csv --namespace "$operator_ns"

kubectl create secret generic dash0-authorization-secret --namespace "$operator_ns" \
  --from-literal=token="$DASH0_AUTHORIZATION_TOKEN" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f - <<EOF
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  exports:
    - dash0:
        endpoint: $DASH0_INGRESS_ENDPOINT
        authorization:
          secretRef:
            name: dash0-authorization-secret
            key: token
        apiEndpoint: $DASH0_API_ENDPOINT
  clusterName: ${CLUSTER_NAME:-olm-bundle-test}
EOF
# The validation webhook rejects a monitoring resource without exports until the operator configuration is available.
kubectl wait dash0operatorconfigurations.operator.dash0.com/dash0-operator-configuration --for condition=Available

kubectl create namespace "$monitored_ns" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply --namespace "$monitored_ns" -f config/samples/operator_v1beta1_dash0monitoring.yaml
