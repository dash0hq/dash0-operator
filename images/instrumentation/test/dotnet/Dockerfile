# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG instrumentation_image=dash0-instrumentation:latest
ARG base_image_build=mcr.microsoft.com/dotnet/sdk:10.0-resolute
ARG base_image_run=mcr.microsoft.com/dotnet/aspnet:10.0-resolute

FROM ${instrumentation_image} AS init

FROM ${base_image_build} AS build
ARG base_image_build

WORKDIR /
COPY test-cases /test-cases

# adapt the target framework of all test-cases/apps to the .NET version of the build base image
RUN case "${base_image_build}" in \
    *:8.0*) target_framework=net8.0 ;; \
    *:9.0*) target_framework=net9.0 ;; \
    *) target_framework= ;; \
  esac && \
  if [ -n "${target_framework}" ]; then \
    find /test-cases -mindepth 2 -name '*.csproj' -exec \
      sed -i "s|<TargetFramework>net10.0</TargetFramework>|<TargetFramework>${target_framework}</TargetFramework>|" {} + \
  ; fi

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
COPY --from=init /dash0-instrumentation /__otel_auto_instrumentation
ENV LD_PRELOAD=/__otel_auto_instrumentation/injector/libotelinject.so
ENV OTEL_INJECTOR_CONFIG_FILE=/__otel_auto_instrumentation/injector/injector.conf
