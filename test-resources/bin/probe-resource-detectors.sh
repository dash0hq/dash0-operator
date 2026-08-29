#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

###############################################################################
# Isolates which resource detector of the collector's resourcedetection processor blocks on startup.
#
# The daemonset collector configures eight detectors and runs them once per pipeline on startup. When that takes much
# longer than the configured timeout of two seconds, the collector container does not become ready in time. This script
# finds the detector responsible: it starts a collector with a single pipeline, the resourcedetection processor as its
# only processor and exactly one detector configured, waits until the collector is ready, stops it and moves on to the
# next detector. The remaining resourcedetection settings are the ones the operator uses, see
# internal/collectors/otelcolresources/daemonset.config.yaml.template.
#
# For every detector the script reports how long the collector needed to become ready and, from the collector's own
# log, how long the detection itself took. A detector that respects the timeout finishes in at most two seconds. A
# detector that overruns it is the one to investigate. The final row runs all eight detectors together, which is the
# baseline the isolated runs have to add up to.
#
# The collector runs as a pod, that is, in the same network position as the real daemonset collector.
#
# Environment:
#   DASH0_PROBE_COLLECTOR_IMAGE  the collector image to probe, defaults to ${IMAGE_REPOSITORY_PREFIX}collector:latest
#   DASH0_PROBE_READY_TIMEOUT    seconds to wait for the collector to become ready, defaults to 90
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

probe_name=resource-detector-probe
probe_namespace=resource-detector-probe
collector_image="${DASH0_PROBE_COLLECTOR_IMAGE:-${IMAGE_REPOSITORY_PREFIX:-}collector:latest}"
ready_timeout_seconds="${DASH0_PROBE_READY_TIMEOUT:-90}"

# The detectors of the daemonset collector, in the order in which the operator configures them.
detectors=(eks ecs ec2 gcp azure aks k8snode system)

# The collector config for a probe run. Everything below `resourcedetection` except the detector list is copied
# verbatim from the daemonset template. The endpoints are bound to 0.0.0.0 rather than to the pod IP, which the
# template uses; the bind address has no bearing on resource detection and this way the pod needs no downward API
# value to start.
collector_config() {
  local detector_list=$1
  cat <<EOF
extensions:
  health_check:
    endpoint: "0.0.0.0:13133"

receivers:
  otlp:
    protocols:
      grpc:
        endpoint: "0.0.0.0:4317"

processors:
  resourcedetection:
    fail_on_missing_metadata: false
    detectors:
$detector_list
    timeout: 2s
    eks:
      node_from_env_var: K8S_NODE_NAME
    k8snode:
      node_from_env_var: K8S_NODE_NAME
    system:
      resource_attributes:
        host.name:
          enabled: false

exporters:
  debug: {}

service:
  extensions:
  - health_check
  pipelines:
    metrics:
      receivers:
      - otlp
      processors:
      - resourcedetection
      exporters:
      - debug
EOF
}

# The eks and the k8snode detector read the node object, which they find via node_from_env_var. Nothing else the probe
# runs needs a permission.
create_probe_infrastructure() {
  kubectl apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: $probe_namespace
  labels:
    dash0.com/enable: "false"
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: $probe_name
  namespace: $probe_namespace
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: $probe_name
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: $probe_name
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: $probe_name
subjects:
- kind: ServiceAccount
  name: $probe_name
  namespace: $probe_namespace
EOF
}

# shellcheck disable=SC2329  # invoked indirectly, via the EXIT trap in main
delete_probe_infrastructure() {
  kubectl delete namespace "$probe_namespace" --ignore-not-found --wait=false >/dev/null 2>&1
  kubectl delete clusterrolebinding "$probe_name" --ignore-not-found >/dev/null 2>&1
  kubectl delete clusterrole "$probe_name" --ignore-not-found >/dev/null 2>&1
}

apply_config_map() {
  local detector_list=$1
  kubectl apply -f - >/dev/null <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: $probe_name
  namespace: $probe_namespace
data:
  config.yaml: |
$(collector_config "$detector_list" | sed 's/^/    /')
EOF
}

