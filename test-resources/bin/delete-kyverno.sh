#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Deletes Kyverno and the Kyverno policies installed by deploy-kyverno.sh from the cluster of the current kube context,
# together with the PolicyException for the collector DaemonSet, which lives in the Kyverno namespace.

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file
verify_kubectx

helm uninstall --namespace "$kyverno_namespace" --ignore-not-found --wait kyverno-policies
helm uninstall --namespace "$kyverno_namespace" --ignore-not-found --wait kyverno
kubectl delete namespace "$kyverno_namespace" --ignore-not-found
echo "Kyverno has been deleted."
