# build injector
FROM ubuntu:24.04 AS build-injector

RUN apt-get update && \
  apt-get install --no-install-recommends build-essential -y && \
  apt-get autoremove -y && \
  apt-get clean -y

COPY ./injector /dash0-init-container
WORKDIR /dash0-init-container
RUN gcc \
  -shared \
  -nostdlib \
  -fPIC \
  -Wl,--version-script=src/dash0_injector.exports.map \
  src/dash0_injector.c \
  -o dash0_injector.so

# download OpenTelemetry Java agent
FROM alpine:3.21.0 AS build-jvm
ARG otel_javaagent_version="latest"
RUN apk --no-cache add curl jq
RUN mkdir -p /dash0-init-container/instrumentation/jvm
WORKDIR /dash0-init-container/instrumentation/jvm
ENV VERSION=$otel_javaagent_version
RUN curl -s "https://api.github.com/repos/open-telemetry/opentelemetry-java-instrumentation/releases/tags/${VERSION}" | \
  jq -r '.assets[] | select(.content_type == "application/java-archive") | .browser_download_url' | \
  xargs curl -s -L -o opentelemetry-javaagent.jar

# build Node.js artifacts
FROM node:20.13.1-alpine3.19 AS build-node.js
RUN mkdir -p /dash0-init-container/instrumentation/node.js
WORKDIR /dash0-init-container/instrumentation/node.js
COPY node.js/package* ./
COPY node.js/dash0hq-opentelemetry-*.tgz .
RUN NPM_CONFIG_UPDATE_NOTIFIER=false \
  npm ci \
  --ignore-scripts \
  --omit=dev \
  --no-audit \
  --no-fund=true

# build final image
FROM alpine:3.19.1
COPY copy-instrumentation.sh /

# copy artifacts (distros, injector binary) from the build stages to the final image
RUN mkdir -p /dash0-init-container/instrumentation
COPY --from=build-injector /dash0-init-container/dash0_injector.so /dash0-init-container/dash0_injector.so
COPY --from=build-jvm /dash0-init-container/instrumentation/jvm /dash0-init-container/instrumentation/jvm
COPY --from=build-node.js /dash0-init-container/instrumentation/node.js /dash0-init-container/instrumentation/node.js

WORKDIR /
CMD ["/copy-instrumentation.sh"]
