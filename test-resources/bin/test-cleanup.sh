#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

operator_namespace=${OPERATOR_NAMESPACE:-operator-namespace}
target_namespace=${1:-test-namespace}
delete_namespaces=${2:-true}

source test-resources/bin/util
load_env_file
verify_kubectx

resource_types=( cronjob daemonset deployment job pod replicaset statefulset )
for resource_type in "${resource_types[@]}"; do
  test-resources/node.js/express/undeploy.sh "$target_namespace" "$resource_type"
  pushd test-resources/jvm/spring-boot > /dev/null
    if [[ -f "$resource_type.yaml" ]]; then
      kubectl delete --namespace "$target_namespace" --ignore-not-found -f "$resource_type.yaml"
    fi
  popd > /dev/null
  pushd test-resources/dotnet > /dev/null
    if [[ -f "$resource_type.yaml" ]]; then
      kubectl delete --namespace "$target_namespace" --ignore-not-found -f "$resource_type.yaml"
    fi
  popd > /dev/null
  pushd test-resources/python/flask > /dev/null
    if [[ -f "$resource_type.yaml" ]]; then
      kubectl delete --namespace "$target_namespace" --ignore-not-found -f "$resource_type.yaml"
    fi
  popd > /dev/null
done

wait_for_third_party_resource_deletion="false"
if kubectl delete -n "$target_namespace" -f test-resources/customresources/dash0syntheticcheck/dash0syntheticcheck.yaml; then
  wait_for_third_party_resource_deletion="true"
fi
if kubectl delete -n "$target_namespace" -f test-resources/customresources/dash0view/dash0view.yaml; then
  wait_for_third_party_resource_deletion="true"
fi
if kubectl delete -n "$target_namespace" -f test-resources/customresources/persesdashboard/persesdashboard.yaml; then
  wait_for_third_party_resource_deletion="true"
fi
if kubectl delete -n "$target_namespace" -f test-resources/customresources/prometheusrule/prometheusrule.yaml; then
  wait_for_third_party_resource_deletion="true"
fi

if [[ "$wait_for_third_party_resource_deletion" = "true" ]]; then
  echo "Waiting for third party resource deletion to be synchronized to the Dash0 API."
  sleep 2
fi

kubectl delete -n "$target_namespace" -f test-resources/customresources/dash0monitoring/dash0monitoring.yaml --wait=false || true
kubectl delete -n test-namespace-2 -f test-resources/customresources/dash0monitoring/dash0monitoring.yaml --wait=false || true
kubectl delete -n test-namespace-3 -f test-resources/customresources/dash0monitoring/dash0monitoring.yaml --wait=false || true
sleep 1
# If the cluster is in a bad state because an operator image has been deployed that terminates abruptly, the monitoring
# resource's finalizer will block the deletion of the monitoring resource, and thus also the deletion of the
# test-namespace. Also, we cannot remove the finalizer from the resource via kubectl patch if either the
# validatingwebhookconfiguration or the mutatingwebhookconfiguration for this resource type is not reachable due to the
# operator image being faulty or because it already has been removed (the edit made via kubectl patch is send to the
# webhooks first, and if the webhook service is not up, the patch never gets executed).
# To get out of this state, we remove the validatingwebhookconfiguration first and then remove the finalizer. All of
# this is only relevant for local development, a fully tested operator image from an official release cannot get into
# this state.
kubectl delete validatingwebhookconfiguration --ignore-not-found dash0-operator-monitoring-validator
kubectl delete mutatingwebhookconfiguration --ignore-not-found dash0-operator-monitoring-mutating
kubectl patch -n "${target_namespace}" -f test-resources/customresources/dash0monitoring/dash0monitoring.yaml -p '{"metadata":{"finalizers":null}}' --type=merge --request-timeout=1s || true
kubectl delete -f test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml || true
kubectl delete dash0operatorconfigurations.operator.dash0.com/dash0-operator-configuration-auto-resource || true

if [[ "${target_namespace}" != "default" ]] && [[ "${delete_namespaces}" = "true" ]]; then
  kubectl delete ns "${target_namespace}" --ignore-not-found
fi
if [[ "${delete_namespaces}" = "true" ]]; then
  kubectl delete ns test-namespace-2 --ignore-not-found
  kubectl delete ns test-namespace-3 --ignore-not-found
fi

helm uninstall --namespace "$operator_namespace" dash0-operator --timeout 30s || true

kubectl delete secret \
  --namespace "$operator_namespace" \
  dash0-authorization-secret \
  --ignore-not-found

kubectl delete ns "$operator_namespace" --ignore-not-found

kubectl delete --ignore-not-found=true -f test-resources/otlp-sink/otlp-sink.yaml --wait

kubectl delete --ignore-not-found=true customresourcedefinition dash0monitorings.operator.dash0.com
kubectl delete --ignore-not-found=true customresourcedefinition dash0operatorconfigurations.operator.dash0.com
kubectl delete --ignore-not-found=true customresourcedefinition dash0operatorconfigurations.operator.dash0.com
kubectl delete --ignore-not-found=true customresourcedefinition persesdashboards.perses.dev
kubectl delete --ignore-not-found=true customresourcedefinition prometheusrules.monitoring.coreos.com
kubectl delete --ignore-not-found=true -f test-resources/customresources/priorityclass/priorityclasses.yaml
kubectl delete --ignore-not-found=true -f test-resources/cert-manager/certificate-and-issuer.yaml || true

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
