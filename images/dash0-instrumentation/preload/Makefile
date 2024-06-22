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
SHELL := sh

NAMES := $(notdir $(basename $(wildcard $(SRC_DIR)/*.$(SRC_EXT))))
OBJECTS :=$(patsubst %,$(OBJ_DIR)/%.o,$(NAMES))

TEST_NAMES := $(notdir $(basename $(wildcard $(TEST_DIR)/*.$(SRC_EXT))))
TEST_OBJECTS :=$(patsubst %,$(OBJ_DIR)/%.o,$(TEST_NAMES))

all: $(OBJECTS) $(TEST_OBJECTS) testbin/appundertest.so
	$(CC) $(CFLAGS) -rdynamic -shared -ldl $(OBJECTS) -o $(LIB_DIR)/libdash0envhook.so

$(OBJ_DIR)/%.o: $(SRC_DIR)/%.$(SRC_EXT)
	$(CC) -c -fPIC $^ -o $@ $(DEBUG) $(CFLAGS) $(LIBS)

$(OBJ_DIR)/%.o: $(TEST_DIR)/%.$(SRC_EXT)
	$(CC) -c $^ -o $@ $(DEBUG) $(CFLAGS) $(LIBS)

testbin/appundertest.so: test/appundertest.c
	$(CC) $(CFLAGS) -o $@ test/appundertest.c

clean:
	@rm -f $(OBJECTS) $(OBJ_DIR)/*.o $(TEST_OBJECTS) $(LIB_DIR)/*.so testbin/appundertest.so

.PHONY: all clean install
