// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process');

if (
  process.env['NODE_OPTIONS'] !== '--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry'
) {
  console.error(`Unexpected value for NODE_OPTIONS: ${process.env['NODE_OPTIONS']}`);
  process.exit(1);
}
