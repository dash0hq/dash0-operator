# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

FROM --platform=${BUILDPLATFORM} golang:1.25.6-alpine3.23 AS builder

RUN apk add --update git make yq && apk cache clean

COPY src/builder /src/builder
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

FROM alpine:3.23.2
COPY src/image/entrypoint.sh /entrypoint.sh
COPY --from=builder /src/dist/dash0-operator-collector otelcol
USER 65532:65532
ENTRYPOINT ["/entrypoint.sh"]
