# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Note: This Dockerfile expects test/e2e as the build context.

FROM --platform=${BUILDPLATFORM} golang:1.25.5-alpine3.23 AS builder

WORKDIR /usr/local/go/src/telemetrymatcher

# Copy go.mod and friends and run go mod download separately before starting the container image build, to improve
# container build caching.
COPY otlp-sink/telemetrymatcher/go.mod .
COPY otlp-sink/telemetrymatcher/go.sum .
COPY pkg /usr/local/go/pkg
RUN go mod download

# now copy the actual go sources and compile them
COPY otlp-sink/telemetrymatcher/*.go .
COPY otlp-sink/telemetrymatcher/*.txt .

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build \
    -ldflags '-extldflags "-static"' \
    -v \
    telemetrymatcher

FROM --platform=${BUILDPLATFORM} alpine:3.23.2 AS certs
RUN apk --update add ca-certificates && apk cache clean

FROM scratch
COPY --from=builder /usr/local/go/src/telemetrymatcher/telemetrymatcher /app/telemetrymatcher
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT [ "/app/telemetrymatcher" ]
