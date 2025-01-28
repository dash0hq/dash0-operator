#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

source test-resources/bin/util
load_env_file

# shellcheck disable=SC2002
cat test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml.template | DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" envsubst > test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.token.yaml
# shellcheck disable=SC2002
cat test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.secret.yaml.template | DASH0_INGRESS_ENDPOINT="$DASH0_INGRESS_ENDPOINT" DASH0_AUTHORIZATION_TOKEN="$DASH0_AUTHORIZATION_TOKEN" DASH0_API_ENDPOINT="$DASH0_API_ENDPOINT" envsubst > test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.secret.yaml
