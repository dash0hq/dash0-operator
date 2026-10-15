#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Deletes the Operator Lifecycle Manager (OLM v0) installed by deploy-olm.sh from the cluster of the current kube
# context. Aborts if the Dash0 operator is still installed via OLM, other operators installed via OLM are deleted
# together with OLM (their CRDs are kept). Running the script again completes an interrupted deletion. An OLM of
# another version or in another namespace is not deleted.

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
  echo "OLM is preinstalled in the namespace openshift-operator-lifecycle-manager, it is not deleted."
  exit 0
fi

csv_crd_established=$(kubectl get crd clusterserviceversions.operators.coreos.com --ignore-not-found \
  -o jsonpath='{.status.conditions[?(@.type=="Established")].status}')
installed_olm=""
dash0_operator_owned_crds=""
if [[ "$csv_crd_established" = "True" ]]; then
  # Copied CSVs carry the label olm.copiedFrom, only the original identifies the OLM installation.
  installed_olm=$(kubectl get csv --all-namespaces --field-selector metadata.name=packageserver \
    --selector '!olm.copiedFrom' \
    -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{.metadata.labels.olm\.version}{"\n"}{end}')
  dash0_monitoring_crd=dash0monitorings.operator.dash0.com
  dash0_operator_owned_crds=$(kubectl get csv --all-namespaces --selector '!olm.copiedFrom' \
    -o jsonpath="{.items[*].spec.customresourcedefinitions.owned[?(@.name==\"$dash0_monitoring_crd\")].name}")
fi
read -r installed_namespace installed_version <<< "$installed_olm"

if [[ -n "$installed_namespace" &&
  ("$installed_namespace" != "$olm_namespace" || "${installed_version#v}" != "${olm_version#v}") ]]; then
  echo "OLM ${installed_version:-of an unknown version} is installed in the namespace $installed_namespace," \
    "it is not deleted."
  exit 0
fi

# Deleting the operator's CSV would leave its monitoring resources behind with a finalizer that nothing removes.
if [[ -n "$dash0_operator_owned_crds" ]]; then
  echo "warning: the Dash0 operator is still installed via OLM, uninstall it before deleting OLM. OLM has not been" \
    "deleted."
  exit 1
fi

olm_namespace_exists=$(kubectl get namespace "$olm_namespace" --ignore-not-found -o name)
if [[ -n "$olm_namespace_exists" ]]; then
  # OLM removes the finalizers of deleted CSVs and deletes the APIService of the packageserver, so the CSVs are deleted
  # while OLM is still running. The APIService is also deleted explicitly, in case OLM has not done so.
  echo "deleting the remaining Subscriptions and ClusterServiceVersions"
  kubectl delete subscriptions.operators.coreos.com --all --all-namespaces
  kubectl delete csv --all-namespaces --selector '!olm.copiedFrom' --timeout="$wait_timeout"
  kubectl delete apiservice v1.packages.operators.coreos.com --ignore-not-found

  echo "deleting the manifests of OLM $olm_version"
  kubectl delete --ignore-not-found --timeout="$wait_timeout" -f "$olm_release_url/olm.yaml"
fi
kubectl delete --ignore-not-found --timeout="$wait_timeout" -f "$olm_release_url/crds.yaml"
echo "OLM has been deleted."
