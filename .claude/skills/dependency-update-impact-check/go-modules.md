# Go modules of the operator's own binaries

This file applies when the update changes the `require` lines of one of these modules:

| Module | Binary | Built by |
|---|---|---|
| `go.mod` (root) | operator manager | `Dockerfile` (`go build ... cmd/main.go`) |
| `images/pkg/common/go.mod` | library, linked into the manager, configreloader, filelogoffsetsync and agent0-connector | – |
| `images/configreloader/src/go.mod` | configreloader | `images/configreloader/Dockerfile` |
| `images/filelogoffsetsync/src/go.mod` | filelogoffsetsync | `images/filelogoffsetsync/Dockerfile` |
| `images/agent0-connector/src/go.mod` | agent0-connector | `images/agent0-connector/Dockerfile` |

Collector modules, including the collector modules in the root `go.mod` and `images/collector/src/telemetry/go.mod`,
are covered by `collector.md`. The `go` directive and the golang images are covered by
`base-images-and-toolchain.md`. The modules under `test/` are tier C. So are the root `go.mod` requirements that only
tests use (ginkgo, gomega, gock): check this with the `go list -deps` output below rather than assuming it.

## Determine what is compiled in

In each module whose `go.mod` changed, list the modules that the shipped binary links, in the head copy (step 1 of
SKILL.md):
`GOOS=linux CGO_ENABLED=0 go -C <head copy>/<module dir> list -deps -f '{{with .Module}}{{.Path}} {{.Version}}{{end}}' <package> | sort -u`.
`<package>` is `./cmd` for the root module and `.` for the modules in `images/*/src`. The Dockerfiles of the images
build inside `GOROOT/src`, so their `go build` lines cannot be used as they are. For the libraries in `images/pkg`, use
the binaries that link them.

An updated module that does not appear in the list is tier C. An `// indirect` module that appears in the list is
tier B, unless the operator code imports it directly. A module that the PR adds to `go.mod` and that appears in the
list is an added dependency (step 1 of SKILL.md). `go mod why -m <module>` shows why it is linked.

## Upstream sources

