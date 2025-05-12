# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG instrumentation_image=dash0-instrumentation:latest
ARG base_image=openjdk:24-jdk-bookworm

FROM ${instrumentation_image} AS init

FROM ${base_image}

COPY jvm-test-utils /jvm-test-utils
COPY test-cases /test-cases

RUN export JAVA_HOME=$(which java | xargs dirname | xargs dirname)

WORKDIR /jvm-test-utils
RUN javac src/com/dash0/injector/testutils/*.java

WORKDIR /
RUN for test in /test-cases/*; do \
  cd "/${test}" && \
  cp -R /jvm-test-utils/src/* . && \
  javac \
    -cp /jvm-test-utils/src:"/${test}" \
    Main.java && \
  jar --create --file app.jar --manifest MANIFEST.MF -C . . \
; done

# The following lines emulate the behavior of running the instrumentation image as a Kubernetes init container and
# setting the LD_PRELOAD environment variable via the operator.
COPY --from=init /dash0-init-container/*.so /__dash0__/
COPY --from=init /dash0-init-container/instrumentation /__dash0__/instrumentation
ENV LD_PRELOAD=/__dash0__/dash0_injector.so
