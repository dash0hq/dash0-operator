// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process')

if (process.env["UNDEFINED_ENVIRONMENT_VARIABLE"] !== undefined) {
  console.error(`Unexpected value for UNDEFINED_ENVIRONMENT_VARIABLE: ${process.env["UNDEFINED_ENVIRONMENT_VARIABLE"]}`);
  process.exit(1);
}