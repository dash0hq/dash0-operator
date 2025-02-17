# Note: This Dockerfile expects dash0-operator/images as the build context.

FROM --platform=${BUILDPLATFORM} golang:1.24-alpine3.21 AS builder

WORKDIR /usr/local/go/src/secretrefresolver

# Copy go.mod and friends and run go mod download separately before starting the container image build, to improve
# container build caching.
COPY secretrefresolver/src/go.mod /usr/local/go/src/secretrefresolver/go.mod
COPY secretrefresolver/src/go.sum /usr/local/go/src/secretrefresolver/go.sum
COPY pkg /usr/local/go/pkg
RUN go mod download

# now copy the actual go sources and compile them
COPY secretrefresolver/src/*.go /usr/local/go/src/secretrefresolver

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags '-extldflags "-static"' secretrefresolver

FROM scratch
COPY --from=builder /usr/local/go/src/secretrefresolver/secretrefresolver /app/secretrefresolver
USER 65532:65532
ENTRYPOINT [ "/app/secretrefresolver" ]
