# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# injector_build_start - do not remove this line (see images/instrumentation/injector/test/scripts/test-all.sh)

# build the LD_PRELOAD injector binary

FROM alpine:3.22.1 AS build-injector
COPY injector/zig-version /dash0-init-container/zig-version
RUN source /dash0-init-container/zig-version && \
  apk add zig="$ZIG_VERSION"
COPY injector /dash0-init-container
WORKDIR /dash0-init-container
ARG TARGETARCH
RUN zig build -Dcpu-arch=${TARGETARCH} --prominent-compile-errors --summary none

# injector_build_end - do not remove this line (see images/instrumentation/injector/test/scripts/test-all.sh)

# download OpenTelemetry Java agent
FROM openjdk:26-jdk-bookworm AS build-jvm
COPY jvm /dash0-init-container/instrumentation/jvm
WORKDIR /dash0-init-container/instrumentation/jvm
RUN ./mvnw dependency:copy-dependencies \
  && cp ./target/dependency/opentelemetry-javaagent-*.jar ./build/opentelemetry-javaagent.jar \
  && cp pom.xml ./build/pom.xml

# build Node.js artifacts
FROM node:22.19.0-alpine3.22 AS build-node-js
RUN mkdir -p /dash0-init-container/instrumentation/node.js
WORKDIR /dash0-init-container/instrumentation/node.js
COPY node.js/package* .
RUN NPM_CONFIG_UPDATE_NOTIFIER=false \
  npm ci \
  --ignore-scripts \
  --omit=dev \
  --no-audit \
  --no-fund=true

# download .NET auto instrumentation
FROM alpine:3.22.1 AS build-dotnet
RUN apk add --no-cache \
		curl \
		unzip
COPY dotnet/opentelemetry-dotnet-instrumentation-version .
COPY dotnet/download-instrumentation.sh .
RUN ./download-instrumentation.sh

# build the final instrumentation image
FROM alpine:3.22.1
COPY copy-instrumentation.sh /

# copy artifacts (distros, injector binary) from the build stages to the final image
RUN mkdir -p /dash0-init-container/instrumentation
COPY --from=build-injector /dash0-init-container/dash0_injector.so /dash0-init-container/dash0_injector.so
COPY --from=build-dotnet /glibc /dash0-init-container/instrumentation/dotnet/glibc
COPY --from=build-dotnet /musl /dash0-init-container/instrumentation/dotnet/musl
COPY --from=build-jvm /dash0-init-container/instrumentation/jvm/build /dash0-init-container/instrumentation/jvm
COPY --from=build-node-js /dash0-init-container/instrumentation/node.js /dash0-init-container/instrumentation/node.js

WORKDIR /
CMD ["/copy-instrumentation.sh"]
