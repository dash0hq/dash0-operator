#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Installs the operator from the OLM bundle into the cluster of the current kube context (OLM must be installed, see
# deploy-olm.sh), then creates a Dash0OperatorConfiguration and a Dash0Monitoring resource. Like in the test scenarios,
# the operator images and the bundle image are built unless SKIP_IMAGE_BUILDS=true, and pushed if
# PUSH_BUILT_IMAGES=true. OLM pulls the images from the registry of IMAGE_REPOSITORY_PREFIX, which is required. Like the
# test scenarios, the script reads test-resources/.env, see test-resources/.env.template for DASH0_INGRESS_ENDPOINT,
# DASH0_API_ENDPOINT, DASH0_AUTHORIZATION_TOKEN and the other settings. Set BUNDLE_REGISTRY_USE_HTTP=true if the
# registry of the bundle image only serves plain HTTP, like the local registry of the kind setup.
# Usage: test-resources/bin/test-olm-bundle.sh [cleanup]

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file
verify_kubectx

bundle_version="${BUNDLE_VERSION:-0.0.1}"
operator_ns="${OPERATOR_NAMESPACE:-$default_operator_ns}"
monitored_ns="$default_target_ns"
operator_sdk="${OPERATOR_SDK:-bin/operator-sdk}"

cleanup() {
  delete_olm_test_monitoring_resource "$monitored_ns"
  make operator-sdk
  "$operator_sdk" cleanup dash0-operator --namespace "$operator_ns"
}

if [[ "${1:-}" = "cleanup" ]]; then
  cleanup
  exit 0
fi

: "${IMAGE_REPOSITORY_PREFIX:?IMAGE_REPOSITORY_PREFIX is required}"
catalog_test_installation=$(kubectl get catalogsources.operators.coreos.com dash0-operator-catalog-test \
  --namespace "$operator_ns" --ignore-not-found -o name)
if [[ -n "$catalog_test_installation" ]]; then
  echo "error: the operator has been installed by test-olm-catalog.sh, run test-resources/bin/test-olm-catalog.sh" \
    "cleanup first"
  exit 1
fi
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
run_bundle_args=(--namespace "$operator_ns" --install-mode AllNamespaces --timeout 10m)
# operator-sdk pulls the bundle image on this machine, not in the cluster, and uses HTTPS unless --use-http is set.
if [[ "${BUNDLE_REGISTRY_USE_HTTP:-}" = "true" ]]; then
  run_bundle_args+=(--use-http)
fi
"$operator_sdk" run bundle "$bundle_image" "${run_bundle_args[@]}"
kubectl get csv --namespace "$operator_ns"

install_olm_test_resources "$operator_ns" "$monitored_ns" olm-bundle-test
