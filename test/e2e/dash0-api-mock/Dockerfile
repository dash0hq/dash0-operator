# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

FROM --platform=${BUILDPLATFORM} golang:1.25.5-alpine3.22 AS builder

WORKDIR /usr/local/go/dash0_api_mock

COPY go.mod /usr/local/go/dash0_api_mock/go.mod
COPY go.sum /usr/local/go/dash0_api_mock/go.sum
RUN go mod download

COPY *.go /usr/local/go/dash0_api_mock

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build \
    -ldflags '-extldflags "-static"' \
    -v \
    dash0-api-mock

FROM scratch
COPY --from=builder /usr/local/go/dash0_api_mock/dash0-api-mock /app/dash0-api-mock
USER 65532:65532
ENTRYPOINT [ "/app/dash0-api-mock" ]
