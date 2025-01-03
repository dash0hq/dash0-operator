# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG instrumentation_image=dash0-instrumentation:latest
ARG test_image=openjdk:24-jdk-bookworm

FROM ${instrumentation_image} AS init

FROM ${test_image}

COPY test-cases /test-cases

RUN for test in /test-cases/*; do (cd "/${test}" && JAVA_HOME=$(which java | xargs dirname | xargs dirname) "./mvnw" package); done

# The following lines emulate the behavior of running the instrumentation image as a Kubernetes init container and
# setting the LD_PRELOAD environment variable via the operator.
COPY --from=init /dash0-init-container/*.so /__dash0__/
COPY --from=init /dash0-init-container/instrumentation /__dash0__/instrumentation
ENV LD_PRELOAD=/__dash0__/dash0_injector.so