# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG instrumentation_image=dash0-instrumentation:latest
ARG base_image_build=mcr.microsoft.com/dotnet/sdk:9.0-bookworm-slim
ARG base_image_run=mcr.microsoft.com/dotnet/aspnet:9.0-bookworm-slim

FROM ${instrumentation_image} AS init

FROM ${base_image_build} AS build

WORKDIR /
COPY test-cases /test-cases

# run dotnet restore for all test-cases/apps as a separate image layer (speeds up docker build)
RUN for test in /test-cases/*; do \
  cd "/${test}" && \
  dotnet restore \
; done
# build all test-cases/apps
RUN for test in /test-cases/*; do \
  cd "/${test}" && \
  dotnet publish -o . \
; done

# build the final image
FROM ${base_image_run}

COPY --from=build /test-cases /test-cases

# The following lines emulate the behavior of running the instrumentation image as a Kubernetes init container and
# setting the LD_PRELOAD environment variable via the operator.
COPY --from=init /dash0-init-container /__otel_auto_instrumentation
ENV LD_PRELOAD=/__otel_auto_instrumentation/injector/libotelinject.so
ENV OTEL_INJECTOR_CONFIG_FILE=/__otel_auto_instrumentation/injector/otelinject.conf
