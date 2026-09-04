# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Note: This Dockerfile expects dash0-operator/images as the build context.

FROM --platform=${BUILDPLATFORM} golang:1.27.0-alpine3.23 AS builder

RUN apk add --update git make yq && apk cache clean

COPY collector/src/builder /src/builder
# Custom internal-telemetry factory, adds k8s.node.uid to self-monitoring collector telemetry, plus the shared module
# it uses to resolve the node UID. The layout below has to match the paths in builder/config.yaml.
COPY collector/src/telemetry /src/telemetry
COPY pkg/nodeuid /src/pkg/nodeuid
WORKDIR /src

RUN export builder_version=$(yq '.connectors[0].gomod | match(" v(\d+\.\d+\.\d+)$"; "g") | .captures[0].string' builder/config.yaml) \
 && echo "Using OpenTelemetry collector builder version v${builder_version}." \
 && go install go.opentelemetry.io/collector/cmd/builder@v${builder_version}

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    builder --config=builder/config.yaml && \
    chmod u+x /src/dist/dash0-operator-collector

FROM alpine:3.24.1
COPY collector/src/image/entrypoint.sh /entrypoint.sh
COPY --from=builder /src/dist/dash0-operator-collector otelcol
USER 65532:65532
ENTRYPOINT ["/entrypoint.sh"]
