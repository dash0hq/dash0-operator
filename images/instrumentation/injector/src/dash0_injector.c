#include <stdint.h>
#include <unistd.h>

#define ALIGN (sizeof(size_t))
#define UCHAR_MAX 255
#define ONES ((size_t)-1 / UCHAR_MAX)
#define HIGHS (ONES * (UCHAR_MAX / 2 + 1))
#define HASZERO(x) ((x) - ONES & ~(x) & HIGHS)

#define NODE_OPTIONS_ENV_VAR_NAME "NODE_OPTIONS"
#define NODE_OPTIONS_DASH0_REQUIRE                                             \
  "--require "                                                                 \
  "/__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
#define OTEL_RESOURCE_ATTRIBUTES_ENV_VAR_NAME "OTEL_RESOURCE_ATTRIBUTES"
#define DASH0_POD_UID_ENV_VAR_NAME "DASH0_POD_UID"
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
char val1[1012];
char val2[1012];

char *getenv(const char *name) {
  char *origValue = __getenv(name);
  char *podUid = __getenv(DASH0_POD_UID_ENV_VAR_NAME);
  char *containerName = __getenv(DASH0_POD_CONTAINER_NAME_VAR_NAME);
  int l = __strlen(name);

  char *otelResourceAttributesVarName = OTEL_RESOURCE_ATTRIBUTES_ENV_VAR_NAME;
  char *nodeOptionsVarName = NODE_OPTIONS_ENV_VAR_NAME;
  if (__strcmp(name, otelResourceAttributesVarName) == 0) {
    // If we have not calculated this env var yet
    if (__strlen(val1) == 0) {
      int catCount = 0;

      if (podUid != NULL && __strlen(podUid) > 0) {
        __strcat(val1, "k8s.pod.uid=");
        __strcat(val1, podUid);
        catCount += 1;
      }

      if (containerName != NULL && __strlen(containerName) > 0) {
        if (catCount > 0) {
          __strcat(val1, ",");
        }

        __strcat(val1, "k8s.container.name=");
        __strcat(val1, containerName);
        catCount += 1;
      }

      if (origValue != NULL && __strlen(origValue) > 0) {
        if (catCount > 0) {
          __strcat(val1, ",");
        }

        __strcat(val1, origValue);
      }
    }

    return val1;
  } else if (__strcmp(name, nodeOptionsVarName) == 0) {
    // If we have not calculated this env var yet
    if (__strlen(val2) == 0) {
      // Prepend our --require as the first item to the NODE_OPTIONS string.
      char *nodeOptionsDash0Require = NODE_OPTIONS_DASH0_REQUIRE;
      __strcat(val2, nodeOptionsDash0Require);

      if (origValue != NULL && __strlen(origValue) > 0) {
        // If NODE_OPTIONS were present, append the existing NODE_OPTIONS after
        // our --require.
        __strcat(val2, " ");
        __strcat(val2, origValue);
      }
    }

    return val2;
  }

  return origValue;
}
