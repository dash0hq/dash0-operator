// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>
#include <unistd.h>

extern char** __environ;
extern char** _environ;
extern char** environ;

// TODO what is this test about? It is currently a mess. Delete it or turn it into a useful test.
// TODO turn this into a test case that verifies the exact byte-for-byte content of __environ, _environ, and environ,
// as compared to running this without LD_PRELOAD.
int main() {
  pid_t pid = getpid();
  printf("app.c pid: %d\n", pid);

  for (char **e = __environ; *e; e++) {
     printf("RAW __environ value: '%s'\n", *e);
  }
  // Aliases: https://sourceware.org/git/?p=glibc.git;a=blob;f=posix/environ.c
  for (char **e = _environ; *e; e++) {
     printf("RAW _environ value: '%s'\n", *e);
  }
  for (char **e = environ; *e; e++) {
    printf("RAW environ value: '%s'\n", *e);
  }
  printf("app.c#main() END\n");
}
