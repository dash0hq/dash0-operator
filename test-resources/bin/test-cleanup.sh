#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

target_namespace=${1:-test-namespace}
delete_namespace=${2:-true}

source test-resources/bin/util
load_env_file
verify_kubectx

resource_types=( cronjob daemonset deployment job pod replicaset statefulset )
for resource_type in "${resource_types[@]}"; do
  test-resources/node.js/express/undeploy.sh ${target_namespace} ${resource_type}
done

kubectl delete -n ${target_namespace} -f test-resources/customresources/dash0monitoring/dash0monitoring.yaml --wait=false || true
sleep 1
kubectl patch -f test-resources/customresources/dash0monitoring/dash0monitoring.yaml -p '{"metadata":{"finalizers":null}}' --type=merge || true
kubectl delete -f test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml || true
kubectl delete dash0operatorconfigurations.operator.dash0.com/dash0-operator-configuration-auto-resource || true

if [[ "${target_namespace}" != "default" ]] && [[ "${delete_namespace}" == "true" ]]; then
  kubectl delete ns ${target_namespace} --ignore-not-found
fi

helm uninstall --namespace dash0-system dash0-operator --timeout 30s || true

kubectl delete secret \
  --namespace dash0-system \
  dash0-authorization-secret \
  --ignore-not-found

kubectl delete ns dash0-system --ignore-not-found

# deliberately deleting the dashboard after undeploying the operator to avoid deleting the dashboard in Dash0 every time.
kubectl delete -n ${target_namespace} -f test-resources/customresources/persesdashboard/persesdashboard.yaml || true

kubectl delete --ignore-not-found=true customresourcedefinition dash0monitorings.operator.dash0.com
kubectl delete --ignore-not-found=true customresourcedefinition dash0operatorconfigurations.operator.dash0.com
kubectl delete --ignore-not-found=true customresourcedefinition dash0operatorconfigurations.operator.dash0.com
kubectl delete --ignore-not-found=true customresourcedefinition perses.perses.dev
kubectl delete --ignore-not-found=true customresourcedefinition persesdashboards.perses.dev
kubectl delete --ignore-not-found=true customresourcedefinition persesdatasources.perses.dev

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

kubectl delete --ignore-not-found crd scrapeconfigs.monitoring.coreos.com

