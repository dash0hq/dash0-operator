# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# injector_build_start - do not remove this line (see images/instrumentation/injector/test/scripts/test-all.sh)

# build the LD_PRELOAD injector binary

FROM alpine:3.23.2 AS build-injector

RUN mkdir -p /opt/zig
WORKDIR /opt/zig
COPY injector/zig-version .
RUN . /opt/zig/zig-version && \
  case $(uname -m) in \
    x86_64)  export zig_architecture="x86_64" ;; \
    aarch64) export zig_architecture="aarch64" ;; \
    *)       echo "build architecture unsupported: $(uname -m)" && exit 1 ;; \
  esac && \
  wget -q -O /tmp/zig.tar.gz https://ziglang.org/download/${ZIG_VERSION%-*}/zig-${zig_architecture}-linux-${ZIG_VERSION%-*}.tar.xz && \
  tar --strip-components=1 -xf /tmp/zig.tar.gz
ENV PATH="$PATH:/opt/zig"

COPY injector /dash0-init-container
WORKDIR /dash0-init-container
ARG TARGETARCH
RUN zig build -Dcpu-arch=${TARGETARCH} --prominent-compile-errors --summary none

# injector_build_end - do not remove this line (see images/instrumentation/injector/test/scripts/test-all.sh)

# download OpenTelemetry Java agent
FROM eclipse-temurin:25-jdk-noble AS build-jvm
COPY jvm /dash0-init-container/instrumentation/jvm
WORKDIR /dash0-init-container/instrumentation/jvm
RUN ./mvnw dependency:copy-dependencies \
  && cp ./target/dependency/opentelemetry-javaagent-*.jar ./build/opentelemetry-javaagent.jar \
  && cp pom.xml ./build/pom.xml

# build Node.js artifacts
FROM node:25.3.0-alpine3.22 AS build-node-js
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
FROM alpine:3.23.2 AS build-dotnet
RUN apk add --no-cache \
		curl \
		unzip
COPY dotnet/opentelemetry-dotnet-instrumentation-version .
COPY dotnet/download-instrumentation.sh .
RUN ./download-instrumentation.sh

# build the final instrumentation image
FROM alpine:3.23.2
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
