# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# download the opentelemetry-injector binary for the target architecture
FROM alpine:3.23.2 AS download-injector-binary

RUN apk add --no-cache curl jq wget

COPY opentelemetry-injector/version /tmp/opentelemetry-injector-version

ARG TARGETARCH
RUN injector_version=$(cat /tmp/opentelemetry-injector-version) && \
  case ${TARGETARCH} in \
    amd64) export injector_binary="libotelinject_amd64.so" ;; \
    arm64) export injector_binary="libotelinject_arm64.so" ;; \
    *)     echo "architecture unsupported: ${TARGETARCH}" && exit 1 ;; \
  esac && \
  wget -q -O /libotelinject.so "https://github.com/open-telemetry/opentelemetry-injector/releases/download/${injector_version}/${injector_binary}" && \
  export checksum=$(curl "https://api.github.com/repos/open-telemetry/opentelemetry-injector/releases/tags/$injector_version" | jq -r ".assets[] | select(.name==\"$injector_binary\") | .digest") && \
  export checksum=${checksum#sha256:} && \
  echo "${checksum} libotelinject.so" > checksum.txt && \
  sha256sum -c checksum.txt

# download OpenTelemetry Java agent
FROM eclipse-temurin:25-jdk-noble AS build-jvm
COPY jvm /dash0-init-container/jvm
WORKDIR /dash0-init-container/jvm
RUN ./mvnw dependency:copy-dependencies \
  && cp ./target/dependency/opentelemetry-javaagent-*.jar ./build/opentelemetry-javaagent.jar \
  && cp pom.xml ./build/pom.xml

# build Node.js artifacts
FROM node:25.3.0-alpine3.22 AS build-node-js
RUN mkdir -p /dash0-init-container/node.js
WORKDIR /dash0-init-container/node.js
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
COPY --from=download-injector-binary /libotelinject.so /dash0-init-container/injector/libotelinject.so
COPY opentelemetry-injector/otelinject.conf /dash0-init-container/injector/otelinject.conf
COPY --from=build-dotnet /glibc /dash0-init-container/agents/dotnet/glibc
COPY --from=build-dotnet /musl /dash0-init-container/agents/dotnet/musl
COPY --from=build-jvm /dash0-init-container/jvm/build /dash0-init-container/agents/jvm
COPY --from=build-node-js /dash0-init-container/node.js /dash0-init-container/agents/node.js

WORKDIR /
CMD ["/copy-instrumentation.sh"]
