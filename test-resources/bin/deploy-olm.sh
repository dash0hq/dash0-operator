#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Installs the Operator Lifecycle Manager (OLM v0) into the cluster of the current kube context, for testing the OLM
# bundle, and waits until OLM is ready. Running the script again completes an interrupted installation. An OLM of
# another version or in another namespace is not modified, only waited for. Fails if OLM is preinstalled by OpenShift,
# which the OLM bundle does not support.

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

olm_namespace=olm
wait_timeout=5m

openshift_olm=$(kubectl get deployment olm-operator --namespace openshift-operator-lifecycle-manager \
  --ignore-not-found -o name)
if [[ -n "$openshift_olm" ]]; then
  echo "error: OLM is preinstalled in the namespace openshift-operator-lifecycle-manager, OpenShift is not supported" \
    "by the OLM bundle."
  exit 1
fi

csv_crd_established=$(kubectl get crd clusterserviceversions.operators.coreos.com --ignore-not-found \
  -o jsonpath='{.status.conditions[?(@.type=="Established")].status}')
installed_olm=""
if [[ "$csv_crd_established" = "True" ]]; then
  # Copied CSVs carry the label olm.copiedFrom, only the original identifies the OLM installation.
  installed_olm=$(kubectl get csv --all-namespaces --field-selector metadata.name=packageserver \
    --selector '!olm.copiedFrom' \
    -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{.metadata.labels.olm\.version}{"\n"}{end}')
fi
read -r installed_namespace installed_version <<< "$installed_olm"

if [[ -n "$installed_namespace" &&
  ("$installed_namespace" != "$olm_namespace" || "${installed_version#v}" != "${olm_version#v}") ]]; then
  echo "OLM ${installed_version:-of an unknown version} is already installed in the namespace $installed_namespace," \
    "it is not modified."
  olm_namespace=$installed_namespace
else
  echo "applying the manifests of OLM $olm_version"
  kubectl apply --server-side -f "$olm_release_url/crds.yaml"
  kubectl wait --for=condition=Established --timeout="$wait_timeout" -f "$olm_release_url/crds.yaml"
  kubectl apply --server-side -f "$olm_release_url/olm.yaml"
  # Do not accidentally add monitoring resources to the OLM namespaces when testing namespace auto-monitoring.
  kubectl label namespace "$olm_namespace" operators --overwrite dash0.com/enable=false
fi

echo "waiting for OLM to become ready"
kubectl rollout status deployment/olm-operator --namespace "$olm_namespace" --timeout="$wait_timeout"
kubectl rollout status deployment/catalog-operator --namespace "$olm_namespace" --timeout="$wait_timeout"
kubectl wait --for=jsonpath='{.status.phase}'=Succeeded --timeout="$wait_timeout" csv/packageserver \
  --namespace "$olm_namespace"
kubectl rollout status deployment/packageserver --namespace "$olm_namespace" --timeout="$wait_timeout"
echo "OLM is ready."
