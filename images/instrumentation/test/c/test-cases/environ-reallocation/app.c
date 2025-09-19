// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dlfcn.h>

void expect_getenv_value(char* name, char* expected) {
  char* actual = getenv(name);
  if (actual == NULL) {
    fprintf(stderr, "Unexpected value for the environment variable %s --\nexpected: %s\n     was: null\n", name, expected);
    exit(1);
  }
  if (strcmp(expected, actual) != 0) {
    fprintf(stderr, "Unexpected value for the environment variable %s --\nexpected: %s\n     was: %s\n", name, expected, actual);
    exit(1);
  }
}

void expect_getenv_null(const char* name) {
  char* actual = getenv(name);
  if (actual != NULL) {
    fprintf(stderr, "Unexpected value for the environment variable %s -- expected: null, was: %s\n", name, actual);
    exit(1);
  }
}

void expect_putenv_no_err(char* arg) {
  int err = putenv(arg);
  if (err != 0) {
    fprintf(stderr, "putenv(%s) failed, return code: %d\n", arg, err);
    exit(1);
  }
}

void expect_setenv_no_err(const char *name, const char *value, int replace) {
  int err = setenv(name, value, replace);
  if (err != 0) {
    fprintf(stderr, "setenv(%s, %s, %d) failed, return code: %d\n", name, value, replace, err);
    exit(1);
  }
}

int main() {
  // We need to include dlfcn.h and use at least one symbol from dlfcn.h, otherwise, on systems where libc-xxx.so
  // (providing almost all libc things) and libdl-xxx.so (providing only dlopen, dlcose, dlsysm and dlerror) are
  // actually two different shared libraries (which is the case on some older distributions, like Debian bullseye), the
  // dynamic linker might decide to not load libdl-xxx.so at all, hence the dlsym lookup in the injector would not
  // succeed.
  dlerror();

  int number_of_calls = 5000;

  // putenv
  for (int i = 0; i < number_of_calls; i++) {
    char *env_var = (char*) malloc(55 * sizeof(char));
    sprintf(env_var, "PUT_ENV_TEST_ENVIRONMENT_VARIABLE_%04d=test value %04d", i, i);
    expect_putenv_no_err(env_var);
    // deliberately not freeing env_var
  }
  for (int i = 0; i < number_of_calls; i++) {
    char *name = (char*) malloc(40 * sizeof(char));
    sprintf(name, "PUT_ENV_TEST_ENVIRONMENT_VARIABLE_%04d", i);
    char *expected = (char*) malloc(18 * sizeof(char));
    sprintf(expected, "test value %04d", i);
    expect_getenv_value(name, expected);
    free(name);
    free(expected);
  }

  // setenv, no replace
  for (int i = 0; i < number_of_calls; i++) {
    char *name = (char*) malloc(40 * sizeof(char));
    sprintf(name, "SET_ENV_TEST_ENVIRONMENT_VARIABLE_%04d", i);
    char *value = (char*) malloc(18 * sizeof(char));
    sprintf(value, "test value %04d", i);
    expect_setenv_no_err(name, value, 0);
    // deliberately not freeing name or value
  }
  for (int i = 0; i < number_of_calls; i++) {
    char *name = (char*) malloc(40 * sizeof(char));
    sprintf(name, "SET_ENV_TEST_ENVIRONMENT_VARIABLE_%04d", i);
    char *expected = (char*) malloc(18 * sizeof(char));
    sprintf(expected, "test value %04d", i);
    expect_getenv_value(name, expected);
    free(name);
    free(expected);
  }

  // setenv, with replace
  for (int i = 0; i < number_of_calls; i++) {
    char *name = (char*) malloc(40 * sizeof(char));
    sprintf(name, "SET_ENV_TEST_ENVIRONMENT_VARIABLE_%04d", i);
    char *value = (char*) malloc(18 * sizeof(char));
    sprintf(value, "test value %04d", i);
    expect_setenv_no_err(name, value, 1);
    // deliberately not freeing name or value
  }
  for (int i = 0; i < number_of_calls; i++) {
    char *name = (char*) malloc(40 * sizeof(char));
    sprintf(name, "SET_ENV_TEST_ENVIRONMENT_VARIABLE_%04d", i);
    char *expected = (char*) malloc(18 * sizeof(char));
    sprintf(expected, "test value %04d", i);
    expect_getenv_value(name, expected);
    free(name);
    free(expected);
  }

  expect_getenv_null("DOES_NOT_EXIST");
}