| Module | Release notes |
|---|---|
| `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, other `k8s.io/*` staging modules | `v0.X.Y` belongs to Kubernetes `v1.X.Y`: `CHANGELOG/CHANGELOG-1.X.md` in `kubernetes/kubernetes` |
| `sigs.k8s.io/controller-runtime` | GitHub releases of `kubernetes-sigs/controller-runtime` |
| `go.opentelemetry.io/otel/...` | `CHANGELOG.md` in `open-telemetry/opentelemetry-go` |
| `go.opentelemetry.io/contrib/...` | `CHANGELOG.md` in `open-telemetry/opentelemetry-go-contrib` |
| `github.com/dash0hq/dash0-api-client-go` | GitHub releases of `dash0hq/dash0-api-client-go` |
| others | GitHub releases or `CHANGELOG.md` of the repository behind the module path |

Code of both versions for the lookups: `go mod download -json <module>@<version>` (field `Dir`). Do not diff it.

## Lookups

Look up each of these in both versions of the tier A modules and compare them. Only look at the packages that appear
in the `go list -deps` output.

* Every function, method, type and option that the operator calls or sets (see the usage inventory): the
  signature, the documented behavior, the default values and the returned errors.
* Defaults that the operator relies on without setting them:
  * timeouts, retries, QPS and burst of the Kubernetes clients
  * the documented defaults of the cache, informer and leader election options
  * webhook server, metrics server, health probes (controller-runtime `pkg/manager`, `pkg/webhook`, `pkg/client`,
    `pkg/cache`)
* Environment variables that the operator or the platform sets for the binaries: whether they are still read
  (`os.Getenv`, `os.LookupEnv`), and how the values are parsed.
  * the `OTEL_*` variables of the OTel Go SDK, which configure the self-monitoring export of the operator's own
    binaries (see `images/pkg/common/otel.go`, and the env vars that
    `internal/selfmonitoringapiaccess/self_monitoring_and_api_access.go` sets)
  * the `KUBERNETES_*` variables of client-go
* Error semantics (`apierrors.IsNotFound`, conflicts), serialization (`sigs.k8s.io/yaml`, JSON field handling) and
  validation of the calls the operator makes.
* OTel Go: the semantic conventions version that the SDK resource uses, resource detection, and how the resource is
  merged with `resource.Environment()` (see the section on `OTEL_RESOURCE_ATTRIBUTES` in `CLAUDE.md`).
* Kubernetes API types that the operator writes, for example pod spec fields in `internal/workloads`. API versions
  that are deprecated or removed in the Kubernetes versions the operator supports are listed in the Kubernetes
  changelog.
* Functions that the version range newly marks `Deprecated:` and that the operator calls. CI already flags the call
  sites (staticcheck SA1019 in `make lint`). List them as deprecations in the report (step 7). If the replacement
  behaves differently, report that as an impact.

## Usage inventory

* For each changed module, find the packages the operator imports from it (`grep -rn '"<module path>'` over the
  module's `.go` files, excluding `_test.go`), then read the call sites.
* Places that configure library behavior:
  * `cmd/main.go`
  * `internal/startup/operator_manager_startup.go`: manager options, webhooks, metrics, leader election, caches
  * `internal/webhooks`
  * `internal/workloads`
  * `internal/util/cluster`: Kubernetes version handling
  * `images/pkg/common/otel.go`: OTel SDK setup of the operator's own binaries
* Pagination with the clientset and the client-go pager is described in `CLAUDE.md`. A change in
  `k8s.io/client-go/tools/pager` or in the continue-token handling affects `internal/instrumentation/instrumenter.go`
  and `internal/util/cluster/kubernetes_version.go`.

## kubectl in the agent0-connector

kubectl updates come from `.github/workflows/update-agent0-connector-kubectl.yaml`. They change the kubectl image in
`images/agent0-connector/Dockerfile` and the kubectl Go modules in `images/agent0-connector/src/go.mod`.

* This skill covers the functional side: changes in the output of the kubectl commands that the connector allows (see
  `supportedKubectlCommands`, `allowedSubcommandsPerKubectlCommand` and `knownOutputFormats` in
  `images/agent0-connector/src/kubectl/validation.go`, and `allowedFlags` in `kubectl.go`). This matters most for the
  `-o json` and `-o yaml` documents, which `redaction.go` parses and redacts.
* The security side, i.e. drift between the connector's argument parsing and the kubectl binary, is the subject of
  `/kubectl-parsing-drift-check`. Do not repeat that analysis. Say in the report that the other skill has to be run.

## Verification techniques

Kubernetes API behavior (validation, immutable fields, RBAC, events, error semantics) can be checked against envtest,
which runs a local API server without a cluster:

* Write a temporary test with the plain `testing` package in the head copy. It starts its own `envtest.Environment`
  like the `*_suite_test.go` files do, and calls the operator code or the library function in question. If it uses
  Gomega, it calls `RegisterTestingT(t)` first. Run only that test with `go test -run '^<test name>$'`, so that the
  Ginkgo suite of the package does not start.
* Set `UseExistingCluster: ptr.To(false)` in the `envtest.Environment`. Otherwise, a `USE_EXISTING_CLUSTER=true` in the
  environment connects the test to the cluster of the kubeconfig.
* The config that `testEnv.Start()` returns belongs to the `system:masters` group, which bypasses RBAC. For an RBAC
  check, create a user with `testEnv.AddUser`, bind only the rules in question to it, and use the config of that user.
* Set `KUBEBUILDER_ASSETS` to the directory of the envtest binaries. It takes precedence over the
  `BinaryAssetsDirectory` of the suites.
  * Use `bin/k8s/<EnvtestK8sVersion>-<GOOS>-<GOARCH>` of the working tree. `EnvtestK8sVersion` is in
    `test/util/constants.go` of the head copy. A head copy extracted from an archive has no `bin` directory.
  * If the working tree does not have that directory, install setup-envtest into the scratchpad with
    `GOBIN=<scratchpad>/bin go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest`, as the Makefile
    does for `bin`. Then run
    `<scratchpad>/bin/setup-envtest use <EnvtestK8sVersion> --bin-dir <scratchpad>/envtest -p path`, which downloads
    the binaries and prints the directory to use.
* To show a difference, run the same test in a full copy of the merge base (`git archive <merge base>`, or the source
  archive of the merge base commit) as well. If the adaptation changed the API that the test calls, write an
  equivalent test against the old API.
