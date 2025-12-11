#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# See ./README.md for documentation.

set -eu

cd "$(dirname "${BASH_SOURCE[0]}")"

prefix="${1:-}"
if [[ -n "$prefix" ]]; then
  prefix="${prefix}-"
fi

# Kubernetes context to use
target_ctx=dash0-operator-test-bk

# Kubernetes namespace to use
target_namespace=operator-k8s-api-load-test

# number of different deployment names to be used
max_deployments=200

# number of replicaset revisions to create per deployment. The script will create
# $max_deployments * $revisions_per_deployment replicasets, but limited by $max_number_of_replicasets.
revisions_per_deployment=50

# The script will stop once >= $max_number_of_replicasets replicasets exist in the target namespace.
# (The check is independent of whether the replicasets have been created by this script.)
max_number_of_replicasets=10000

# Checking the number of existing replicasets after each kubect apply is costly and slows down the script, so
# the number is only checked after every $check_number_every_nth_iteration loop iteration. This might lead to
# creating a slightly higher number than specified by $max_number_of_replicasets, but getting exact number of
# replicasets is not really relevant.
check_number_every_nth_iteration=100

kubectl --context "$target_ctx" create ns "$target_namespace" || true

iteration=0
for _ in $(seq 1 $revisions_per_deployment); do
  for deployment_idx in $(seq 1 $max_deployments); do
    # echo "deployment: $deployment_idx, revision: $revision, random_annotation: $random_annotation"
    name="${prefix}test-deployment-${deployment_idx}"
    selector="${name}-app"
    manifest_filename="${prefix}deployment.yaml"
    # shellcheck disable=SC2002
    random_annotation=$(cat /dev/urandom | LC_ALL=C tr -dc 'a-zA-Z0-9' | fold -w 10 | head -n 1)
    # shellcheck disable=SC2002
    cat \
      deployment.yaml.template | \
      NAME="$name" \
      SELECTOR="$selector" \
      RANDOM_ANNOTATION="$random_annotation" \
      envsubst > \
      "$manifest_filename"
    kubectl --context "$target_ctx" apply -n "$target_namespace" -f "$manifest_filename" || true

    echo $iteration
    ((++iteration))
    if [[ $iteration -ge $check_number_every_nth_iteration ]]; then
      iteration=0
      number_of_replicasets=$(kubectl --context "$target_ctx" get replicasets -n "$target_namespace" --no-headers | wc -l)
      if [[ $number_of_replicasets -ge $max_number_of_replicasets ]]; then
        echo "$number_of_replicasets now exist in the namespace $target_namespace"
        exit 0
      fi
    fi
  done
done

