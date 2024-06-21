# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

CFLAGS ?= -Wall -Werror -Wextra -O2

SRC_DIR := src
BIN_DIR := bin
OBJ_DIR := obj
LIB_DIR := lib
TEST_DIR := test
SRC_EXT := c

OS := $(shell uname -s)
SHELL := bash

NAMES := $(notdir $(basename $(wildcard $(SRC_DIR)/*.$(SRC_EXT))))
$(info NAMES: $(NAMES))
OBJECTS :=$(patsubst %,$(OBJ_DIR)/%.o,$(NAMES))
$(info OBJECTS: $(OBJECTS))

TEST_NAMES := $(notdir $(basename $(wildcard $(TEST_DIR)/*.$(SRC_EXT))))
$(info TEST_NAMES: $(TEST_NAMES))
TEST_OBJECTS :=$(patsubst %,$(OBJ_DIR)/%.o,$(TEST_NAMES))
$(info TEST_OBJECTS: $(TEST_OBJECTS))


all: $(OBJECTS) $(TEST_OBJECTS) testbin/appundertest.so
	$(CC) $(CFLAGS) -rdynamic -shared -ldl $(OBJECTS) -o $(LIB_DIR)/libdash0envhook.so

$(OBJ_DIR)/%.o: $(SRC_DIR)/%.$(SRC_EXT)
	$(CC) -c -fPIC $^ -o $@ $(DEBUG) $(CFLAGS) $(LIBS)

$(OBJ_DIR)/%.o: $(TEST_DIR)/%.$(SRC_EXT)
	$(CC) -c $^ -o $@ $(DEBUG) $(CFLAGS) $(LIBS)

testbin/appundertest.so: test/appundertest.c
	$(CC) $(CFLAGS) -o $@ test/appundertest.c

clean:
	@rm -f *.so

.PHONY: all clean install
