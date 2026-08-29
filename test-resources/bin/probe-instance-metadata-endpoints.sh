#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

###############################################################################
# Diagnostic probe for the cloud instance metadata endpoints.
#
# The OpenTelemetry collector's resourcedetection processor runs the detectors eks, ecs, ec2, gcp, azure and aks on
# startup, and each of them resolves a host name and queries a cloud instance metadata endpoint. A name that resolves
# and an endpoint that answers or refuses the connection let a detector finish in milliseconds. A name server or an
# endpoint that silently drops the packets makes a detector block, and the collector container then needs much longer
# than usual to become ready.
#
# This script measures both name resolution and connectivity from the three network positions between the CI runner
# and a collector pod: the runner itself (one hop from the instance network interface), a kind node container (two
# hops) and a pod (three hops). A probe that runs into its timeout at one position but answers at the previous one
# shows where the responses are dropped. Two known causes:
# - The IMDSv2 hop limit on AWS, which has to be at least 2 for containers, see
#   https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-options.html.
# - A name server that drops the query for metadata.google.internal instead of answering NXDOMAIN. The Go resolver
#   cannot abort an in-flight cgo lookup when the context is cancelled, so such a lookup outlasts the timeout of the
#   resourcedetection processor.
#
# All probes are best-effort, the script only reports and always exits with 0.
###############################################################################

set -uo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root" || exit 0

# shellcheck source=./lib/constants
source "$scripts_lib/constants"
# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file

metadata_ip=169.254.169.254
metadata_host=metadata.google.internal
# Higher than the 2s timeout of the resourcedetection processor, so that a probe which hangs shows for how long it
# hangs instead of only that it does.
probe_timeout=10
busybox_image=busybox:1.38.0-glibc

# Command prefix for the curl probes, empty for the runner itself, "docker exec <node>" for a kind node.
probe_prefix=()

