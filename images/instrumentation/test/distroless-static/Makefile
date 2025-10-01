# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARCH := $(shell uname -m)
CFLAGS ?= -Wall -Werror -Wextra -O2

TEST_CASES_DIR := test-cases
SRC_EXT := go

OS := $(shell uname -s)
SHELL := sh

TEST_SOURCES := $(wildcard $(TEST_CASES_DIR)/**/main.$(SRC_EXT))
$(info TEST_SOURCES $(TEST_SOURCES))
TEST_OBJECTS := $(patsubst %/main.go,%/app,$(TEST_SOURCES))
$(info TEST_OBJECTS $(TEST_OBJECTS))
TEST_CASE_NAMES := $(sort $(patsubst test-cases/%/,%,$(dir $(TEST_SOURCES))))
$(info TEST_CASE_NAMES $(TEST_CASE_NAMES))

.PHONY: all
all: clean build

.PHONY: clean
clean:
	rm -f $(TEST_OBJECTS)

.PHONY: build
build: $(TEST_CASE_NAMES)

$(TEST_CASE_NAMES):
	cd $(TEST_CASES_DIR)/$@/ && \
	CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags '-extldflags "-static"' -v .
