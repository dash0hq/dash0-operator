// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process');

if (process.env["OTEL_RESOURCE_ATTRIBUTES"] !== "") {
  console.error(`Unexpected value for OTEL_RESOURCE_ATTRIBUTES: ${process.env["OTEL_RESOURCE_ATTRIBUTES"]}`);
  process.exit(1);
}
