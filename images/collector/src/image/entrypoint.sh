#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

if [ -f /etc/otelcol/conf-compressed/config.yaml ]; then
  if ! gunzip -c /etc/otelcol/conf-compressed/config.yaml > /etc/otelcol/conf/config.yaml; then
    echo "ERROR: Failed to decompress config.yaml" >&2
    exit 1
  fi
fi

# Non-fatal preflight: on SELinux-enforcing nodes the default container_t type cannot read the container_log_t-labelled
# pod logs while listing the directory still works, so log collection silently produces nothing. This runs regardless
# of the current config, since log collection can be enabled later via a config reload without a container restart.
POD_LOGS_DIR=/var/log/pods
pod_log_attempts=0
pod_log_readable=false
pod_log_denied=false
# A glob instead of find -type: it does not need to stat the (possibly symlinked) log files, which SELinux can deny.
for log_file in "${POD_LOGS_DIR}"/*/*/*.log; do
  [ "${log_file}" = "${POD_LOGS_DIR}/*/*/*.log" ] && break
  if read_error=$(head -c 1 "${log_file}" 2>&1 >/dev/null); then
    pod_log_readable=true
    break
  fi
  # Only EACCES counts, a log file removed by pod churn between the glob and the read must not trigger the error.
  case "${read_error}" in
    *"Permission denied"*) pod_log_denied=true ;;
  esac
  pod_log_attempts=$((pod_log_attempts + 1))
  [ "${pod_log_attempts}" -ge 5 ] && break
done
if [ "${pod_log_readable}" = false ] && [ "${pod_log_denied}" = true ]; then
  selinux_context=$(cat /proc/self/attr/current 2>/dev/null)
  echo "ERROR: permission denied when reading pod logs under ${POD_LOGS_DIR}." >&2
  echo "ERROR: until resolved, log collection (if enabled) will silently produce no logs on this node." >&2
  case "${selinux_context}" in
    *:spc_t:* | *:unconfined_t:* | *:super_t:*)
      echo "ERROR: the collector already runs with the unconfined SELinux context ${selinux_context}." >&2
      echo "ERROR: check the ownership and file mode of the pod log files on the node." >&2
      ;;
    *:*_t:*)
      echo "ERROR: the collector runs with SELinux context ${selinux_context}, which cannot read container_log_t logs." >&2
      echo "ERROR: fix: set operator.collectors.daemonSetSeLinuxOptions.type=spc_t." >&2
      ;;
    *)
      echo "ERROR: check the ownership and file mode of the pod log files on the node." >&2
      ;;
  esac
fi

./otelcol "$@" &

DASH0_COLLECTOR_PID=$!

mkdir -p "$(dirname "${DASH0_COLLECTOR_PID_FILE}")"

printf "%s" "${DASH0_COLLECTOR_PID}" > "${DASH0_COLLECTOR_PID_FILE}"

printf "Collector pid file created at \"%s\": " "${DASH0_COLLECTOR_PID_FILE}"
cat "${DASH0_COLLECTOR_PID_FILE}"
echo

wait ${DASH0_COLLECTOR_PID}
