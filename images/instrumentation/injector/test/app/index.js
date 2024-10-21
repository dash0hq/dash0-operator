// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process');

function echoEnvVar(envVarName) {
  const envVarValue = process.env[envVarName];
  if (!envVarValue) {
    process.stdout.write(`${envVarName}: -`);
  } else {
    process.stdout.write(`${envVarName}: ${envVarValue}`);
  }
}

function main () {
  const testCase = process.argv[2];
  if (!testCase) {
    console.error("error: not enough arguments, the name of the test case needs to be specifed");
    process.exit(1)
  }

  switch (testCase) {
    case "non-existing":
      echoEnvVar("DOES_NOT_EXIST");
      break;
    case "term":
      echoEnvVar("TERM");
      break;
    case "node_options":
      echoEnvVar("NODE_OPTIONS");
      break;
    case "node_options_twice":
      echoEnvVar("NODE_OPTIONS");
      process.stdout.write("; ")
      echoEnvVar("NODE_OPTIONS");
      break;
    default:
      console.error(`unknown test case: ${testCase}`);
      process.exit(1)
  }
}

main();
