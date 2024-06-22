// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#define _GNU_SOURCE

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

void echo_env_var(const char* name) {
  fputs(name, stdout);
  fputs(": ", stdout);

  char* value = getenv(name);
  if (value != NULL) {
    fputs(value, stdout);
  } else {
    fputs("NULL", stdout);
  }
}

void echo_env_var_secure(const char* name) {
  fputs(name, stdout);
  fputs(": ", stdout);

  char* value = secure_getenv(name);
  if (value != NULL) {
    fputs(value, stdout);
  } else {
    fputs("NULL", stdout);
  }
}

int main(int argc, char* argv[]) {
  if (argc < 2) {
    fputs("not enough arguments, the name of the test case needs to be specifed", stdout);
    fputs("\n", stdout);
    exit(1);
  }
  char* test_case = argv[1];
  if (strcmp(test_case, "non-existing") == 0) {
    echo_env_var("DOES_NOT_EXIST");
  } else if (strcmp(test_case, "term") == 0) {
    echo_env_var("TERM");
  } else if (strcmp(test_case, "node_options") == 0) {
    echo_env_var("NODE_OPTIONS");
  } else if (strcmp(test_case, "node_options_twice") == 0) {
    echo_env_var("NODE_OPTIONS");
    fputs("; ", stdout);
    echo_env_var("NODE_OPTIONS");
  } else if (strcmp(test_case, "term-gnu-secure") == 0) {
    echo_env_var_secure("TERM");
  } else if (strcmp(test_case, "node_options-gnu-secure") == 0) {
    echo_env_var_secure("NODE_OPTIONS");
  } else if (strcmp(test_case, "node_options_twice-gnu-secure") == 0) {
    echo_env_var_secure("NODE_OPTIONS");
    fputs("; ", stdout);
    echo_env_var_secure("NODE_OPTIONS");
  } else {
    fputs("unknown test case: ", stdout);
    fputs(test_case, stdout);
    fputs("\n", stdout);
    exit(1);
  }
}

