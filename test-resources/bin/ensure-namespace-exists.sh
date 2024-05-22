#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

target_namespace=${1:-default}

if [[ "${target_namespace}" == default  ]]; then
  exit 0
fi

if ! kubectl get ns ${target_namespace} &> /dev/null; then
  kubectl create ns ${target_namespace}
fi