// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process');

// The Dash0 Node.js OpenTelemetry distribution starts the NodeSDK, which registers a global tracer provider via the
// OpenTelemetry API. Checking the API's global registry (instead of require.cache, as the Node.js test case does) does
// not depend on whether Deno shares its CommonJS module cache between the preloaded module and the main module.
const otelApiGlobal = globalThis[Symbol.for('opentelemetry.js.api.1')];
if (!otelApiGlobal || !otelApiGlobal.trace) {
  console.error(`It looks like the Dash0 Node.js OpenTelemetry distribution has not been loaded.`);
  process.exit(1);
}
