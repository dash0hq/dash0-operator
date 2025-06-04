// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process');

// TODO create a test case that starts a child process, verify that we do not apply modifications twice, because
// the modified environment is passed down to the child process and then modified again.

if (process.env['AN_ENVIRONMENT_VARIABLE'] !== 'value') {
  console.error(`Unexpected value for AN_ENVIRONMENT_VARIABLE: ${process.env['AN_ENVIRONMENT_VARIABLE']}`);
  process.exit(1);
}
