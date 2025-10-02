// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>
#include <unistd.h>
#include <dlfcn.h>

extern char **__environ;

void expect_getenv_value(char *name, char *expected) {
  char *actual = getenv(name);
  if (actual == NULL) {
    fprintf(stderr,
            "Unexpected value for the environment variable %s --\nexpected: "
            "%s\n     was: null\n",
            name, expected);
    exit(1);
  }
  if (strcmp(expected, actual) != 0) {
    fprintf(stderr,
            "Unexpected value for the environment variable %s --\nexpected: "
            "%s\n     was: %s\n",
            name, expected, actual);
    exit(1);
  }
}

void expect_environ_content(const char *expected_key_value_pairs[], int count) {
  for (int i = 0; i < count; i++) {
    const char *expected = expected_key_value_pairs[i];
    bool found = false;
    char **env_ptr = __environ;
    while (*env_ptr) {
      char *kv_pair_from_environ = *env_ptr;
      if (strcmp(expected_key_value_pairs[i], kv_pair_from_environ) == 0) {
        found = true;
        break;
      }
      env_ptr++;
    }
    if (!found) {
      fprintf(stderr,
              "Expected __environ to contain %s, but it didn't --\n__environ: ",
              expected);
      char **env_ptr = __environ;
      while (*env_ptr) {
        fprintf(stderr, "%s|", *env_ptr);
        env_ptr++;
      }
      fprintf(stderr, "\n");
      exit(1);
    }
  }
}

void expect_empty_environ() {
  int i = 0;
  char **env_ptr = __environ;
  if (env_ptr == NULL) {
    // Looks like musl and glibc implement clearenv differently -- in glibc, __environ is a valid pointer, but has no
    // entries (i.e. the first element is NULL). In musl, __environ is set to NULL (and probably re-initialized on the
    // next call to putenv/setenv).
    return;
  }
  while (*env_ptr) {
	i++;
    env_ptr++;
  }
  if (i != 0) {
    fprintf(stderr, "Expected empty __environ, but it was not empty");
    char **env_ptr = __environ;
    while (*env_ptr) {
      fprintf(stderr, "%s|", *env_ptr);
      env_ptr++;
    }
    fprintf(stderr, "\n");
    exit(1);
  }
}

void expect_getenv_null(const char *name) {
  char *actual = getenv(name);
  if (actual != NULL) {
    fprintf(stderr,
            "Unexpected value for the environment variable %s -- expected: "
            "null, was: \"%s\"\n",
            name, actual);
    exit(1);
  }
}

void expect_putenv_no_err(char *arg) {
  int err = putenv(arg);
  if (err != 0) {
    fprintf(stderr, "putenv(%s) failed, return code: %d\n", arg, err);
    exit(1);
  }
}

void expect_setenv_no_err(const char *name, const char *value, int replace) {
  int err = setenv(name, value, replace);
  if (err != 0) {
    fprintf(stderr, "setenv(%s, %s, %d) failed, return code: %d\n", name, value,
            replace, err);
    exit(1);
  }
}

