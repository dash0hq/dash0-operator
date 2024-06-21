// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>

typedef char* (*getenv_fun_ptr)(const char *name);
typedef char* (*secure_getenv_fun_ptr)(const char *name);

char* getenv(const char* name)
{
 	getenv_fun_ptr original_function = (getenv_fun_ptr)dlsym(RTLD_NEXT, "getenv");
  if (strcmp(name, "NODE_OPTIONS") != 0) {
 	  return original_function(name);
  }

  char* dash0_stuff = "--require /dash0";

 	char* original_value = original_function(name);
  if (original_value == NULL) {
    return dash0_stuff;
  }

  char* modified_value = malloc(strlen(dash0_stuff) + 1 + strlen(original_value) + 1);
  sprintf(modified_value, "%s %s", dash0_stuff, original_value);
  strcpy(modified_value, dash0_stuff);
  strcat(modified_value, " ");
  strcat(modified_value, original_value);

  // Note: it is probably okay to not free the modified_value, as long as we only malloc a very limited number of char*
  // instances, i.e. only one for every environment variable name we want to override, *not* one per getenv call!
  return modified_value;
}

