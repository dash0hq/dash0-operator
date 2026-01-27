#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

namespace="${1:-}"

if [[ -z $namespace ]]; then
  echo Missing mandatory parameter: namespace
  exit 1
fi

resource_types=$(kubectl api-resources --verbs=list --namespaced -o name)

while IFS= read -r resource_type; do
  resources=$(kubectl get -n "$namespace" "$resource_type" 2>&1)
  if [[ ! "$resources" =~ ^"No resources found in " ]]; then
    echo "Resource type $resource_type:"
    echo "$resources"
    echo
  fi
done <<< "$resource_types"
