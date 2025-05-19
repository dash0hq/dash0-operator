// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>

extern char** __environ;
extern void change_values();

void print_char_ptr_of_ptr(char** str_ptr) {

  int idx = 0;
  for (char **kv_pair = str_ptr; *kv_pair; kv_pair++) {
     printf("app.c: __environ address kv pair %d: '%s'\n", idx, *kv_pair);
	 idx++;
  }
}

int main() {
  printf("app.c: START\n");

  printf("app.c: __environ address (initial): %p\n", __environ);
  print_char_ptr_of_ptr(__environ);
  printf("app.c: getenv(\"PATH\"): %p\n", getenv("PATH"));
  printf("app.c: getenv(\"VAR3\"): %s\n", getenv("VAR3"));
  printf("app.c: getenv(\"VAR4\"): %s\n", getenv("VAR4"));

  printf("app.c: calling change_values\n");
  change_values();
  printf("app.c: --------\n");

  printf("app.c: __environ address (after): %p\n", __environ);
  print_char_ptr_of_ptr(__environ);
  printf("app.c: getenv(\"PATH\"): %p\n", getenv("PATH"));
  printf("app.c: getenv(\"VAR5\"): %s\n", getenv("VAR5"));
  printf("app.c: getenv(\"VAR6\"): %s\n", getenv("VAR6"));
  printf("app.c: getenv(\"VAR7\"): %s\n", getenv("VAR7"));
  printf("app.c: --------\n");

  printf("app.c: changing values from C via putenv\n");
  putenv("VAR6=changed");
  putenv("NEW_VAR=new");
  print_char_ptr_of_ptr(__environ);
  printf("app.c: getenv(\"PATH\"): %p\n", getenv("PATH"));
  printf("app.c: getenv(\"VAR5\"): %s\n", getenv("VAR5"));
  printf("app.c: getenv(\"VAR6\"): %s\n", getenv("VAR6"));
  printf("app.c: getenv(\"VAR7\"): %s\n", getenv("VAR7"));
  printf("app.c: getenv(\"NEW_VAR\"): %s\n", getenv("NEW_VAR"));
  printf("app.c: --------\n");

  printf("app.c: changing values from C via setenv\n");
  setenv("VAR6", "changed again", 1);
  setenv("NEW_VAR_2", "also new", 0);
  print_char_ptr_of_ptr(__environ);
  printf("app.c: getenv(\"PATH\"): %p\n", getenv("PATH"));
  printf("app.c: getenv(\"VAR5\"): %s\n", getenv("VAR5"));
  printf("app.c: getenv(\"VAR6\"): %s\n", getenv("VAR6"));
  printf("app.c: getenv(\"VAR7\"): %s\n", getenv("VAR7"));
  printf("app.c: getenv(\"NEW_VAR\"): %s\n", getenv("NEW_VAR"));
  printf("app.c: getenv(\"NEW_VAR_2\"): %s\n", getenv("NEW_VAR_2"));
  printf("app.c: --------\n");

  printf("app.c: END\n-----------------\n\n");
}

