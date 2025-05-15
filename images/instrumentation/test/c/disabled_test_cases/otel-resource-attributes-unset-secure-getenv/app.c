// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#define _GNU_SOURCE

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main() {
  // TODO We do not hook into secure_getenv, so this test case currently fails.
  // For now, it is disabled by default.
  printf("! test case disabled: otel-resource-attributes-unset-secure-getenv\n");
  return 0;

  char* name = "OTEL_RESOURCE_ATTRIBUTES";
  char* actual = secure_getenv(name);
  char* expected = "k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name";
  if (actual == NULL) {
    printf("Unexpected value for the environment variable %s -- expected: %s, was: null\n", name, expected);
    return 1;
  }
  if (strcmp(expected, actual) != 0) {
    printf("Unexpected value for the environment variable %s -- expected: %s, was: %s\n", name, expected, actual);
    return 1;
  }
}
