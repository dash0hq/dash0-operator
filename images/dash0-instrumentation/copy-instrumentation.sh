#!/bin/sh

set -eu

if [ -n "${DASH0_DEBUG:-}" ]; then
  set -x
fi

cd -P -- "$(dirname -- "$0")"

if [ -z "${DASH0_INSTRUMENTATION_FOLDER_SOURCE:-}" ]; then
  DASH0_INSTRUMENTATION_FOLDER_SOURCE=/dash0/instrumentation
fi
if [ ! -d "${DASH0_INSTRUMENTATION_FOLDER_SOURCE}" ]; then
  >&2 echo "[Dash0] Instrumentation source directory ${DASH0_INSTRUMENTATION_FOLDER_SOURCE} does not exist."
  exit 1
fi

if [ -z "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION:-}" ]; then
  DASH0_INSTRUMENTATION_FOLDER_DESTINATION=/opt/dash0
fi

cp -R "${DASH0_INSTRUMENTATION_FOLDER_SOURCE}"/ "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION}"

echo "[Dash0] Instrumentation files have been copied."

if [ -n "${DASH0_DEBUG:-}" ]; then
  find "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION}"
fi
