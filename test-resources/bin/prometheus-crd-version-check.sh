#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

command -v go >/dev/null 2>&1 || {
  echo "Go needs to be installed but it isn't. Aborting."
  exit 1
}
command -v jq >/dev/null 2>&1 || {
  echo "jq needs to be installed but it isn't. Aborting."
  exit 1
}
command -v yq >/dev/null 2>&1 || {
  echo "yq needs to be installed but it isn't. Aborting."
  exit 1
}

moduleName=github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring
moduleVersion=$(go mod edit -json | jq -r ".Require[] | select(.Path==\"$moduleName\") | .Version")
if [[ -z "$moduleVersion" ]]; then
  echo "Cannot retrieve the module version for $moduleName. Aborting."
  exit 1
fi

moduleVersion=${moduleVersion:1} # cut of the leading "v" charactre

foundInReadme=false
chartReadme=helm-chart/dash0-operator/README.md
if grep "kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v$moduleVersion/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml" "$chartReadme" > /dev/null; then
  foundInReadme=true
fi

testCrdFile=test/util/crds/monitoring.coreos.com_prometheusrules.yaml
versionInTestCrd=$(yq e ".metadata.annotations.\"operator.prometheus.io/version\"" "$testCrdFile")
versionInTestCrdMatches=false
if [[ "$versionInTestCrd" == "$moduleVersion" ]]; then
  versionInTestCrdMatches=true
fi

if [[ "$foundInReadme" == "true" ]] && [[ "$versionInTestCrdMatches" == "true" ]]; then
  exit 0
fi

echo
echo "The version of the Go module $moduleName in go.mod ($moduleVersion) is not in sync with other references to its associated CRD. Maybe the Go module has been updated (e.g. by dependabot)? If so, other references have to be updated as well:"
echo
if [[ "$foundInReadme" != "true" ]]; then
  echo "* Please update the instruction in $chartReadme for installing the Prometheus CRD to read as follows:"
  echo "    kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v$moduleVersion/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml"
  echo
fi
if [[ "$versionInTestCrdMatches" != "true" ]]; then
  echo "* Please update $testCrdFile with the following command:"
   crdYamlUrl="https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v${moduleVersion}/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml"
  echo "    echo \"# $crdYamlUrl\" > $testCrdFile && curl $crdYamlUrl >> $testCrdFile"
  echo
fi

exit 1

