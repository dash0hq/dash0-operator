#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

source test-resources/bin/third-party-crd-version-check-util

module_name=github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring
chart_readme=helm-chart/dash0-operator/README.md
unit_test_crd_file=test/util/crds/monitoring.coreos.com_prometheusrules.yaml
test_resources_util_file=test-resources/bin/util

get_module_version_from_go_mod "$module_name"

remote_crd_url="https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v$module_version/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml"
kubectl_apply="kubectl apply --server-side -f $remote_crd_url"

check_all

echo "${BASH_SOURCE[0]}": All checks have passed.
