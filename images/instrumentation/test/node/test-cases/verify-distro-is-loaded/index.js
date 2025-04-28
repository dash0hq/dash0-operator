// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process');

const loadedModules = Object.keys(require.cache);
if (
  !loadedModules.includes(
    '/__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry/dist/src/1.x/init.js',
  ) &&
  !loadedModules.includes('/__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry/dist/src/2.x/init.js')
) {
  console.error(`It looks like the Dash0 Node.js OpenTelemetry distribution has not been loaded.`);
  process.exit(1);
}
