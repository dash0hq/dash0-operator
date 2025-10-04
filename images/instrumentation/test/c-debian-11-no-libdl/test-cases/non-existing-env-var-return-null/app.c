// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// For distributions where libc and libdl are separate files (e.g. Debian 11), when the application under monitoring
// does not bind libdl, the injector will not find dlsym, hence we will not be able to inject. This test ensures that
// the injector does not interfere with normal getenv calls.
int main() {
  char* name = "DOES_NOT_EXIST";
  char* actual = getenv(name);
  if (actual != NULL) {
    fprintf(stderr, "Unexpected value for the environment variable %s -- expected: null, was: %s\n", name, actual);
    return 1;
  }
}
