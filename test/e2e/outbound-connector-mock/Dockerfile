# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

FROM --platform=${BUILDPLATFORM} golang:1.26.4-alpine3.23 AS builder

WORKDIR /usr/local/go/outbound_connector_mock

COPY go.mod /usr/local/go/outbound_connector_mock/go.mod
COPY go.sum /usr/local/go/outbound_connector_mock/go.sum
RUN go mod download

COPY *.go /usr/local/go/outbound_connector_mock/
COPY proto/ /usr/local/go/outbound_connector_mock/proto/

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build \
    -ldflags '-extldflags "-static"' \
    -v \
    outbound-connector-mock

FROM scratch
COPY --from=builder /usr/local/go/outbound_connector_mock/outbound-connector-mock /app/outbound-connector-mock
USER 65532:65532
ENTRYPOINT [ "/app/outbound-connector-mock" ]
