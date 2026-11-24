#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

if [ -f /etc/otelcol/conf-compressed/config.yaml ]; then
  if ! gunzip -c /etc/otelcol/conf-compressed/config.yaml > /etc/otelcol/conf/config.yaml; then
    echo "ERROR: Failed to decompress config.yaml" >&2
    exit 1
  fi
fi

# The GKE Autopilot WorkloadAllowlist of the cluster metrics collector permits no --feature-gates argument, so the
# operator requests these particular feature gates via environment variables there.
feature_gates=""
if [ "${DASH0_GKE_AUTOPILOT_WORKAROUND_EMIT_V0_K8S_CONVENTIONS:-}" = "true" ]; then
  feature_gates="-processor.k8sattributes.DontEmitV0K8sConventions"
fi
if [ "${DASH0_GKE_AUTOPILOT_WORKAROUND_SHARE_K8S_ATTRIBUTES_PROCESSOR:-}" = "true" ]; then
  feature_gates="${feature_gates:+${feature_gates},}processor.k8sattributes.ShareProcessorBetweenPipelines"
fi
if [ -n "${feature_gates}" ]; then
  set -- "$@" "--feature-gates=${feature_gates}"
fi

mkdir -p "$(dirname "${DASH0_COLLECTOR_PID_FILE}")"

# exec keeps the PID of this shell, so the pid file is valid for the collector as well. Replacing the shell with the
# collector makes the collector the container's main process, which receives the SIGTERM when the pod is stopped.
printf "%s" "$$" > "${DASH0_COLLECTOR_PID_FILE}"

printf "Collector pid file created at \"%s\": " "${DASH0_COLLECTOR_PID_FILE}"
cat "${DASH0_COLLECTOR_PID_FILE}"
echo

exec ./otelcol "$@"
