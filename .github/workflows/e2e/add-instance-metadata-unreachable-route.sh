#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Adds an unreachable route for the cloud instance metadata endpoint to every node of the kind cluster.
#
# The CI runner drops the packets to 169.254.169.254 rather than rejecting them. The gcp resource detector of the
# collector queries that address twice while it starts, in onGKE() and in onGCE() of
# github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp, and both calls pass context.TODO(), which
# carries no deadline. See also: https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/issues/1026.
# The timeout of the resourcedetection processor therefore cannot stop them. Each call runs into the dial timeout of
# 2s five times, which costs about 13s, so the collector needs about 80s to become ready instead of the 6s it needs when
# the endpoint answers.
#
# An unreachable route makes connect() fail at once with EHOSTUNREACH. The metadata client neither retries that error
# nor treats it as temporary, so the detector gives up immediately.
#
# Note: an iptables REJECT rule is the worse choice here. Both --reject-with tcp-reset and the default
# icmp-port-unreachable produce ECONNREFUSED, and syscallRetryable in retry_linux.go of
# cloud.google.com/go/compute/metadata retries exactly ECONNRESET and ECONNREFUSED. The five retries would still run,
# only the dial timeouts would be saved, not the backoff between them.
#
# Nothing in the kind cluster needs the instance metadata endpoint, so making it unreachable costs nothing. This works
# around the CI environment, it does not fix the underlying problem, which is that the gcp detector discards the
# deadline it is given, see https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/issues/1026.

set -euo pipefail

metadata_ip=169.254.169.254

add_route() {
  local node=$1
  docker exec "$node" ip route replace unreachable "$metadata_ip/32"
}

# An endpoint that is unreachable fails in milliseconds. A drop, which is what this works around, would keep curl busy
# until it runs into the timeout.
verify_route() {
  local node=$1
  docker exec "$node" \
    curl \
    --silent \
    --show-error \
    --max-time 5 \
    --output /dev/null \
    --write-out 'elapsed=%{time_total}s' \
    "http://$metadata_ip/" 2>&1 || true
}

main() {
  local kind_cluster="${DASH0_KIND_CLUSTER:?DASH0_KIND_CLUSTER needs to be set}"

  local nodes
  nodes=$(docker ps --filter "label=io.x-k8s.kind.cluster=$kind_cluster" --format '{{.Names}}')
  if [[ -z $nodes ]]; then
    echo "error: no running node container of cluster $kind_cluster found."
    exit 1
  fi

  local node
  for node in $nodes; do
    add_route "$node"
    printf '  %-34s unreachable route added, %s\n' "$node" "$(verify_route "$node")"
  done
}

main "$@"
