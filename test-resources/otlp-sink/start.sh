#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"/../..

rm -rf test-resources/e2e/volumes/otlp-sink/traces.jsonl
rm -rf test-resources/e2e/volumes/otlp-sink/metrics.jsonl
rm -rf test-resources/e2e/volumes/otlp-sink/logs.jsonl
touch	test-resources/e2e/volumes/otlp-sink/traces.jsonl
touch	test-resources/e2e/volumes/otlp-sink/metrics.jsonl
touch	test-resources/e2e/volumes/otlp-sink/logs.jsonl

docker run \
  --rm \
  -v "$(pwd)/test-resources/otlp-sink/config.yaml:/etc/otelcol-contrib/config.yaml" \
  -v "$(pwd)/test-resources/e2e/volumes/otlp-sink:/telemetry" \
  -p 127.0.0.1:4317:4317 \
  -p 127.0.0.1:4318:4318 \
  -p 127.0.0.1:55679:55679 \
  -p 127.0.0.1:13100:13100 \
  otel/opentelemetry-collector-contrib:0.136.0
