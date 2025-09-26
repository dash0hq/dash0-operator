# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Note: This Dockerfile expects dash0-operator/images as the build context.

FROM --platform=${BUILDPLATFORM} golang:1.25-alpine3.22 AS builder

WORKDIR /usr/local/go/src/configreloader

# Copy go.mod and friends and run go mod download separately before starting the container image build, to improve
# container build caching.
COPY configreloader/src/go.mod /usr/local/go/src/configreloader/go.mod
COPY configreloader/src/go.sum /usr/local/go/src/configreloader/go.sum
COPY pkg /usr/local/go/pkg
RUN go mod download

# now copy the actual go sources and compile them
COPY configreloader/src/*.go /usr/local/go/src/configreloader

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build \
    -ldflags '-extldflags "-static"' \
    -v \
    configreloader

FROM --platform=${BUILDPLATFORM} alpine:3.22.1 AS certs
RUN apk --update add ca-certificates && apk cache clean

FROM scratch
COPY --from=builder /usr/local/go/src/configreloader/configreloader /app/configreloader
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT [ "/app/configreloader" ]
