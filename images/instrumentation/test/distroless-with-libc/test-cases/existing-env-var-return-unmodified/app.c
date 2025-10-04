// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

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

  char* name = "AN_ENVIRONMENT_VARIABLE";
  char* actual = getenv(name);
  char* expected = "value";
  if (actual == NULL) {
    fprintf(stderr, "Unexpected value for the environment variable %s --\nexpected: %s\n     was: null\n", name, expected);
    return 1;
  }
  if (strcmp(expected, actual) != 0) {
    fprintf(stderr, "Unexpected value for the environment variable %s --\nexpected: %s\n     was: %s\n", name, expected, actual);
    return 1;
  }
}
