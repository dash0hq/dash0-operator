#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"/../..

source test-resources/bin/util
load_env_file

cat test-resources/customresources/dash0monitoring/dash0monitoring.token.yaml.template | DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" envsubst > test-resources/customresources/dash0monitoring/dash0monitoring.token.yaml
cat test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml.template | DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" envsubst > test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml
