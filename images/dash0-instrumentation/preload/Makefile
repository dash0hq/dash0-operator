# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARCH := $(shell uname -m)
CFLAGS ?= -Wall -Werror -Wextra -O2
LIB_LINKER_FLAGS ?= -nostdlib -rdynamic -shared -ldl

SRC_DIR := src
OBJ_DIR := obj
LIB_DIR := lib
TEST_DIR := test
TESTBIN_DIR := testbin/$(ARCH)
SRC_EXT := c

OS := $(shell uname -s)
SHELL := sh

NAMES := $(notdir $(basename $(wildcard $(SRC_DIR)/*.$(SRC_EXT))))
OBJECTS :=$(patsubst %,$(OBJ_DIR)/%.o,$(NAMES))

TEST_NAMES := $(notdir $(basename $(wildcard $(TEST_DIR)/*.$(SRC_EXT))))
TEST_OBJECTS :=$(patsubst %,$(TESTBIN_DIR)/%.o,$(TEST_NAMES))

all: $(LIB_DIR)/libdash0envhook.so

$(LIB_DIR)/libdash0envhook.so: $(OBJECTS)
	$(CC) $(CFLAGS) $(LIB_LINKER_FLAGS) $(OBJECTS) -o $(LIB_DIR)/libdash0envhook.so

$(OBJ_DIR)/%.o: $(SRC_DIR)/%.$(SRC_EXT)
	$(CC) -c -fPIC $^ -o $@ $(DEBUG) $(CFLAGS) $(LIBS)

clean: clean-test
	@rm -f $(OBJECTS) $(LIB_DIR)/libdash0envhook.so

clean-test:
	@rm -f $(TEST_OBJECTS)

build-test: $(TEST_OBJECTS)

$(TESTBIN_DIR)/%.o: $(TEST_DIR)/%.$(SRC_EXT)
	$(CC) $(CFLAGS) -o $@ $(DEBUG) $^

.PHONY: all clean install clean-test build-test
