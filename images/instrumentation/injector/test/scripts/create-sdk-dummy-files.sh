#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Add dummy Dash0 OTel SDKs/distros which actually do nothing but make the file check in the injector pass.
mkdir -p /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry
touch /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry/index.js
mkdir -p /__dash0__/instrumentation/jvm
touch /__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar

