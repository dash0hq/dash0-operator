FROM --platform=${BUILDPLATFORM} golang:1.25.7 AS builder

WORKDIR /workspace

# copy the Go Modules manifests individually to improve container build caching (see
# go mod download step below)
COPY go.mod go.mod
COPY go.sum go.sum

# These particular COPY instructions need to be executed before go mod download since it is referenced by a replace
# directive in go.mod.
COPY images/pkg/common/ images/pkg/common/
# Only used in test/e2e sources, not in production code.
COPY test/e2e/pkg/shared test/e2e/pkg/shared

# download dependencies before building and copying the sources, so that we don't need to re-download as much
# and so that source changes do not invalidate the cached container image build layer
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/

ARG TARGETOS
ARG TARGETARCH

# Note: By using --platform=${BUILDPLATFORM} in the FROM statement for the build stage, we run the cross-platform
# compilation (specifically, cross-CPU-architecture compilation) for the multi-platform image on the platform/CPU
# architecture of the machine running the docker build, instead of running the build via emulation (QEMU); go build
# takes care of producing the correct binary for the target architecture on its own.
# See https://www.docker.com/blog/faster-multi-platform-builds-dockerfile-cross-compilation-guide/.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -a -o manager cmd/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
