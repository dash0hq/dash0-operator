// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>

extern char** my_environ;
extern void change_values();

void print_char_ptr_of_ptr(char** str_ptr) {
  int idx = 0;
  for (char **kv_pair = str_ptr; *kv_pair; kv_pair++) {
     printf("app.c: my_environ address kv pair %d: '%s'\n", idx, *kv_pair);
	 idx++;
  }
}

int main() {
  printf("app.c: START\n");

  printf("app.c: my_environ address (initial): %p\n", my_environ);
  print_char_ptr_of_ptr(my_environ);

  printf("app.c: calling change_values\n");
  change_values();
  printf("app.c: --------\n");

  printf("app.c: my_environ address (after): %p\n", my_environ);
  print_char_ptr_of_ptr(my_environ);

  printf("app.c: END\n-----------------\n\n");
}

