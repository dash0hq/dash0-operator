# Note: This Dockerfile expects dash0-operator/images as the build context.

FROM --platform=${BUILDPLATFORM} golang:1.24-alpine3.21 AS builder

WORKDIR /usr/local/go/src/filelogoffsetsync

# Copy go.mod and friends and run go mod download separately before starting the container image build, to improve
# container build caching.
COPY filelogoffsetsync/src/go.mod /usr/local/go/src/filelogoffsetsync/go.mod
COPY filelogoffsetsync/src/go.sum /usr/local/go/src/filelogoffsetsync/go.sum
COPY pkg /usr/local/go/pkg
RUN go mod download

# now copy the actual go sources and compile them
COPY filelogoffsetsync/src/*.go /usr/local/go/src/filelogoffsetsync

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build \
    -ldflags '-extldflags "-static"' \
    -v \
    filelogoffsetsync

FROM --platform=${BUILDPLATFORM} alpine:3.21.3 AS certs
RUN apk --update add ca-certificates && apk cache clean

FROM scratch
COPY --from=builder /usr/local/go/src/filelogoffsetsync/filelogoffsetsync /app/filelogoffsetsync
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT [ "/app/filelogoffsetsync" ]
