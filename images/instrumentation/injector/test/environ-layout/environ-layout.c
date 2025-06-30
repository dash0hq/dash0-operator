// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>
#include <unistd.h>
#include <inttypes.h>

extern char** __environ;
extern char** _environ;
extern char** environ;

// Copy and paste snapshots here, see ../scripts/environ-layout.tests for instructions on recreating the snapshots:

const char *SNAPSHOT_NO_MODIFICATIONS___ENVIRON = "LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so|ENV_VAR1=value1|ENV_VAR2=value2|__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true|(nil)";
const char *SNAPSHOT_NO_MODIFICATIONS__ENVIRON = "LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so|ENV_VAR1=value1|ENV_VAR2=value2|__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true|(nil)";
const char *SNAPSHOT_NO_MODIFICATIONS_ENVIRON = "LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so|ENV_VAR1=value1|ENV_VAR2=value2|__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true|(nil)";
const char *SNAPSHOT_NO_MODIFICATIONS_ENVP = "LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so|ENV_VAR1=value1|ENV_VAR2=value2|__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true|(nil)";

const char *SNAPSHOT_WITH_MODIFICATIONS___ENVIRON = "LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so|DASH0_CONTAINER_NAME=test-app|DASH0_NAMESPACE_NAME=my-namespace|DASH0_POD_NAME=my-pod|DASH0_POD_UID=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff|ENV_VAR1=value1|ENV_VAR2=value2|__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true|JAVA_TOOL_OPTIONS=-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app|NODE_OPTIONS=--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry|OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app|(nil)";
const char *SNAPSHOT_WITH_MODIFICATIONS__ENVIRON = "LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so|DASH0_CONTAINER_NAME=test-app|DASH0_NAMESPACE_NAME=my-namespace|DASH0_POD_NAME=my-pod|DASH0_POD_UID=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff|ENV_VAR1=value1|ENV_VAR2=value2|__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true|JAVA_TOOL_OPTIONS=-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app|NODE_OPTIONS=--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry|OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app|(nil)";
const char *SNAPSHOT_WITH_MODIFICATIONS_ENVIRON = "LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so|DASH0_CONTAINER_NAME=test-app|DASH0_NAMESPACE_NAME=my-namespace|DASH0_POD_NAME=my-pod|DASH0_POD_UID=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff|ENV_VAR1=value1|ENV_VAR2=value2|__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true|JAVA_TOOL_OPTIONS=-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app|NODE_OPTIONS=--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry|OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app|(nil)";
const char *SNAPSHOT_WITH_MODIFICATIONS_ENVP = "LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so|DASH0_CONTAINER_NAME=test-app|DASH0_NAMESPACE_NAME=my-namespace|DASH0_POD_NAME=my-pod|DASH0_POD_UID=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff|ENV_VAR1=value1|ENV_VAR2=value2|__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true|JAVA_TOOL_OPTIONS=-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app|NODE_OPTIONS=--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry|OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app|(nil)";

static void print_pointer_list_for_snapshot(char* var_name, char** list) {
  printf("const char *%s = \"LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so|", var_name);
  char** ptr = list;
  while (*ptr) {
      printf("%s|", *ptr);
      ptr++;
  }
  printf("%p\";\n", *ptr);
}

static void create_snapshots(char** envp, char* snapshot_name) {
  printf("\n================ start of snapshot:\n");
  char var_name___environ[50];
  sprintf(var_name___environ, "SNAPSHOT_%s___ENVIRON", snapshot_name),
  print_pointer_list_for_snapshot(var_name___environ, __environ);
  char var_name__environ[50];
  sprintf(var_name__environ, "SNAPSHOT_%s__ENVIRON", snapshot_name),
  print_pointer_list_for_snapshot(var_name__environ, _environ);
  char var_name_environ[50];
  sprintf(var_name_environ, "SNAPSHOT_%s_ENVIRON", snapshot_name),
  print_pointer_list_for_snapshot(var_name_environ, environ);
  char var_name_envp[50];
  sprintf(var_name_envp, "SNAPSHOT_%s_ENVP", snapshot_name),
  print_pointer_list_for_snapshot(var_name_envp, envp);
  printf("================ end of snapshot\n");
}

