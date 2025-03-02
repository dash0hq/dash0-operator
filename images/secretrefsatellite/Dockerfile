# Note: This Dockerfile expects dash0-operator/images as the build context.

FROM --platform=${BUILDPLATFORM} golang:1.24-alpine3.21 AS builder

WORKDIR /usr/local/go/src/secretrefsatellite

# Copy go.mod and friends and run go mod download separately before starting the container image build, to improve
# container build caching.
COPY secretrefsatellite/src/go.mod /usr/local/go/src/secretrefsatellite/go.mod
COPY secretrefsatellite/src/go.sum /usr/local/go/src/secretrefsatellite/go.sum
COPY pkg /usr/local/go/pkg
RUN go mod download

# now copy the actual go sources and compile them
COPY secretrefsatellite/src/*.go /usr/local/go/src/secretrefsatellite

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags '-extldflags "-static"' secretrefsatellite

FROM scratch
COPY --from=builder /usr/local/go/src/secretrefsatellite/secretrefsatellite /app/secretrefsatellite
USER 65532:65532
ENTRYPOINT [ "/app/secretrefsatellite" ]
