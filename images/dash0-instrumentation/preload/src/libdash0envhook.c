// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <dlfcn.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>

#include "map.h"

typedef char* (*getenv_fun_ptr)(const char* name);

typedef char* (*secure_getenv_fun_ptr)(const char* name);

getenv_fun_ptr original_getenv;
secure_getenv_fun_ptr original_secure_getenv;

int num_map_entries = 1;
Entry map[1];

char* default_node_options_value = "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js";


__attribute__((constructor)) static void setup(void) {
  Entry node_options_entry = { .key = "NODE_OPTIONS", .value = NULL };
  map[0] = node_options_entry;
}

char* _getenv(char* (*original_function)(const char* name), const char* name)
{
  if (strcmp(name, "NODE_OPTIONS") != 0) {
 	  return original_function(name);
  }

  char* cached = get_map_entry(map, num_map_entries, name);
  if (cached != NULL) {
    return cached;
  }

 	char* original_value = original_function(name);
  if (original_value == NULL) {
    return default_node_options_value;
  }

  char* modified_value = malloc(strlen(default_node_options_value) + 1 + strlen(original_value) + 1);
  strcpy(modified_value, default_node_options_value);
  strcat(modified_value, " ");
  strcat(modified_value, original_value);

  // Note: it is probably okay to not free the modified_value, as long as we only malloc a very limited number of char*
  // instances, i.e. only one for every environment variable name we want to override.
  put_map_entry(map, num_map_entries, name, modified_value);
  return modified_value;
}

char* getenv(const char* name) {
  if (!original_getenv) {
    original_getenv = (getenv_fun_ptr)dlsym(RTLD_NEXT, "getenv");
  }
  return _getenv(original_getenv, name);
}

char* secure_getenv(const char* name) {
  if (!original_secure_getenv) {
    original_secure_getenv = (secure_getenv_fun_ptr)dlsym(RTLD_NEXT, "secure_getenv");
  }
  return _getenv(original_secure_getenv, name);
}