static char* list_to_string(char** list) {
  size_t total_length = 0;
  int count = 0;
  for (int i = 0; list[i] != NULL; i++) {
      total_length += strlen(list[i]);
      count++;
  }
  total_length += (count - 1); // separator between elements
  total_length += 5; // final (nil)
  total_length += 1; // actual null terminator

  char *result = malloc(total_length);
  if (!result) {
    perror("malloc failed");
    exit(1);
  }

  // initialize with empty string
  result[0] = '\0';
  for (int i = 0; list[i] != NULL; i++) {
    strcat(result, list[i]);
    strcat(result, "|");
  }
  strcat(result, "(nil)");
  return result;
}

static void compare_pointer_list_to_snapshot(char* var_name, const char* snapshot, char** actual) {
   int number_of_items_in_snapshot = 0;
   for (int i = 0; snapshot[i] != '\0'; i++) {
     if (snapshot[i] == '|') {
       number_of_items_in_snapshot++;
     }
   }

   char** ptr_actual = actual;
   int number_of_items_actual = 0;
   for (; ptr_actual[number_of_items_actual] != NULL; number_of_items_actual++);

   char* actual_as_string = list_to_string(actual);

  if (number_of_items_actual != number_of_items_in_snapshot) {
     printf("\n%s: number of actual list items differs from snapshot, expected %d but got %d\n", var_name, number_of_items_in_snapshot, number_of_items_actual);
     printf("%s: snapshot: %s\n", var_name, snapshot);
     printf("%s: actual:   %s\n", var_name, actual_as_string);
     exit(1);
  }
  if (strcmp(actual_as_string, snapshot) != 0) {
     printf("\n%s: actual list items differ from snapshot\n", var_name);
     printf("%s: snapshot: %s\n", var_name, snapshot);
     printf("%s: actual:   %s\n", var_name, actual_as_string);
     exit(1);
  }

  free(actual_as_string);
}

static void compare_to_snapshot_no_modifications(char** envp) {
  compare_pointer_list_to_snapshot("__environ", SNAPSHOT_NO_MODIFICATIONS___ENVIRON, __environ);
  compare_pointer_list_to_snapshot("_environ", SNAPSHOT_NO_MODIFICATIONS__ENVIRON, _environ);
  compare_pointer_list_to_snapshot("environ", SNAPSHOT_NO_MODIFICATIONS_ENVIRON, environ);
  compare_pointer_list_to_snapshot("envp", SNAPSHOT_NO_MODIFICATIONS_ENVP, envp);
}

static void compare_to_snapshot_with_modifications(char** envp) {
  compare_pointer_list_to_snapshot("__environ", SNAPSHOT_WITH_MODIFICATIONS___ENVIRON, __environ);
  compare_pointer_list_to_snapshot("_environ", SNAPSHOT_WITH_MODIFICATIONS__ENVIRON, _environ);
  compare_pointer_list_to_snapshot("environ", SNAPSHOT_WITH_MODIFICATIONS_ENVIRON, environ);
  compare_pointer_list_to_snapshot("envp", SNAPSHOT_WITH_MODIFICATIONS_ENVP, envp);
}


int main(int argc, char** argv, char** envp) {
  if (argc < 2) {
    printf("You provided too few arguments, a command is required.\n");
    return 1;
  }
  if (argc > 3) {
    printf("You provided too many arguments.\n");
    return 1;
  }

  if (strcmp(argv[1], "compare-to-snapshot") == 0) {
    if (argc < 3) {
      printf("The command compare-to-snapshot requires a snapshot name.\n");
      return 1;
    }
    if (strcmp(argv[2], "NO_MODIFICATIONS") == 0) {
      compare_to_snapshot_no_modifications(envp);
    } else if (strcmp(argv[2], "WITH_MODIFICATIONS") == 0) {
      compare_to_snapshot_with_modifications(envp);
    } else {
      printf("Unknown snapshot name: %s\n", argv[2]);
      return 1;
    }
    printf("success: everything matches the snapshot");
    return 0;
  } else if (strcmp(argv[1], "create-snapshot") == 0) {
    if (argc < 3) {
      printf("The command create-snapshot requires a snapshot name.\n");
      return 1;
    }
    if (argc > 3) {
      printf("You provided too many arguments.\n");
      return 1;
    }
    create_snapshots(envp, argv[2]);
    return 0;
  } else {
    printf("command not supported: %s\n", argv[1]);
    return 1;
  }
}

