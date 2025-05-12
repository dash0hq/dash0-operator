// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main() {
  char* name = "AN_ENVIRONMENT_VARIABLE";
  char* actual = getenv(name);
  char* expected = "value";
  if (actual == NULL) {
    printf("Unexpected value for the environment variable %s -- expected: %s, was: null\n", name, expected);
    return 1;
  }
  if (strcmp(expected, actual) != 0) {
    printf("Unexpected value for the environment variable %s -- expected: %s, was: %s\n", name, expected, actual);
    return 1;
  }
}
