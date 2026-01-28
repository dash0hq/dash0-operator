# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# download the opentelemetry-injector binary for the target architecture
FROM alpine:3.23.3 AS download-injector-binary

RUN apk add --no-cache curl jq wget

COPY opentelemetry-injector/version /tmp/opentelemetry-injector-version

ARG TARGETARCH

# Temporary change to use locally compiled injector binaries:
COPY opentelemetry-injector/libotelinject_${TARGETARCH}.so /libotelinject.so

# download OpenTelemetry Java agent
FROM eclipse-temurin:25-jdk-noble AS build-jvm
COPY jvm /dash0-init-container/jvm
WORKDIR /dash0-init-container/jvm
RUN ./mvnw dependency:copy-dependencies \
  && cp ./target/dependency/opentelemetry-javaagent-*.jar ./build/opentelemetry-javaagent.jar \
  && cp pom.xml ./build/pom.xml

# build Node.js artifacts
FROM node:25.5.0-alpine3.22 AS build-node-js
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
FROM alpine:3.23.3 AS build-dotnet
RUN apk add --no-cache \
		curl \
		unzip
COPY dotnet/opentelemetry-dotnet-instrumentation-version .
COPY dotnet/download-instrumentation.sh .
RUN ./download-instrumentation.sh

# install Python artifacts (glibc)
FROM python:3.14.2-trixie AS build-python-glibc
RUN mkdir -p /dash0-init-container/python
WORKDIR /dash0-init-container/python
ADD python/requirements.txt .
RUN pip install --target . -r requirements.txt
ADD python/usercustomize.py .

# install Python artifacts (musl)
FROM python:3.14.2-alpine3.23 AS build-python-musl
RUN mkdir -p /dash0-init-container/python
WORKDIR /dash0-init-container/python
ADD python/requirements.txt .
RUN pip install --target . -r requirements.txt
ADD python/usercustomize.py .

# build the final instrumentation image
FROM alpine:3.23.3
COPY copy-instrumentation.sh /

# copy artifacts (distros, injector binary) from the build stages to the final image
COPY --from=download-injector-binary /libotelinject.so /dash0-init-container/injector/libotelinject.so
COPY opentelemetry-injector/otelinject.conf /dash0-init-container/injector/otelinject.conf
COPY --from=build-dotnet /glibc /dash0-init-container/agents/dotnet/glibc
COPY --from=build-dotnet /musl /dash0-init-container/agents/dotnet/musl
COPY --from=build-jvm /dash0-init-container/jvm/build /dash0-init-container/agents/jvm
COPY --from=build-node-js /dash0-init-container/node.js /dash0-init-container/agents/node.js
COPY --from=build-python-glibc /dash0-init-container/python /dash0-init-container/agents/python/glibc
COPY --from=build-python-musl /dash0-init-container/python /dash0-init-container/agents/python/musl

WORKDIR /
CMD ["/copy-instrumentation.sh"]
