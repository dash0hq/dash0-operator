#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Installs the operator from an OLM catalog image into the cluster of the current kube context (OLM must be installed,
# see deploy-olm.sh) via a CatalogSource, an OperatorGroup and a Subscription, then creates a Dash0OperatorConfiguration
# and a Dash0Monitoring resource. By default, the dev catalog of the main branch is installed
# (ghcr.io/dash0hq/dash0-operator-catalog:main-dev). Set CATALOG_IMAGE to install another catalog image, e.g. one built
# with "make olm-catalog olm-catalog-image push-olm-catalog-image", and CATALOG_CHANNEL (default: stable-v0) to
# subscribe to another channel. The script does not build any images. Like the test scenarios, the script reads
# test-resources/.env, see test-resources/.env.template for DASH0_INGRESS_ENDPOINT, DASH0_API_ENDPOINT,
# DASH0_AUTHORIZATION_TOKEN and the other settings.
# Usage: test-resources/bin/test-olm-catalog.sh [cleanup]

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

catalog_image="${CATALOG_IMAGE:-ghcr.io/dash0hq/dash0-operator-catalog:main-dev}"
catalog_channel="${CATALOG_CHANNEL:-stable-v0}"
operator_ns="${OPERATOR_NAMESPACE:-$default_operator_ns}"
monitored_ns="$default_target_ns"
# test-olm-bundle.sh treats a catalog source named dash0-operator-catalog as a leftover of its own runs.
catalog_source=dash0-operator-catalog-test
operator_group=dash0-operator-catalog-test
subscription=dash0-operator
install_timeout_seconds=600

cleanup() {
  delete_olm_test_monitoring_resource "$monitored_ns"
  local csvs
  csvs=$(kubectl get subscriptions.operators.coreos.com "$subscription" --namespace "$operator_ns" \
    --ignore-not-found -o jsonpath='{.status.currentCSV}{" "}{.status.installedCSV}')
  kubectl delete subscriptions.operators.coreos.com "$subscription" --namespace "$operator_ns" --ignore-not-found
  local csv
  for csv in $csvs; do
    kubectl delete clusterserviceversions.operators.coreos.com "$csv" --namespace "$operator_ns" --ignore-not-found
  done
  kubectl delete catalogsources.operators.coreos.com "$catalog_source" --namespace "$operator_ns" --ignore-not-found
  kubectl delete operatorgroups.operators.coreos.com "$operator_group" --namespace "$operator_ns" --ignore-not-found
}

# Prints the value of the JSONPath $2 of the resource $1 in the operator namespace as soon as it is not empty and, if $3
# is given, equal to $3. Fails when the deadline (in SECONDS) passes.
wait_for_value() {
  local value
  while true; do
    value=$(kubectl get "$1" --namespace "$operator_ns" --ignore-not-found -o jsonpath="$2")
    if [[ -n "$value" && (-z "${3:-}" || "$value" == "$3") ]]; then
      echo "$value"
      return 0
    fi
    if ((SECONDS >= deadline)); then
      echo "error: $1 has not reached the expected state within $install_timeout_seconds seconds, $2 is \"$value\"" >&2
      return 1
    fi
    sleep 5
  done
}

if [[ "${1:-}" = "cleanup" ]]; then
  cleanup
  exit 0
fi

kubectl create namespace "$operator_ns" --dry-run=client -o yaml | kubectl apply -f -
bundle_test_installation=$(kubectl get catalogsources.operators.coreos.com dash0-operator-catalog \
  --namespace "$operator_ns" --ignore-not-found -o name)
if [[ -n "$bundle_test_installation" ]]; then
  echo "error: the operator has been installed by test-olm-bundle.sh, run test-resources/bin/test-olm-bundle.sh" \
    "cleanup first"
  exit 1
fi
previous_catalog=$(kubectl get catalogsources.operators.coreos.com "$catalog_source" --namespace "$operator_ns" \
  --ignore-not-found -o name)
if [[ -n "$previous_catalog" ]]; then
  echo "removing the previous installation"
  cleanup
fi

# A namespace must not have more than one OperatorGroup.
existing_operator_groups=$(kubectl get operatorgroups.operators.coreos.com --namespace "$operator_ns" -o name)
if [[ -z "$existing_operator_groups" ]]; then
  kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: $operator_group
  namespace: $operator_ns
EOF
fi

echo "creating the catalog source for $catalog_image"
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: $catalog_source
  namespace: $operator_ns
spec:
  sourceType: grpc
  image: $catalog_image
  displayName: Dash0 Operator (test)
  publisher: Dash0
  grpcPodConfig:
    securityContextConfig: restricted
EOF
deadline=$((SECONDS + install_timeout_seconds))
echo "waiting for the catalog source to become ready"
wait_for_value "catalogsources.operators.coreos.com/$catalog_source" \
  '{.status.connectionState.lastObservedState}' READY > /dev/null

echo "subscribing to the channel $catalog_channel"
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: $subscription
  namespace: $operator_ns
spec:
  channel: $catalog_channel
  name: dash0-operator
  source: $catalog_source
  sourceNamespace: $operator_ns
  installPlanApproval: Automatic
EOF

echo "waiting for OLM to install the operator"
if ! installed_csv=$(wait_for_value "subscriptions.operators.coreos.com/$subscription" '{.status.installedCSV}'); then
  kubectl get subscriptions.operators.coreos.com "$subscription" --namespace "$operator_ns" \
    -o jsonpath='{.status.conditions}{"\n"}'
  exit 1
fi
if ! wait_for_value "clusterserviceversions.operators.coreos.com/$installed_csv" '{.status.phase}' Succeeded \
  > /dev/null; then
  kubectl get clusterserviceversions.operators.coreos.com "$installed_csv" --namespace "$operator_ns" \
    -o jsonpath='{.status.reason}{": "}{.status.message}{"\n"}'
  exit 1
fi
kubectl get clusterserviceversions.operators.coreos.com --namespace "$operator_ns"

install_olm_test_resources "$operator_ns" "$monitored_ns" olm-catalog-test
