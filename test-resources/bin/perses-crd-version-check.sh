#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

source test-resources/bin/third-party-crd-version-check-util

module_name=github.com/perses/perses-operator
chart_readme=helm-chart/dash0-operator/README.md
unit_test_crd_file=test/util/crds/perses.dev_persesdashboards.yaml
test_resources_util_file=test-resources/bin/util

get_module_version_from_go_mod "$module_name"

remote_crd_url="https://raw.githubusercontent.com/perses/perses-operator/refs/tags/v$module_version/config/crd/bases/perses.dev_persesdashboards.yaml"
kubectl_apply="kubectl apply --server-side -f $remote_crd_url"

check_all
