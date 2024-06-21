// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>

void echo_env_var(const char* name) {
  fputs(name, stdout);
  fputs(": ", stdout);

  char* value = getenv(name);
  if (value != NULL) {
    fputs(value, stdout);
  } else {
    fputs("not set", stdout);
  }
  fputs("; ", stdout);
}

int main(void) {
    echo_env_var("TERM");
    echo_env_var("NODE_OPTIONS");
}


