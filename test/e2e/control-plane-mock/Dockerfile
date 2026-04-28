# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

FROM --platform=${BUILDPLATFORM} golang:1.26.2-alpine3.23 AS builder

WORKDIR /usr/local/go/control_plane_mock

COPY go.mod /usr/local/go/control_plane_mock/go.mod
COPY go.sum /usr/local/go/control_plane_mock/go.sum
RUN go mod download

COPY *.go /usr/local/go/control_plane_mock

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build \
    -ldflags '-extldflags "-static"' \
    -v \
    control-plane-mock

FROM scratch
COPY --from=builder /usr/local/go/control_plane_mock/control-plane-mock /app/control-plane-mock
USER 65532:65532
ENTRYPOINT [ "/app/control-plane-mock" ]
