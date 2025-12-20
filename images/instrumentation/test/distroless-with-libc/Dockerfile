# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG instrumentation_image=dash0-instrumentation:latest
ARG base_image_build=debian:bookworm-slim
ARG base_image_run=gcr.io/distroless/cc-debian12

FROM ${instrumentation_image} AS init

FROM ${base_image_build} AS builder
ARG base_image_build=alpine:3.23.2

RUN case ${base_image_build} in \
  *alpine*) apk update && apk upgrade --no-cache && apk add --no-cache build-base ;; \
  *) apt-get update && apt-get install -y --no-install-recommends build-essential && apt-get clean; \
esac

WORKDIR /build
COPY Makefile .
COPY test-cases test-cases

RUN make && ls -R test-cases

FROM ${base_image_run}
COPY --from=builder /build/test-cases /test-cases
USER 65532:65532

# The following lines emulate the behavior of running the instrumentation image as a Kubernetes init container and
# setting the LD_PRELOAD environment variable via the operator.
COPY --from=init /dash0-init-container/*.so /__dash0__/
COPY --from=init /dash0-init-container/instrumentation /__dash0__/instrumentation
ENV LD_PRELOAD=/__dash0__/dash0_injector.so
