# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG instrumentation_image=dash0-instrumentation:latest
ARG base_image_build=golang:1.25.5-alpine3.23
ARG base_image_run=scratch

FROM ${instrumentation_image} AS init

FROM --platform=${BUILDPLATFORM} ${base_image_build} AS builder

WORKDIR /build
COPY Makefile .
COPY test-cases test-cases

RUN apk update && apk upgrade --no-cache && apk add --no-cache build-base

ARG TARGETOS
ARG TARGETARCH

RUN make build

FROM ${base_image_run}
COPY --from=builder /build/test-cases /test-cases
USER 65532:65532

# The following lines emulate the behavior of running the instrumentation image as a Kubernetes init container and
# setting the LD_PRELOAD environment variable via the operator.
COPY --from=init /dash0-init-container /__otel_auto_instrumentation
ENV LD_PRELOAD=/__otel_auto_instrumentation/injector/libotelinject.so
ENV OTEL_INJECTOR_CONFIG_FILE=/__otel_auto_instrumentation/injector/otelinject.conf
