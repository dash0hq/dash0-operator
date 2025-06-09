// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>
#include <unistd.h>

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

int main() {
  pid_t pid = getpid();
  printf("app.c pid: %d\n", pid);

  // TODO check that the value of the __environ pointer actually changes to verify that re-allocation occurs.
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
