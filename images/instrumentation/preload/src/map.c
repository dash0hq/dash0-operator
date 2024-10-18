// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

#include <string.h>

#include "map.h"

Entry* find(Entry map[], int len, const char* key) {
  for (int i = 0; i < len; i++) {
    Entry* e = &map[i];
    if (strcmp(e->key, key) == 0) {
      return e;
    }
  }
  return NULL;
}

char* get_map_entry(Entry map[], int len, const char* key) {
  Entry* e = find(map, len, key);
  if (e == NULL) {
    return NULL;
  }
  return e->value;
}

void put_map_entry(Entry* map, int len, const char* key, char* value) {
  Entry* e = find(map, len, key);
  if (e == NULL) {
    return;
  }
  e->value = value;
}

