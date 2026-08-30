# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# The build settings and the base image match images/collector/Dockerfile, so that the probe resolves names and dials
# the metadata endpoint exactly like the collector does.

FROM --platform=${BUILDPLATFORM} golang:1.27.0-alpine3.23 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build -o /src/gcp-detector-probe .

FROM alpine:3.24.1
COPY --from=builder /src/gcp-detector-probe /gcp-detector-probe
USER 65532:65532
ENTRYPOINT ["/gcp-detector-probe"]
