#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

if [ -f /etc/otelcol/conf-compressed/config.yaml ]; then
  if ! gunzip -c /etc/otelcol/conf-compressed/config.yaml > /etc/otelcol/conf/config.yaml; then
    echo "ERROR: Failed to decompress config.yaml" >&2
    exit 1
  fi
fi

./otelcol "$@" &

DASH0_COLLECTOR_PID=$!

mkdir -p "$(dirname "${DASH0_COLLECTOR_PID_FILE}")"

printf "%s" "${DASH0_COLLECTOR_PID}" > "${DASH0_COLLECTOR_PID_FILE}"

printf "Collector pid file created at \"%s\": " "${DASH0_COLLECTOR_PID_FILE}"
cat "${DASH0_COLLECTOR_PID_FILE}"
echo

wait ${DASH0_COLLECTOR_PID}
