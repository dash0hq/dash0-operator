#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

###############################################################################
# Runs the gcp detector probe as a pod, that is, in the same network position as the collector.
#
# The probe of test-resources/gcp-detector-probe replicates the steps of the gcp resource detector one by one and logs
# the start and the end of each of them, which shows which step blocks and for how long. It has to run as a pod: the
# first metadata call of the detector only happens when KUBERNETES_SERVICE_HOST is set, which is the case inside a pod
# and nowhere else.
#
# The image is built locally and loaded into the kind cluster directly, so this needs no registry.
#
# The script only reports, it always exits with 0.
###############################################################################

set -uo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root" || exit 1

# shellcheck source=./lib/constants
source "$scripts_lib/constants"
# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file
verify_kubectx

probe_name=gcp-detector-probe
probe_namespace=gcp-detector-probe
probe_image="${DASH0_GCP_PROBE_IMAGE:-gcp-detector-probe:latest}"
probe_timeout_seconds="${DASH0_GCP_PROBE_TIMEOUT:-300}"

# shellcheck disable=SC2329  # invoked indirectly, via the EXIT trap in main
delete_probe_namespace() {
  kubectl delete namespace "$probe_namespace" --ignore-not-found --wait=false >/dev/null 2>&1
}

build_image() {
  docker build --tag "$probe_image" test-resources/gcp-detector-probe
}

load_image_into_cluster() {
  local kind_cluster=$1
  kind load docker-image "$probe_image" --name "$kind_cluster"
}

run_probe_pod() {
  kubectl create namespace "$probe_namespace" >/dev/null 2>&1
  kubectl label namespace "$probe_namespace" --overwrite dash0.com/enable=false >/dev/null 2>&1

  kubectl run \
    "$probe_name" \
    --namespace "$probe_namespace" \
    --image="$probe_image" \
    --image-pull-policy=Never \
    --restart=Never \
    --attach \
    --quiet \
    --pod-running-timeout="${probe_timeout_seconds}s"
}

main() {
  local kind_cluster="${DASH0_KIND_CLUSTER:-$default_kind_cluster}"

  echo "Probing the steps of the gcp resource detector from inside a pod."
  echo "image:   $probe_image"
  echo "cluster: $kind_cluster"
  echo

  trap 'delete_probe_namespace' EXIT

  local output
  if ! output=$(build_image 2>&1); then
    echo "error: cannot build the probe image:"
    echo "$output"
    exit 1
  fi
  if ! output=$(load_image_into_cluster "$kind_cluster" 2>&1); then
    echo "error: cannot load the probe image into cluster $kind_cluster:"
    echo "$output"
    exit 1
  fi

  run_probe_pod

  exit 0
}

main "$@"
