// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#define _GNU_SOURCE

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dlfcn.h>

int main() {
  // We need to include dlfcn.h and use at least one symbol from dlfcn.h, otherwise, on systems where libc-xxx.so
  // (providing almost all libc things) and libdl-xxx.so (providing only dlopen, dlcose, dlsysm and dlerror) are
  // actually two different shared libraries (which is the case on some older distributions, like Debian bullseye), the
  // dynamic linker might decide to not load libdl-xxx.so at all, hence the dlsym lookup in the injector would not
  // succeed.
  dlerror();

  // The test for OTEL_RESOURCE_ATTRIBUTES works independently of whether we override secure_getenv, since it relies on
  // the injector putting the updated OTEL_RESOURCE_ATTRIBUTES into __environ via the setenv call in
  // root.zig/initEnviron.
  char* name = "OTEL_RESOURCE_ATTRIBUTES";
  char* actual = secure_getenv(name);
  char* expected = "k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name";
  if (actual == NULL) {
    fprintf(stderr, "Unexpected value for the environment variable %s --\nexpected: %s\n     was: null\n", name, expected);
    return 1;
  }
  if (strcmp(expected, actual) != 0) {
    fprintf(stderr, "Unexpected value for the environment variable %s --\nexpected: %s\n     was: %s\n", name, expected, actual);
    return 1;
  }

  // Also check NODE_OPTIONS, since that actually depends on the getenv/secure_getenv override.
  name = "NODE_OPTIONS";
  actual = secure_getenv(name);
  expected = "--require /__dash0__/instrumentation/node.js/node_modules/@dash0/opentelemetry";
  if (actual == NULL) {
    fprintf(stderr, "Unexpected value for the environment variable %s --\nexpected: %s\n     was: null\n", name, expected);
    return 1;
  }
  if (strcmp(expected, actual) != 0) {
    fprintf(stderr, "Unexpected value for the environment variable %s --\nexpected: %s\n     was: %s\n", name, expected, actual);
    return 1;
  }
}
