#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# See README.md for an explanation of this script and its purpose.

# MAINTENANCE NOTE: THIS SCRIPT *DELIBERATELY* DOES NOT RUN WITH set -e! See README.md for more details.
set -x

/bin/mkdir -p /var/otelcol/filelogreceiver_offsets
/bin/chown -R 65532:0 /var/otelcol/filelogreceiver_offsets

# log directory ownership and permission information for troubleshooting purposes
ls -lad /var/otelcol/filelogreceiver_offsets
ls -la /var/otelcol/filelogreceiver_offsets/
