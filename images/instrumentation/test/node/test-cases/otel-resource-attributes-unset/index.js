// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const process = require('node:process');

if (process.env["OTEL_RESOURCE_ATTRIBUTES"] !== "k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name") {
  console.error(`Unexpected value for OTEL_RESOURCE_ATTRIBUTES: ${process.env["OTEL_RESOURCE_ATTRIBUTES"]}`);
  process.exit(1);
}
