#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

target_namespace=${1:-test-namespace}

helm uninstall dash0-opentelemetry-collector-daemonset --namespace ${target_namespace} --ignore-not-found
