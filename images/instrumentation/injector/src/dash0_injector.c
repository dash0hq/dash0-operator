#include <stdint.h>
#include <unistd.h>

#define ALIGN (sizeof(size_t))
#define UCHAR_MAX 255
#define ONES ((size_t)-1 / UCHAR_MAX)
#define HIGHS (ONES * (UCHAR_MAX / 2 + 1))
#define HASZERO(x) ((x) - ONES & ~(x) & HIGHS)

#define JAVA_TOOL_OPTIONS_ENV_VAR_NAME "JAVA_TOOL_OPTIONS"
#define JAVA_TOOL_OPTIONS_DASH0_REQUIRE                                        \
  "-javaagent:"                                                                \
  "/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar"
#define NODE_OPTIONS_ENV_VAR_NAME "NODE_OPTIONS"
#define NODE_OPTIONS_DASH0_REQUIRE                                             \
  "--require "                                                                 \
  "/__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
#define OTEL_RESOURCE_ATTRIBUTES_ENV_VAR_NAME "OTEL_RESOURCE_ATTRIBUTES"
#define DASH0_NAMESPACE_NAME_ENV_VAR_NAME "DASH0_NAMESPACE_NAME"
#define DASH0_POD_UID_ENV_VAR_NAME "DASH0_POD_UID"
#define DASH0_POD_NAME_ENV_VAR_NAME "DASH0_POD_NAME"
#define DASH0_POD_CONTAINER_NAME_VAR_NAME "DASH0_CONTAINER_NAME"

extern char **__environ;

size_t __strlen(const char *s) {
  const char *a = s;
  const size_t *w;
  for (; (uintptr_t)s % ALIGN; s++)
    if (!*s)
      return s - a;
  for (w = (const void *)s; !HASZERO(*w); w++)
    ;
  for (s = (const void *)w; *s; s++)
    ;
  return s - a;
}

char *__strchrnul(const char *s, int c) {
  size_t *w, k;

  c = (unsigned char)c;
  if (!c)
    return (char *)s + __strlen(s);

  for (; (uintptr_t)s % ALIGN; s++)
    if (!*s || *(unsigned char *)s == c)
      return (char *)s;
  k = ONES * c;
  for (w = (void *)s; !HASZERO(*w) && !HASZERO(*w ^ k); w++)
    ;
  for (s = (void *)w; *s && *(unsigned char *)s != c; s++)
    ;
  return (char *)s;
}

char *__strcpy(char *restrict dest, const char *restrict src) {
  const unsigned char *s = src;
  unsigned char *d = dest;
  while ((*d++ = *s++))
    ;
  return dest;
}

char *__strcat(char *restrict dest, const char *restrict src) {
  __strcpy(dest + __strlen(dest), src);
  return dest;
}

int __strcmp(const char *l, const char *r) {
  for (; *l == *r && *l; l++, r++)
    ;
  return *(unsigned char *)l - *(unsigned char *)r;
}

int __strncmp(const char *_l, const char *_r, size_t n) {
  const unsigned char *l = (void *)_l, *r = (void *)_r;
  if (!n--)
    return 0;
  for (; *l && *r && n && *l == *r; l++, r++, n--)
    ;
  return *l - *r;
}

char *__getenv(const char *name) {
  size_t l = __strchrnul(name, '=') - name;
  if (l && !name[l] && __environ)
    for (char **e = __environ; *e; e++)
      if (!__strncmp(name, *e, l) && l[*e] == '=')
        return *e + l + 1;
  return 0;
}

/*
 * Buffers of statically-allocated memory that we can use to safely return to
 * the program manipulated values of env vars without dynamic allocations.
 */
char cachedModifiedOtelResourceAttributesValue[1012];
char cachedModifiedRuntimeOptionsValue[1012];

