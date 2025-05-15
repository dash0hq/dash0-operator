// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

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
  // See
  // https://www.gnu.org/software/libc/manual/html_node/Environment-Access.html
  // for docs on env-related functions.

  int err = 0;

  expect_getenv_value("EXISTING_VAR_1", "original-value-1");
  expect_getenv_value("EXISTING_VAR_2", "original-value-2");
  expect_getenv_value("EXISTING_VAR_3", "original-value-3");
  expect_getenv_value("EXISTING_VAR_4", "original-value-4");
  expect_getenv_value("EXISTING_VAR_5", "original-value-5");
  expect_getenv_value("EXISTING_VAR_6", "original-value-6");

  expect_putenv_no_err("EXISTING_VAR_1=new value from putenv for variable that existed before");
  expect_getenv_value("EXISTING_VAR_1", "new value from putenv for variable that existed before");

  expect_putenv_no_err("NON_EXISTING_VAR_1=new value from putenv for variable that did not exist before");
  expect_getenv_value("NON_EXISTING_VAR_1", "new value from putenv for variable that did not exist before");

  // Calling putenv with a string without "=" should delete the env var.
  expect_putenv_no_err("EXISTING_VAR_2");
  expect_getenv_null("EXISTING_VAR_2");

  // The setenv function can be used to add a new definition to the environment. The entry with the name name is replaced by the value ‘name=value’. Please note that this is also true if value is the empty string. To do this a new string is created and the strings name and value are copied. A null pointer for the value parameter is illegal. If the environment already contains an entry with key name the replace parameter controls the action. If replace is zero, nothing happens. Otherwise the old entry is replaced by the new one.
  setenv("NON_EXISTING_VAR_2", "value from setenv for non-existing without replace", 0);
  expect_getenv_value("NON_EXISTING_VAR_2", "value from setenv for non-existing without replace");
  setenv("NON_EXISTING_VAR_3", "value from setenv for non-existing with replace", 1);
  expect_getenv_value("NON_EXISTING_VAR_3", "value from setenv for non-existing with replace");
  setenv("EXISTING_VAR_3", "value from setenv for existing without replace", 0);
  expect_getenv_value("EXISTING_VAR_3", "original-value-3");
  setenv("EXISTING_VAR_4", "value from setenv for existing with replace", 1);
  expect_getenv_value("EXISTING_VAR_4", "value from setenv for existing with replace");
  // replace with empty string
  setenv("EXISTING_VAR_4", "", 1);
  expect_getenv_value("EXISTING_VAR_4", "");

  expect_unsetenv_no_err("NON_EXISTING_VAR_4");
  expect_getenv_null("NON_EXISTING_VAR_4");
  expect_unsetenv_no_err("EXISTING_VAR_5");
  expect_getenv_null("EXISTING_VAR_5");

  err = unsetenv("string=with_equals_character");
  if (err == 0) {
    printf("unsetenv(string=with_equals_character) succeeded, should have failed.\n");
    exit(1);
  }

  err = clearenv();
  if (err != 0) {
    printf("clearenv() failed, return code: %d\n", err);
    exit(1);
  }

  expect_getenv_null("EXISTING_VAR_1");
  expect_getenv_null("EXISTING_VAR_2");
  expect_getenv_null("EXISTING_VAR_3");
  expect_getenv_null("EXISTING_VAR_4");
  expect_getenv_null("EXISTING_VAR_5");
  expect_getenv_null("EXISTING_VAR_6");
  expect_getenv_null("NON_EXISTING_VAR_1");
  expect_getenv_null("NON_EXISTING_VAR_2");
  expect_getenv_null("NON_EXISTING_VAR_3");
  expect_getenv_null("NON_EXISTING_VAR_4");

  // verify that putenv/getenv still work as expected after clearenv
  expect_putenv_no_err("NEW_VAR=value");
  expect_getenv_value("NEW_VAR", "value");
  expect_getenv_null("DOES_NOT_EXIST");

  // TODO Add additional tests that read environ, _environ, __environ after executing putenv/setenv when continuing with
  // https://linear.app/dash0/issue/ENG-4882/export-environ-instead-of-getenv-in-injector
}
