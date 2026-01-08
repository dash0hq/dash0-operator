#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -eu

copy_options="-R"
if [ -n "${DASH0_COPY_INSTRUMENTATION_DEBUG:-}" ]; then
  set -x
  copy_options="-vR"
fi

cd -P -- "$(dirname -- "$0")"

if [ -z "${DASH0_INSTRUMENTATION_FOLDER_SOURCE:-}" ]; then
  DASH0_INSTRUMENTATION_FOLDER_SOURCE=/dash0-init-container
fi
if [ ! -d "${DASH0_INSTRUMENTATION_FOLDER_SOURCE}" ]; then
  >&2 echo "[Dash0] Instrumentation source directory ${DASH0_INSTRUMENTATION_FOLDER_SOURCE} does not exist."
  exit 1
fi

if [ -z "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION:-}" ]; then
  DASH0_INSTRUMENTATION_FOLDER_DESTINATION=/__otel_auto_instrumentation
fi

# We deliberately do not create the base directory for $DASH0_INSTRUMENTATION_FOLDER_DESTINATION via mkdir, it needs
# to be an existing mount point provided externally.
cp "$copy_options" "${DASH0_INSTRUMENTATION_FOLDER_SOURCE}"/* "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION}"

if [ -n "${DASH0_COPY_INSTRUMENTATION_DEBUG:-}" ]; then
  find "${DASH0_INSTRUMENTATION_FOLDER_DESTINATION}" -maxdepth 4
fi
