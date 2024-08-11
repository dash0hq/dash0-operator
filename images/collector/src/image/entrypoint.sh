#!/bin/sh

./otelcol "$@" &

DASH0_COLLECTOR_PID=$!

mkdir -p $(dirname ${DASH0_COLLECTOR_PID_FILE})

echo -n "${DASH0_COLLECTOR_PID}" > ${DASH0_COLLECTOR_PID_FILE}

echo -n "Collector pid file created at '${DASH0_COLLECTOR_PID_FILE}': "
cat ${DASH0_COLLECTOR_PID_FILE}
echo

wait ${DASH0_COLLECTOR_PID}