char *__appendResourceAttributes(const char *buffer, const char *origValue) {
  char *namespaceName = __getenv(DASH0_NAMESPACE_NAME_ENV_VAR_NAME);
  char *podUid = __getenv(DASH0_POD_UID_ENV_VAR_NAME);
  char *podName = __getenv(DASH0_POD_NAME_ENV_VAR_NAME);
  char *containerName = __getenv(DASH0_POD_CONTAINER_NAME_VAR_NAME);

  int attributeCount = 0;

  /*
   * We do not perform octect escaping in the resource attributes as
   * specified in
   * https://opentelemetry.io/docs/specs/otel/resource/sdk/#specifying-resource-information-via-an-environment-variable
   * because the values that are passed down to the injector comes from
   * fields that Kubernetes already enforces to either conform to RFC 1035
   * or RFC RFC 1123
   * (https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-label-names),
   * and in either case, none of the characters allowed require escaping
   * based on https://www.w3.org/TR/baggage/#header-content
   */

  if (namespaceName != NULL && __strlen(namespaceName) > 0) {
    __strcat(buffer, "k8s.namespace.name=");
    __strcat(buffer, namespaceName);
    attributeCount += 1;
  }

  if (podName != NULL && __strlen(podName) > 0) {
    if (attributeCount > 0) {
      __strcat(buffer, ",");
    }

    __strcat(buffer, "k8s.pod.name=");
    __strcat(buffer, podName);
    attributeCount += 1;
  }

  if (podUid != NULL && __strlen(podUid) > 0) {
    if (attributeCount > 0) {
      __strcat(buffer, ",");
    }

    __strcat(buffer, "k8s.pod.uid=");
    __strcat(buffer, podUid);
    attributeCount += 1;
  }

  if (containerName != NULL && __strlen(containerName) > 0) {
    if (attributeCount > 0) {
      __strcat(buffer, ",");
    }

    __strcat(buffer, "k8s.container.name=");
    __strcat(buffer, containerName);
    attributeCount += 1;
  }

  if (origValue != NULL && __strlen(origValue) > 0) {
    if (attributeCount > 0) {
      __strcat(buffer, ",");
    }

    __strcat(buffer, origValue);
  }
}

char *getenv(const char *name) {
  char *origValue = __getenv(name);
  int l = __strlen(name);

  char *otelResourceAttributesVarName = OTEL_RESOURCE_ATTRIBUTES_ENV_VAR_NAME;
  char *javaToolOptionsVarName = JAVA_TOOL_OPTIONS_ENV_VAR_NAME;
  char *nodeOptionsVarName = NODE_OPTIONS_ENV_VAR_NAME;
  if (__strcmp(name, otelResourceAttributesVarName) == 0) {
    if (__strlen(cachedModifiedOtelResourceAttributesValue) == 0) {
      // This environment variable (OTEL_RESOURCE_ATTRIBUTES) has not been
      // requested before, calculate the modified value and cache it.
      __appendResourceAttributes(cachedModifiedOtelResourceAttributesValue,
                                 origValue);
    }

    return cachedModifiedOtelResourceAttributesValue;
  } else if (__strcmp(name, javaToolOptionsVarName) == 0) {
    if (__strlen(cachedModifiedRuntimeOptionsValue) == 0) {
      // No runtime environment variable has been requested before,
      // calculate the modified value and cache it.

      // Prepend our --require as the first item to the JAVA_TOOL_OPTIONS
      // string.
      char *javaToolOptionsDash0Require = JAVA_TOOL_OPTIONS_DASH0_REQUIRE;
      __strcat(cachedModifiedRuntimeOptionsValue, javaToolOptionsDash0Require);

      // The Java runtime does not look up the OTEL_RESOURCE_ATTRIBUTES env var
      // using getenv(), but rather by parsing the environment block
      // (/proc/env/<pid>) directly, which we cannot affect with the getenv
      // hook. So, instead, we append the resource attributes as the
      // -Dotel.resource.attributes Java system property.
      // TODO Handle the case when there ALREADY is the
      // -Dotel.resource.attributes system property set.
      char *otelResourceAttributesViaEnv =
          __getenv(OTEL_RESOURCE_ATTRIBUTES_ENV_VAR_NAME);
      __strcat(cachedModifiedRuntimeOptionsValue,
               " -Dotel.resource.attributes=");
      __appendResourceAttributes(cachedModifiedRuntimeOptionsValue,
                                 otelResourceAttributesViaEnv);

      if (origValue != NULL && __strlen(origValue) > 0) {
        // If JAVA_TOOL_OPTIONS were present, append the existing
        // JAVA_TOOL_OPTIONS after our --javaagent.
        __strcat(cachedModifiedRuntimeOptionsValue, " ");
        __strcat(cachedModifiedRuntimeOptionsValue, origValue);
      }
    }

    return cachedModifiedRuntimeOptionsValue;
  } else if (__strcmp(name, nodeOptionsVarName) == 0) {
    if (__strlen(cachedModifiedRuntimeOptionsValue) == 0) {
      // No runtime environment variable has been requested before,
      // calculate the modified value and cache it.

      // Prepend our --require as the first item to the NODE_OPTIONS string.
      char *nodeOptionsDash0Require = NODE_OPTIONS_DASH0_REQUIRE;
      __strcat(cachedModifiedRuntimeOptionsValue, nodeOptionsDash0Require);

      if (origValue != NULL && __strlen(origValue) > 0) {
        // If NODE_OPTIONS were present, append the existing NODE_OPTIONS after
        // our --require.
        __strcat(cachedModifiedRuntimeOptionsValue, " ");
        __strcat(cachedModifiedRuntimeOptionsValue, origValue);
      }
    }

    return cachedModifiedRuntimeOptionsValue;
  }

  return origValue;
}
