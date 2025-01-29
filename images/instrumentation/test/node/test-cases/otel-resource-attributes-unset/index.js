// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process');
const envVarName = 'OTEL_RESOURCE_ATTRIBUTES';
const expectedValue = "k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name";

if (process.env[envVarName] !== expectedValue) {
  console.error(`Unexpected value for ${envVarName}: expected: '${expectedValue}'; actual: '${process.env[envVarName]}'`);
  process.exit(1);
}
