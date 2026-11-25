# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Note: This Dockerfile expects dash0-operator/images as the build context.

FROM --platform=${BUILDPLATFORM} golang:1.27.4-alpine3.24 AS builder

WORKDIR /usr/local/go/src/scrape-coordinator

RUN apk add --no-cache ca-certificates

# Copy go.mod and go.sum and run go mod download separately before copying the actual sources, to improve container
# build caching.
COPY scrape-coordinator/src/go.mod scrape-coordinator/src/go.sum /usr/local/go/src/scrape-coordinator/
COPY pkg /usr/local/go/pkg
RUN go mod download

# now copy the actual go sources (including the internal/ subpackages) and compile them
COPY scrape-coordinator/src/ /usr/local/go/src/scrape-coordinator/

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build \
    -ldflags '-s -w' \
    -trimpath \
    -v \
    -o /app/scrape-coordinator \
    .

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/scrape-coordinator /app/scrape-coordinator
USER 65532:65532
ENTRYPOINT [ "/app/scrape-coordinator" ]