void expect_unsetenv_no_err(const char *arg) {
  int err = unsetenv(arg);
  if (err != 0) {
    fprintf(stderr, "unsetenv(%s) failed, return code: %d\n", arg, err);
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

  pid_t pid = getpid();
  printf("app.c pid: %d\n", pid);

  // See
  // https://sourceware.org/glibc/manual/latest/html_mono/libc.html#Environment-Access
  // for docs on env-related functions.

  int err = 0;

  expect_environ_content(
      (const char *[]){
          "EXISTING_VAR_1=original-value-1",
          "EXISTING_VAR_2=original-value-2",
          "EXISTING_VAR_3=original-value-3",
          "EXISTING_VAR_4=original-value-4",
          "EXISTING_VAR_5=original-value-5",
          "EXISTING_VAR_6=original-value-6",
          "EMPTY_ENV_VAR=",
      },
      7);

  // This variable is not set, libc is supposed to return NULL.
  expect_getenv_null("NON_EXISTING_VAR_1");
  // This variable is explicitly set to the empty string, getenv is supposed to return "".
  expect_getenv_value("EMPTY_ENV_VAR", "");

  expect_getenv_value("EXISTING_VAR_1", "original-value-1");
  expect_getenv_value("EXISTING_VAR_2", "original-value-2");
  expect_getenv_value("EXISTING_VAR_3", "original-value-3");
  expect_getenv_value("EXISTING_VAR_4", "original-value-4");
  expect_getenv_value("EXISTING_VAR_5", "original-value-5");
  expect_getenv_value("EXISTING_VAR_6", "original-value-6");

  expect_putenv_no_err(
      "EXISTING_VAR_1=new value from putenv for variable that existed before");
  expect_getenv_value("EXISTING_VAR_1",
                      "new value from putenv for variable that existed before");
  expect_environ_content(
      (const char *[]){
          "EXISTING_VAR_1=new value from putenv for variable that existed before",
          "EXISTING_VAR_2=original-value-2",
          "EXISTING_VAR_3=original-value-3",
          "EXISTING_VAR_4=original-value-4",
          "EXISTING_VAR_5=original-value-5",
          "EXISTING_VAR_6=original-value-6",
          "EMPTY_ENV_VAR=",
      },
      7);

  expect_putenv_no_err("NON_EXISTING_VAR_1=new value from putenv for variable that did not exist before");
  expect_getenv_value(
      "NON_EXISTING_VAR_1",
      "new value from putenv for variable that did not exist before");
  expect_environ_content(
      (const char *[]){
          "EXISTING_VAR_1=new value from putenv for variable that existed before",
          "EXISTING_VAR_2=original-value-2",
          "EXISTING_VAR_3=original-value-3",
          "EXISTING_VAR_4=original-value-4",
          "EXISTING_VAR_5=original-value-5",
          "EXISTING_VAR_6=original-value-6",
          "EMPTY_ENV_VAR=",
          "NON_EXISTING_VAR_1=new value from putenv for variable that did not exist before",
      },
      8);

  // Calling putenv with a string without "=" should delete the env var.
  expect_putenv_no_err("EXISTING_VAR_2");
  expect_getenv_null("EXISTING_VAR_2");
  expect_environ_content(
      (const char *[]){
          "EXISTING_VAR_1=new value from putenv for variable that existed before",
          "EXISTING_VAR_3=original-value-3",
          "EXISTING_VAR_4=original-value-4",
          "EXISTING_VAR_5=original-value-5",
          "EXISTING_VAR_6=original-value-6",
          "EMPTY_ENV_VAR=",
          "NON_EXISTING_VAR_1=new value from putenv for variable that did not exist before",
      },
      7);

  setenv("NON_EXISTING_VAR_2",
         "value from setenv for non-existing without replace", 0);
  expect_getenv_value("NON_EXISTING_VAR_2",
                      "value from setenv for non-existing without replace");
  setenv("NON_EXISTING_VAR_3",
         "value from setenv for non-existing with replace", 1);
  expect_getenv_value("NON_EXISTING_VAR_3",
                      "value from setenv for non-existing with replace");
  setenv("EXISTING_VAR_3", "value from setenv for existing without replace", 0);
  expect_getenv_value("EXISTING_VAR_3", "original-value-3");
  setenv("EXISTING_VAR_4", "value from setenv for existing with replace", 1);
  expect_getenv_value("EXISTING_VAR_4",
                      "value from setenv for existing with replace");
  // replace with empty string
  setenv("EXISTING_VAR_4", "", 1);
  expect_getenv_value("EXISTING_VAR_4", "");

  expect_environ_content(
      (const char *[]){
          "EXISTING_VAR_1=new value from putenv for variable that existed before",
          "EXISTING_VAR_3=original-value-3",
          "EXISTING_VAR_4=",
          "EXISTING_VAR_5=original-value-5",
          "EXISTING_VAR_6=original-value-6",
          "EMPTY_ENV_VAR=",
          "NON_EXISTING_VAR_1=new value from putenv for variable that did not exist before",
          "NON_EXISTING_VAR_2=value from setenv for non-existing without replace",
          "NON_EXISTING_VAR_3=value from setenv for non-existing with replace",
      },
      9);

  expect_unsetenv_no_err("NON_EXISTING_VAR_4");
  expect_getenv_null("NON_EXISTING_VAR_4");
  expect_unsetenv_no_err("EXISTING_VAR_5");
  expect_getenv_null("EXISTING_VAR_5");

  expect_environ_content(
      (const char *[]){
          "EXISTING_VAR_1=new value from putenv for variable that existed before",
          "EXISTING_VAR_3=original-value-3",
          "EXISTING_VAR_4=",
          "EXISTING_VAR_6=original-value-6",
          "EMPTY_ENV_VAR=",
          "NON_EXISTING_VAR_1=new value from putenv for variable that did not exist before",
          "NON_EXISTING_VAR_2=value from setenv for non-existing without replace",
          "NON_EXISTING_VAR_3=value from setenv for non-existing with replace",
      },
      8);

  err = unsetenv("string=with_equals_character");
  if (err == 0) {
    fprintf(stderr, "unsetenv(string=with_equals_character) succeeded, should have failed.\n");
    exit(1);
  }

  err = clearenv();
  if (err != 0) {
    fprintf(stderr, "clearenv() failed, return code: %d\n", err);
    exit(1);
  }
  expect_empty_environ();
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
}
