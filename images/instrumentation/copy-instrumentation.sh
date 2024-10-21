#!/bin/sh

set -eu

if [ -n "${DASH0_DEBUG:-}" ]; then
  set -x
fi

cd -P -- "$(dirname -- "$0")"

if [ -z "${DASH0_INJECTOR_FOLDER_SOURCE:-}" ]; then
  DASH0_INJECTOR_FOLDER_SOURCE=/dash0-init-container
fi
if [ ! -d "${DASH0_INJECTOR_FOLDER_SOURCE}" ]; then
  >&2 echo "[Dash0] injector source directory ${DASH0_INJECTOR_FOLDER_SOURCE} does not exist."
  exit 1
fi
if [ -z "${DASH0_INSTRUMENTATION_FOLDER_SOURCE:-}" ]; then
  DASH0_INSTRUMENTATION_FOLDER_SOURCE=/dash0-init-container/instrumentation
fi
if [ ! -d "${DASH0_INSTRUMENTATION_FOLDER_SOURCE}" ]; then
  >&2 echo "[Dash0] Instrumentation source directory ${DASH0_INSTRUMENTATION_FOLDER_SOURCE} does not exist."
  exit 1
fi

if [ -z "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION:-}" ]; then
  DASH0_INSTRUMENTATION_FOLDER_DESTINATION=/__dash0__
fi

# We deliberately do not create the base directory for $DASH0_INSTRUMENTATION_FOLDER_DESTINATION via mkdir, it needs
# be an existing mount point provided externally.
cp -R "${DASH0_INSTRUMENTATION_FOLDER_SOURCE}"/ "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION}"

# Copy the injector from the init container to the monitored container.
cp "${DASH0_INJECTOR_FOLDER_SOURCE}"/*.so "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION}"/

if [ -n "${DASH0_DEBUG:-}" ]; then
  find "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION}"
fi
