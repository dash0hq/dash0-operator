// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process');
const envVarName = 'OTEL_RESOURCE_ATTRIBUTES';
const expectedValue = undefined;

if (process.env[envVarName] !== expectedValue) {
  console.error(
    `Unexpected value for ${envVarName}: expected: '${expectedValue}'; actual: '${process.env[envVarName]}'`,
  );
  process.exit(1);
}
