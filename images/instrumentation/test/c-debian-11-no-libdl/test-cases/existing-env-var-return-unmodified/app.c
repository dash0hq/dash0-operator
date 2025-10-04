// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// For distributions where libc and libdl are separate files (e.g. Debian 11), when the application under monitoring
// does not bind libdl, the injector will not find dlsym, hence we will not be able to inject. This test ensures that
// the injector does not interfere with normal getenv calls.
int main() {
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