create_collector_pod() {
  kubectl apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $probe_name
  namespace: $probe_namespace
spec:
  serviceAccountName: $probe_name
  restartPolicy: Never
  containers:
  - name: opentelemetry-collector
    image: $collector_image
    imagePullPolicy: Always
    args:
    - --config=file:/etc/otelcol/conf/config.yaml
    env:
    - name: K8S_NODE_NAME
      valueFrom:
        fieldRef:
          fieldPath: spec.nodeName
    readinessProbe:
      httpGet:
        path: /
        port: 13133
      periodSeconds: 1
      failureThreshold: $((ready_timeout_seconds + 30))
    volumeMounts:
    - name: config
      mountPath: /etc/otelcol/conf
      readOnly: true
  volumes:
  - name: config
    configMap:
      name: $probe_name
EOF
}

delete_collector_pod() {
  kubectl delete pod "$probe_name" --namespace "$probe_namespace" --ignore-not-found --wait >/dev/null 2>&1
}

# Converts an RFC 3339 timestamp of the collector log, for example 2026-08-29T16:40:36.859Z, into milliseconds since
# midnight. Two timestamps of one probe run are always on the same day, except across midnight, which the caller
# compensates for.
timestamp_to_milliseconds() {
  echo "$1" | awk -F'[T:.Z]' '{ printf "%d", ($2 * 3600 + $3 * 60 + $4) * 1000 + $5 }'
}

# Reports how long the resource detection itself took, from the two log lines the processor writes around it.
detection_duration() {
  local log=$1
  local began ended
  began=$(echo "$log" | grep -m 1 -F 'began detecting resource information' | awk '{ print $1 }')
  ended=$(echo "$log" | grep -m 1 -F 'detected resource information' | awk '{ print $1 }')
  if [[ -z $began ]]; then
    echo "no detection in log"
    return
  fi
  if [[ -z $ended ]]; then
    echo "unfinished"
    return
  fi

  local began_ms ended_ms delta_ms
  began_ms=$(timestamp_to_milliseconds "$began")
  ended_ms=$(timestamp_to_milliseconds "$ended")
  delta_ms=$((ended_ms - began_ms))
  if [[ $delta_ms -lt 0 ]]; then
    delta_ms=$((delta_ms + 86400000))
  fi
  printf '%d.%03ds' $((delta_ms / 1000)) $((delta_ms % 1000))
}

run_probe() {
  local label=$1
  local detector_list=$2

  apply_config_map "$detector_list"
  create_collector_pod

  local start=$SECONDS
  local ready_result
  if kubectl wait \
    --for=condition=Ready \
    "pod/$probe_name" \
    --namespace "$probe_namespace" \
    --timeout="${ready_timeout_seconds}s" >/dev/null 2>&1; then
    ready_result="ready after $((SECONDS - start))s"
  else
    ready_result="NOT READY within ${ready_timeout_seconds}s"
  fi

  local log
  log=$(kubectl logs "pod/$probe_name" --namespace "$probe_namespace" 2>&1)

  printf '  %-10s %-28s detection %s\n' "$label" "$ready_result" "$(detection_duration "$log")"

  delete_collector_pod
}

main() {
  echo "Probing the resource detectors of the collector one at a time."
  echo "image:   $collector_image"
  echo "timeout: ${ready_timeout_seconds}s per detector"
  echo "A detector that respects the configured timeout of the resourcedetection processor detects in at most 2s."
  echo

  trap 'delete_probe_infrastructure' EXIT
  if ! create_probe_infrastructure; then
    echo "error: cannot create the probe namespace and its RBAC resources."
    exit 1
  fi

  local detector
  for detector in "${detectors[@]}"; do
    run_probe "$detector" "    - $detector"
  done

  # All detectors together, the baseline the isolated runs have to add up to.
  local all_detectors=""
  for detector in "${detectors[@]}"; do
    all_detectors+="    - $detector"$'\n'
  done
  run_probe "all" "${all_detectors%$'\n'}"

  # A detector that does not become ready is a result of the probe, not a failure of the probe. Only the setup
  # failures above exit with a non-zero status.
  exit 0
}

main "$@"
