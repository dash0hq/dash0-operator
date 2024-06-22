// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

typedef struct Entry {
  const char* key;
  char* value;
} Entry;

char* get_map_entry(Entry map[], int len, const char* key);

void put_map_entry(Entry* map, int len, const char* key, char* value);
