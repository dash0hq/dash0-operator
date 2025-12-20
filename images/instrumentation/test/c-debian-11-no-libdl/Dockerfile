# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG instrumentation_image=dash0-instrumentation:latest
ARG base_image=alpine:3.23.2

FROM ${instrumentation_image} AS init

FROM ${base_image}

# Need to repeat ARG base_image since build args are cleared by FROM.
ARG base_image

RUN case ${base_image} in \
  *alpine*) apk update && apk upgrade --no-cache && apk add --no-cache build-base ;; \
  *) apt-get update && apt-get install -y --no-install-recommends build-essential && apt-get clean; \
esac

COPY Makefile .
COPY test-cases /test-cases

RUN make

# The following lines emulate the behavior of running the instrumentation image as a Kubernetes init container and
# setting the LD_PRELOAD environment variable via the operator.
COPY --from=init /dash0-init-container/*.so /__dash0__/
COPY --from=init /dash0-init-container/instrumentation /__dash0__/instrumentation
ENV LD_PRELOAD=/__dash0__/dash0_injector.so