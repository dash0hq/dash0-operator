# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG instrumentation_image=dash0-instrumentation:latest
ARG base_image=python:3.13.7-slim

FROM ${instrumentation_image} AS init

FROM ${base_image}

COPY test-cases /test-cases

# The following lines emulate the behavior of running the instrumentation image as a Kubernetes init container and
# setting the LD_PRELOAD environment variable via the operator.
COPY --from=init /dash0-init-container /__otel_auto_instrumentation
ENV LD_PRELOAD=/__otel_auto_instrumentation/injector/libotelinject.so
ENV OTEL_INJECTOR_CONFIG_FILE=/__otel_auto_instrumentation/injector/otelinject.conf
