# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARCH := $(shell uname -m)
CFLAGS ?= -Wall -Werror -Wextra -Wl,-z,undefs,--export-dynamic-symbol=__my_environ -O2

TEST_CASES_DIR := test-cases
SRC_EXT := c

OS := $(shell uname -s)
SHELL := sh

TEST_SOURCES := $(wildcard $(TEST_CASES_DIR)/**/*.$(SRC_EXT))
# $(info TEST_SOURCES $(TEST_SOURCES))
TEST_OBJECTS := $(patsubst %.c,%.o,$(TEST_SOURCES))
# $(info TEST_OBJECTS $(TEST_OBJECTS))
TEST_CASE_NAMES := $(sort $(patsubst test-cases/%/,%,$(dir $(TEST_SOURCES))))
# $(info TEST_CASE_NAMES $(TEST_CASE_NAMES))

.PHONY: all
all: clean build

.PHONY: clean
clean:
	@rm -f $(TEST_OBJECTS)

.PHONY: build
clean:
build: $(TEST_CASE_NAMES)

$(TEST_CASE_NAMES):
	$(CC) $(CFLAGS) $(TEST_CASES_DIR)/$@/app.c -o $(TEST_CASES_DIR)/$@/app.o $(DEBUG) $^
	@chmod 755 $(TEST_CASES_DIR)/$@/app.o

