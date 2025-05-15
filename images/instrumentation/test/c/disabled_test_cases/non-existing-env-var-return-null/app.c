// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>

int main() {
  char* name = "DOES_NOT_EXIST";
  char* actual = getenv(name);
  if (actual != NULL) {
    printf("Unexpected value for the environment variable %s -- expected: null, was: %s\n", name, actual);
    return 1;
  }
}