curl_probe() {
  local label=$1
  local method=$2
  local url=$3
  local header=${4:-}

  local cmd=()
  if [[ ${#probe_prefix[@]} -gt 0 ]]; then
    cmd=("${probe_prefix[@]}")
  fi
  cmd+=(
    curl
    --silent
    --show-error
    --max-time "$probe_timeout"
    --output /dev/null
    --write-out 'http_code=%{http_code} elapsed=%{time_total}s'
    --request "$method"
  )
  if [[ -n $header ]]; then
    cmd+=(--header "$header")
  fi
  cmd+=("$url")

  local output
  output=$("${cmd[@]}" 2>&1)
  printf '  %-20s %s\n' "$label" "$(echo "$output" | tr '\n' ' ')"
}

# Name resolution is measured separately from connectivity. The gcp detector resolves metadata.google.internal, and a
# lookup that neither answers nor fails fast blocks it beyond the timeout of the resourcedetection processor, because
# an in-flight cgo lookup does not end when the context is cancelled.
dns_probe() {
  local host=$1

  local cmd=()
  if [[ ${#probe_prefix[@]} -gt 0 ]]; then
    cmd=("${probe_prefix[@]}")
  fi
  cmd+=(timeout "$probe_timeout" getent ahosts "$host")

  local start=$SECONDS
  local output
  output=$("${cmd[@]}" 2>&1)
  local exit_code=$?
  printf '  %-20s exit=%s elapsed=%ss %s\n' \
    "dns:$host" "$exit_code" "$((SECONDS - start))" "$(echo "$output" | head -1)"
}

run_probes() {
  dns_probe "$metadata_host"
  curl_probe aws-imds-v2-token PUT "http://$metadata_ip/latest/api/token" \
    'X-aws-ec2-metadata-token-ttl-seconds: 60'
  curl_probe aws-imds-v1 GET "http://$metadata_ip/latest/meta-data/"
  curl_probe azure-imds GET "http://$metadata_ip/metadata/instance?api-version=2021-02-01" \
    'Metadata: true'
  curl_probe gcp-metadata-ip GET "http://$metadata_ip/computeMetadata/v1/instance/" \
    'Metadata-Flavor: Google'
  curl_probe gcp-metadata-dns GET "http://$metadata_host/computeMetadata/v1/instance/" \
    'Metadata-Flavor: Google'
}

probe_runner() {
  echo "position: the CI runner itself (one hop from the instance network interface)"
  probe_prefix=()
  run_probes
  echo
}

probe_kind_nodes() {
  local kind_cluster=$1
  # Note: `kind get nodes` reports an unknown cluster on stdout and still exits with 0, hence the node containers are
  # looked up via the label that kind puts on them.
  local nodes
  nodes=$(docker ps --filter "label=io.x-k8s.kind.cluster=$kind_cluster" --format '{{.Names}}' 2>&1)
  if [[ -z $nodes ]]; then
    echo "position: kind node containers - skipped, no running node container of cluster $kind_cluster found"
    echo
    return
  fi

  local node
  for node in $nodes; do
    echo "position: kind node container $node (two hops)"
    probe_prefix=(docker exec "$node")
    run_probes
    echo
  done
}

# The snippet that runs inside the probe pod. busybox has no curl, hence wget, which cannot send a PUT request; the
# reachability of the metadata endpoint is what matters here, not the IMDSv2 handshake.
pod_probe_snippet() {
  cat <<EOF
probe() {
  start=\$(date +%s)
  if [ -n "\$3" ]; then
    out=\$(wget -T $probe_timeout -q -O /dev/null --header "\$3" "\$2" 2>&1)
  else
    out=\$(wget -T $probe_timeout -q -O /dev/null "\$2" 2>&1)
  fi
  exit_code=\$?
  printf '  %-20s exit=%s elapsed=%ss %s\n' "\$1" "\$exit_code" "\$((\$(date +%s) - start))" "\$out"
}
dns_start=\$(date +%s)
dns_out=\$(nslookup $metadata_host 2>&1)
dns_exit=\$?
printf '  %-20s exit=%s elapsed=%ss %s\n' \\
  "dns:$metadata_host" "\$dns_exit" "\$((\$(date +%s) - dns_start))" "\$(echo "\$dns_out" | tail -2 | tr '\n' ' ')"
probe aws-imds-v1 "http://$metadata_ip/latest/meta-data/" ""
probe azure-imds "http://$metadata_ip/metadata/instance?api-version=2021-02-01" "Metadata: true"
probe gcp-metadata-ip "http://$metadata_ip/computeMetadata/v1/instance/" "Metadata-Flavor: Google"
probe gcp-metadata-dns "http://metadata.google.internal/computeMetadata/v1/instance/" "Metadata-Flavor: Google"
EOF
}

probe_pod() {
  local kind_cluster=$1
  echo "position: a pod, that is, where the collector runs (three hops)"

  local output
  if ! output=$(docker pull "$busybox_image" 2>&1); then
    echo "  skipped, cannot pull $busybox_image: $output"
    echo
    return
  fi
  if ! output=$(kind load docker-image "$busybox_image" --name "$kind_cluster" 2>&1); then
    echo "  skipped, cannot load $busybox_image into cluster $kind_cluster: $output"
    echo
    return
  fi

  kubectl run \
    "instance-metadata-probe-$RANDOM" \
    --rm \
    --attach \
    --quiet \
    --restart=Never \
    --image="$busybox_image" \
    --image-pull-policy=Never \
    --command -- sh -c "$(pod_probe_snippet)" 2>&1
  echo
}

main() {
  local kind_cluster="${DASH0_KIND_CLUSTER:-$default_kind_cluster}"

  echo "Probing the cloud instance metadata endpoints that the collector's resourcedetection processor queries."
  echo "An endpoint that is reachable answers in milliseconds. An elapsed time of ${probe_timeout}s is the probe"
  echo "timeout, which means that the endpoint neither answered nor refused the connection."
  echo

  probe_runner
  probe_kind_nodes "$kind_cluster"
  probe_pod "$kind_cluster"
}

main "$@"
