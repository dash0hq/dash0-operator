# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Note: This Dockerfile expects dash0-operator/images as the build context.

# agent0-connector executes read-only kubectl commands, so a statically-linked kubectl binary has to be present in the
# final (scratch) image. We source it from the official Kubernetes-published kubectl image.
FROM registry.k8s.io/kubectl:v1.36.2 AS kubectl

FROM --platform=${BUILDPLATFORM} golang:1.26.4-alpine3.24 AS builder

WORKDIR /usr/local/go/src/agent0-connector

RUN apk add --no-cache ca-certificates

# Copy go.mod and go.sum and run go mod download separately before copying the actual sources, to improve container
# build caching.
COPY agent0-connector/src/go.mod agent0-connector/src/go.sum /usr/local/go/src/agent0-connector/
RUN go mod download

# now copy the actual go sources (including the proto/ subpackage) and compile them
COPY agent0-connector/src/ /usr/local/go/src/agent0-connector/

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build \
    -ldflags '-extldflags "-static"' \
    -v \
    agent0-connector

FROM scratch
COPY --from=kubectl /bin/kubectl /usr/local/bin/kubectl
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/local/go/src/agent0-connector/agent0-connector /app/agent0-connector
# kubectl must be on PATH; DASH0_KUBECTL_TMP points to a writable tmpfs/emptyDir so kubectl can write its discovery cache despite
# the read-only root filesystem.
ENV PATH="/usr/local/bin"
USER 65532:65532
ENTRYPOINT [ "/app/agent0-connector" ]
