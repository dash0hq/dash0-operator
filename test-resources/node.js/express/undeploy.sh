#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname ${BASH_SOURCE})"

target_namespace=${1:-default}
kind=${2:-deployment}

kubectl delete -n ${target_namespace} -f ${kind}.yaml --ignore-not-found || true
