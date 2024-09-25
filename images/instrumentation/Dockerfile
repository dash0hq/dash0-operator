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

# copy node.js artifacts
RUN mkdir -p /dash0-init-container/instrumentation
COPY --from=build-node.js /dash0-init-container/instrumentation/node.js /dash0-init-container/instrumentation/node.js

WORKDIR /
CMD ["/copy-instrumentation.sh"]
