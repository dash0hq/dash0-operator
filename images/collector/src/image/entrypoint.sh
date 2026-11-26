#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

if [ -f /etc/otelcol/conf-compressed/config.yaml ]; then
  if ! gunzip -c /etc/otelcol/conf-compressed/config.yaml > /etc/otelcol/conf/config.yaml; then
    echo "ERROR: Failed to decompress config.yaml" >&2
    exit 1
  fi
fi

mkdir -p "$(dirname "${DASH0_COLLECTOR_PID_FILE}")"

# exec keeps the PID of this shell, so the pid file is valid for the collector as well. Replacing the shell with the
# collector makes the collector the container's main process, which receives the SIGTERM when the pod is stopped.
printf "%s" "$$" > "${DASH0_COLLECTOR_PID_FILE}"

printf "Collector pid file created at \"%s\": " "${DASH0_COLLECTOR_PID_FILE}"
cat "${DASH0_COLLECTOR_PID_FILE}"
echo

exec ./otelcol "$@"
