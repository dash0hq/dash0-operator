// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdbool.h>

extern int my_char;
extern int my_int;
extern char* my_string;
extern char* my_nt_kv_str;
extern void change_values();

void print_nt_kv_str(char* label, char* str) {
  printf("app.c: %s: ", label);
  bool last_char_was_null = false;
  for (int i = 0; true; i++) {
    if (str[i] == '\0') {
      if (last_char_was_null) {
        printf("|NULL|");
        break;
      }
      last_char_was_null = true;
       printf("|NULL|");
    } else {
      last_char_was_null = false;
      printf("%c", str[i]);
    }
  }
  printf("\n");
}

int main() {
  printf("app.c: START\n");

  printf("app.c: my_char   (initial): %c\n", my_char);
  printf("app.c: my_int    (initial): %d\n", my_int);
  printf("app.c: my_string (initial): %s\n", my_string);
  print_nt_kv_str("my_nt_kv_str (initial)", my_nt_kv_str);

  printf("app.c: calling change_values\n");
  change_values();
  printf("app.c: --------\n");

  printf("app.c: my_char   (after): %c\n", my_char);
  printf("app.c: my_int    (after): %d\n", my_int);
  printf("app.c: my_string (after): %s\n", my_string);
  print_nt_kv_str("my_nt_kv_str (after)", my_nt_kv_str);

  printf("app.c: END\n-----------------\n\n");
}

