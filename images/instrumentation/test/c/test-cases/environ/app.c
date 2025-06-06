// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

extern char** __environ;
// extern char** __my_environ;

int main() {
  printf("app.c#main(): START\n");

  printf("app.c#main(): XXX __environ outer pointer: %p\n", __environ);
  printf("app.c#main(): XXX __environ content: %s\n", *__environ);
  for (char **e = __environ; *e; e++) {
     printf("RAW __environ value: '%s'\n", *e);
  }
  // char* val = getenv("OTEL_RESOURCE_ATTRIBUTES");
  // printf("app.c#main(): OTEL_RESOURCE_ATTRIBUTES: %s\n", val);

  // printf("app.c#main(): XXX __my_environ outer pointer: %p\n", __my_environ);
  // printf("app.c#main(): XXX __my_environ inner pointer: %p\n", *__my_environ);
  // printf("app.c#main(): XXX __my_environ content: %s\n", *__my_environ);
  // for (char **e2 = __my_environ; *e2; e2++) {
  //   printf("RAW __my_environ value: '%s'\n", *e2);
  //   break;
  // }

  // // Aliases: https://sourceware.org/git/?p=glibc.git;a=blob;f=posix/environ.c
  // for (char **e = _environ; *e; e++) {
  //    printf("RAW _environ value: '%s'\n", *e);
  // }
  // for (char **e = environ; *e; e++) {
  //   printf("RAW environ value: '%s'\n", *e);
  // }

  // expect_getenv_value("EXISTING_VAR_1", "original-value-1");
  printf("app.c#main() END\n");
}
