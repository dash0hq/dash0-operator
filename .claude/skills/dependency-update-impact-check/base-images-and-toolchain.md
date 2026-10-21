# Base images and the Go toolchain

This file applies when the update changes:

* a `FROM` line or an image `ARG` default in `Dockerfile` or `images/*/Dockerfile`. Changes under
  `images/instrumentation/test` are tier C.
* the `go` directive of the `go.mod` files and the golang image tags. These are updated together by
  `.github/workflows/update-go-versions.yaml`.

The kubectl image in `images/agent0-connector/Dockerfile` is covered by `go-modules.md`. The target-allocator image is
covered by `collector.md`.

## Classify each image

Read every Dockerfile that the PR changes:

* Runtime base image: the `FROM` of the final stage, i.e. what ships. Tier A.
* Build stage whose output ships, i.e. files that a later stage copies with `COPY --from`. Tier B, with these
  exceptions:
  * Python packages and Ruby gems with native extensions: tier A. They are built with the interpreter of the build
    image, but run with the customer's interpreter version.
  * CA certificates copied from an Alpine stage (`images/configreloader`, `images/filelogoffsetsync`,
    `images/agent0-connector`): tier A, because the binaries of these images use them for their TLS connections. The
    Alpine lookup of the runtime base images applies.
  * The kubectl binary copied from the kubectl image: covered by `go-modules.md`.
* Build stage whose output is only the compiled Go binary (golang images): covered by the toolchain section below.
  Check the `COPY --from` lines nevertheless. In `images/agent0-connector/Dockerfile`, the golang stage also installs
  the CA certificates that ship (`apk add ca-certificates`), so they change with the Alpine version of the golang tag.

## Lookups for runtime base images

* Shell scripts that run in the image: the scripts that the Dockerfiles copy in and use as `ENTRYPOINT` or `CMD`, for
  example `images/collector/src/image/entrypoint.sh`, `images/instrumentation/copy-instrumentation.sh`, and the
  `entrypoint.sh` of `images/volumeownership` and `images/filelogoffsetvolumeownership`. Check every command and option
  that they use (BusyBox applets and their options, the shell) against the BusyBox changelog and the Alpine release
  notes.
* Image configuration: default user, `PATH`, working directory, entrypoint. These can be compared without docker
  through the registry API: the image config blob of both tags, fetched with an anonymous pull token.
* Alpine: the musl version, the CA certificates package, and removed packages.
* distroless: the nonroot UID, CA certificates, tzdata.
* User IDs that the operator sets explicitly (for example `RunAsUser` in `desired_state.go` and
  `workload_modifier.go`, `USER` in the Dockerfiles) must still work with the new image.

Release notes: the Alpine release notes, the BusyBox changelog, and the commit history of the distroless repository.

## Lookups for build images

* Python and Ruby build images in `images/instrumentation/Dockerfile`: the interpreter version vs. the range of
  customer runtime versions that the distribution supports (see `helm-chart/dash0-operator/README.md`). Native
  extensions or wheels built for a newer ABI may not load in older runtimes.
* Node.js and JDK build images: only the package manager and the build tools change. Check whether `npm ci` or the
  Maven wrapper behave differently, for example lockfile handling.

## Go toolchain

* Release notes: `https://go.dev/doc/go1.X` for a new minor version, and the release history
  (`https://go.dev/doc/devel/release`) for patch versions.
* The `go` directive of the main module selects the default `GODEBUG` settings of the binary (see
  `https://go.dev/doc/godebug`). Raising the `go` directive across a minor version can therefore change runtime
  behavior even if the code does not change. Typical examples are TLS, X.509, HTTP/2 and `net` defaults. Go through
  the `GODEBUG` changes that the release notes of every minor version in the range list.
* Also check the release notes for removed ports or new minimum OS or kernel requirements, and for behavior changes
  in `net/http`, `crypto/tls`, `crypto/x509`, `os/exec`, `time` and `encoding/json`.
* Usage inventory: TLS clients and servers in the operator and its images:
  * the webhook server and the API clients of the operator manager
  * the exporters of the configreloader, filelogoffsetsync and agent0-connector
  * the collector, which is built with the golang image as well

  Also check for explicit `GODEBUG` settings (`//go:debug` directives, `godebug` blocks in `go.mod`, `GODEBUG` env
  vars in Dockerfiles, `desired_state.go` and the Helm chart).
