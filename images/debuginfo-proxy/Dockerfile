# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Note: This Dockerfile expects dash0-operator/images as the build context.

FROM --platform=${BUILDPLATFORM} golang:1.27.2-alpine3.24 AS builder
WORKDIR /usr/local/go/src/debuginfo-proxy
RUN apk add --no-cache ca-certificates
COPY debuginfo-proxy/src/go.mod debuginfo-proxy/src/go.sum ./
RUN go mod download
COPY debuginfo-proxy/src/ ./
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -ldflags '-s -w -extldflags "-static"' -trimpath -o /app/debuginfo-proxy .

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/debuginfo-proxy /app/debuginfo-proxy
USER 65532:65532
ENTRYPOINT ["/app/debuginfo-proxy"]
