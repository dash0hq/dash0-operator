# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARCH := $(shell uname -m)
SRC_EXT := c
OS := $(shell uname -s)
SHELL := sh

# -Wl passes options to the linker, i.e. everything after -"Wl," is passed on.
# The ld option -z,undefs allows undefined
CFLAGS ?= \
  -Wall \
  -Werror \
  -Wextra \
  -Wl,-z,undefs,--export-dynamic-symbol=my_environ,--export-dynamic-symbol=change_values

.PHONY: all
all: clean build

.PHONY: clean
clean:
	@rm -f app.o

.PHONY: build
clean:
build: app.o

app.o:
	$(CC) $(CFLAGS) app.c -o app.o $(DEBUG) $^
	@chmod 755 app.o

