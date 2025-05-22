#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

./otelcol "$@" &

DASH0_COLLECTOR_PID=$!

mkdir -p "$(dirname "${DASH0_COLLECTOR_PID_FILE}")"

printf "%s" "${DASH0_COLLECTOR_PID}" > "${DASH0_COLLECTOR_PID_FILE}"

printf "Collector pid file created at \"%s\": " "${DASH0_COLLECTOR_PID_FILE}"
cat "${DASH0_COLLECTOR_PID_FILE}"
echo

wait ${DASH0_COLLECTOR_PID}
