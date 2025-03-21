# injector_build_start - do not remove this line (see images/instrumentation/injector/test/scripts/test-all.sh)

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

# injector_build_end - do not remove this line (see images/instrumentation/injector/test/scripts/test-all.sh)

# download OpenTelemetry Java agent
FROM openjdk:24-jdk-bookworm AS build-jvm
COPY jvm /dash0-init-container/instrumentation/jvm
WORKDIR /dash0-init-container/instrumentation/jvm
RUN ./mvnw dependency:copy-dependencies \
  && cp ./target/dependency/opentelemetry-javaagent-*.jar ./build/opentelemetry-javaagent.jar \
  && cp pom.xml ./build/pom.xml

# build Node.js artifacts
FROM node:20.19.0-alpine3.21 AS build-node.js
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
FROM alpine:3.21.3
COPY copy-instrumentation.sh /

# copy artifacts (distros, injector binary) from the build stages to the final image
RUN mkdir -p /dash0-init-container/instrumentation
COPY --from=build-injector /dash0-init-container/dash0_injector.so /dash0-init-container/dash0_injector.so
COPY --from=build-jvm /dash0-init-container/instrumentation/jvm/build /dash0-init-container/instrumentation/jvm
COPY --from=build-node.js /dash0-init-container/instrumentation/node.js /dash0-init-container/instrumentation/node.js

WORKDIR /
CMD ["/copy-instrumentation.sh"]
