#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

target_namespace=${1:-test-namespace}
delete_namespace=${2:-true}

source test-resources/bin/util
load_env_file
verify_kubectx

resource_types=( cronjob daemonset deployment job pod replicaset statefulset )
for resource_type in "${resource_types[@]}"; do
  test-resources/node.js/express/undeploy.sh "${target_namespace}" "${resource_type}"
done

kubectl delete -n "${target_namespace}" -f test-resources/customresources/dash0monitoring/dash0monitoring.yaml --wait=false || true
sleep 1
# If the cluster is in a bad state because an operator image has been deployed that terminates abruptly, the monitoring
# resource's finalizer will block the deletion of the monitoring resource, and thus also the deletion of the
# test-namespace. Also, we cannot remove the finalizer from the resource via kubectl patch if the
# validatingwebhookconfiguration is not reachable due to the operator image being faulty (the edit made via kubectl
# patch is send to the validation webhook first, and if the webhook service is not up, the patch never gets executed).
# To get out of this state, we remove the validatingwebhookconfiguration first and then remove the finalizer. All of
# this is only relevant for local development, a fully tested operator image from an official release cannot get into
# this state.
kubectl delete validatingwebhookconfiguration --ignore-not-found dash0-operator-monitoring-validator
kubectl patch -n "${target_namespace}" -f test-resources/customresources/dash0monitoring/dash0monitoring.yaml -p '{"metadata":{"finalizers":null}}' --type=merge --request-timeout=1s || true
kubectl delete -f test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml || true
kubectl delete dash0operatorconfigurations.operator.dash0.com/dash0-operator-configuration-auto-resource || true

if [[ "${target_namespace}" != "default" ]] && [[ "${delete_namespace}" == "true" ]]; then
  kubectl delete ns "${target_namespace}" --ignore-not-found
fi

helm uninstall --namespace dash0-system dash0-operator --timeout 30s || true

kubectl delete secret \
  --namespace dash0-system \
  dash0-authorization-secret \
  --ignore-not-found

kubectl delete ns dash0-system --ignore-not-found

# deliberately deleting dashboards & check rules after undeploying the operator to avoid deleting these items in
# Dash0 every time.
kubectl delete -n "${target_namespace}" -f test-resources/customresources/persesdashboard/persesdashboard.yaml || true
kubectl delete -n "${target_namespace}" -f test-resources/customresources/prometheusrule/prometheusrule.yaml || true

kubectl delete --ignore-not-found=true customresourcedefinition dash0monitorings.operator.dash0.com
kubectl delete --ignore-not-found=true customresourcedefinition dash0operatorconfigurations.operator.dash0.com
kubectl delete --ignore-not-found=true customresourcedefinition dash0operatorconfigurations.operator.dash0.com
kubectl delete --ignore-not-found=true customresourcedefinition persesdashboards.perses.dev
kubectl delete --ignore-not-found=true customresourcedefinition prometheusrules.monitoring.coreos.com

# The following resources are deleted automatically with helm uninstall, unless for example when the operator manager
# crashes and the helm pre-delete helm hook cannot run, then they might be left behind.
kubectl delete clusterrole                  --ignore-not-found dash0-operator-cluster-metrics-collector-cr
kubectl delete clusterrole                  --ignore-not-found dash0-operator-manager-role
kubectl delete clusterrole                  --ignore-not-found dash0-operator-metrics-reader
kubectl delete clusterrole                  --ignore-not-found dash0-operator-opentelemetry-collector-cr
kubectl delete clusterrole                  --ignore-not-found dash0-operator-proxy-role
kubectl delete clusterrolebinding           --ignore-not-found dash0-operator-cluster-metrics-collector-crb
kubectl delete clusterrolebinding           --ignore-not-found dash0-operator-manager-rolebinding
kubectl delete clusterrolebinding           --ignore-not-found dash0-operator-opentelemetry-collector-crb
kubectl delete clusterrolebinding           --ignore-not-found dash0-operator-proxy-rolebinding
kubectl delete mutatingwebhookconfiguration --ignore-not-found dash0-operator-injector
kubectl delete validatingwebhookconfiguration --ignore-not-found dash0-operator-operator-configuration-validator
kubectl delete validatingwebhookconfiguration --ignore-not-found dash0-operator-monitoring-validator

