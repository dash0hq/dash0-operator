#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Add an inaccessible dummy Dash0 OTel SDKs/distros.
mkdir -p /__dash0__/instrumentation/node.js/node_modules/@dash0/opentelemetry
touch /__dash0__/instrumentation/node.js/node_modules/@dash0/opentelemetry/index.js
mkdir -p /__dash0__/instrumentation/jvm && touch /__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar
chmod -R 600 /__dash0__/instrumentation

