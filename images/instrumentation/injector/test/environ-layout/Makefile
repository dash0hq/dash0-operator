# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARCH := $(shell uname -m)
CFLAGS ?= -Wall -Werror -Wextra -Wl,-z,undefs -O2
SRC_EXT := c
OS := $(shell uname -s)
SHELL := sh

SOURCES := $(wildcard *.$(SRC_EXT))
#$(info SOURCES $(SOURCES))
OBJECTS := $(patsubst %.c,%.o,$(SOURCES))
# $(info OBJECTS $(OBJECTS))

.PHONY: all
all: clean build

.PHONY: clean
clean:
	@rm -f $(OBJECTS)

.PHONY: build
build: $(OBJECTS)

%.o: %.c $(DEPS)
	$(CC) $(CFLAGS) $< -o $@
	@chmod 755 $@
