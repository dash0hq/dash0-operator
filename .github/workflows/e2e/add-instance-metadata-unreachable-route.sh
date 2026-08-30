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
# The route on its own is not enough, though. Linux throttles the generation of ICMP errors per destination via
# net.ipv4.icmp_ratelimit, one message per second by default. The first connections get their ICMP host unreachable at
# once, the ones after that get nothing and fall back to the retransmission of the SYN, which is why a measurement of
# the route alone showed the first two calls at 0s but the detector as a whole still at 18s, down from 28s. The rate
# limit is therefore disabled as well, which is what makes every call fail immediately rather than only the first few.
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

# Without this, only the first ICMP error per second reaches the caller and every connection after that waits for the
# retransmission of its SYN instead of failing at once.
disable_icmp_rate_limit() {
  local node=$1
  docker exec "$node" sysctl --quiet --write net.ipv4.icmp_ratelimit=0
}

# An endpoint that is unreachable fails in milliseconds. A single request would not tell the whole story, because the
# first one succeeds in failing fast even while the ICMP rate limit is in place, so the check makes five in a row. All
# five have to come back at once.
verify_route() {
  local node=$1
  local elapsed=""
  local remaining=5
  while ((remaining-- > 0)); do
    elapsed+="$(docker exec "$node" \
      curl \
      --silent \
      --max-time 5 \
      --output /dev/null \
      --write-out '%{time_total}s ' \
      "http://$metadata_ip/" 2>/dev/null || true)"
  done
  echo "$elapsed"
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
    disable_icmp_rate_limit "$node"
    printf '  %-34s five requests: %s\n' "$node" "$(verify_route "$node")"
  done
}

main "$@"
