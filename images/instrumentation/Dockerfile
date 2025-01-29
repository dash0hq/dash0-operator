# injector_build_start - do not remove this line (see images/instrumentation/injector/test/scripts/test-all.sh)

# build injector
FROM alpine:3.21.3 AS build-injector

## Zig is available as a community package on Alpine
RUN apk add zig --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community

COPY ./injector /dash0-init-container
ARG TARGETARCH
WORKDIR /dash0-init-container
RUN zig build -Dcpu-arch=${TARGETARCH} --summary all

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

FROM mcr.microsoft.com/dotnet/sdk:9.0 AS build-dotnet
COPY dotnet /build/dotnet
WORKDIR /build/dotnet
RUN dotnet restore

# build final image
FROM alpine:3.21.3
COPY copy-instrumentation.sh /

# copy artifacts (distros, injector binary) from the build stages to the final image
RUN mkdir -p /dash0-init-container/instrumentation
COPY --from=build-injector /dash0-init-container/bin/dash0_injector.so /dash0-init-container/dash0_injector.so
COPY --from=build-dotnet /build/dotnet/bin/otel-instrumentation/glibc /dash0-init-container/instrumentation/dotnet/glibc
COPY --from=build-dotnet /build/dotnet/bin/otel-instrumentation/muslc /dash0-init-container/instrumentation/dotnet/muslc
COPY --from=build-jvm /dash0-init-container/instrumentation/jvm/build /dash0-init-container/instrumentation/jvm
COPY --from=build-node.js /dash0-init-container/instrumentation/node.js /dash0-init-container/instrumentation/node.js

WORKDIR /
CMD ["/copy-instrumentation.sh"]
