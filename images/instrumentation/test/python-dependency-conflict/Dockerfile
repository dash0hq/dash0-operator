# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG instrumentation_image=dash0-instrumentation:latest
ARG base_image=python:3.14.2-slim-trixie

FROM ${instrumentation_image} AS init

FROM ${base_image}

ENV PIP_DISABLE_PIP_VERSION_CHECK=1
COPY test-cases /test-cases
WORKDIR /
# Note: The individual pip install commands from each test case will all install dependencies into the system-wide
# PYTHONHOME. Depencies of test cases are not isolated from each other.
RUN for test in /test-cases/*; do \
  cd "${test}" && \
  if [ -f requirements.txt ]; then \
    pip install --no-cache-dir -r requirements.txt; \
  fi \
; done

# The following lines emulate the behavior of running the instrumentation image as a Kubernetes init container and
# setting the LD_PRELOAD environment variable via the operator.
COPY --from=init /dash0-init-container /__otel_auto_instrumentation
ENV LD_PRELOAD=/__otel_auto_instrumentation/injector/libotelinject.so
ENV OTEL_INJECTOR_CONFIG_FILE=/__otel_auto_instrumentation/injector/otelinject-with-python.conf
