// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

extern char ** __environ;
// extern char ** _environ;
// extern char ** environ;

extern char ** __my_environ;

void expect_getenv_value(char* name, char* expected) {
  char* actual = getenv(name);
  if (actual == NULL) {
    printf("Unexpected value for the environment variable %s -- expected: %s, was: null\n", name, expected);
    exit(1);
  }
  if (strcmp(expected, actual) != 0) {
    printf("Unexpected value for the environment variable %s -- expected: %s, was: %s\n", name, expected, actual);
    exit(1);
  }
}

void expect_getenv_null(const char* name) {
  char* actual = getenv(name);
  if (actual != NULL) {
    printf("Unexpected value for the environment variable %s -- expected: null, was: %s\n", name, actual);
    exit(1);
  }
}

void expect_putenv_no_err(char* arg) {
  int err = putenv(arg);
  if (err != 0) {
    printf("putenv(%s) failed, return code: %d\n", arg, err);
    exit(1);
  }
}

void expect_setenv_no_err(const char *name, const char *value, int replace) {
  int err = setenv(name, value, replace);
  if (err != 0) {
    printf("setenv(%s, %s, %d) failed, return code: %d\n", name, value, replace, err);
    exit(1);
  }
}

void expect_unsetenv_no_err(const char *arg) {
  int err = unsetenv(arg);
  if (err != 0) {
    printf("unsetenv(%s) failed, return code: %d\n", arg, err);
    exit(1);
  }
}

int main() {
  printf("app.c#main(): START'\n");
  printf("app.c#main(): XXX __environ outer pointer: %p\n", __environ);
  printf("app.c#main(): XXX __environ inner pointer: %p\n", *__environ);
  printf("app.c#main(): XXX __environ content: %s\n", *__environ);
  for (char **e = __environ; *e; e++) {
     printf("RAW __environ value: '%s'\n", *e);
  }
  // char* val = getenv("OTEL_RESOURCE_ATTRIBUTES");
  // printf("app.c#main(): OTEL_RESOURCE_ATTRIBUTES: %s\n", val);

  printf("app.c#main(): XXX __my_environ outer pointer: %p\n", __my_environ);
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
  printf("app.c#main() END'\n");
}
