# CLAUDE.md

This file contains the Claude development guidelines for the dash0-operator repository.

## Planning and Execution

- You start making a plan without making any further code changes.
- You ask clarifying questions about the task.
- You then confirm the plan, and once confirmed, you start executing on it.

## Code Organization

The `api` directory contains the Go code from which the custom resource definitions for the Dash0 operator
(Dash0OperatorConfiguration, Dash0Monitoring) are generated.
The `config` directory contains the Kustomize sources, which are generated from the Go code in `api`. The Kustomize
sources are not used, except as an intermediate stage for the Helm chart.
The `helm-chart/dash0-operator` directory contains the operator's Helm chart.
The `images` directory contains source code for auxiliary images, like the custom OpenTelemetry collector image and the
instrumentation image.
The `internal` repository contains the Go code for the main operator manager image (also referred to as
operator-controller). This is where most of the operator's logic resides.
The directory `internal/collectors/otelcolresources` contains the files daemonset.config.yaml.template and
internal/collectors/otelcolresources/deployment.config.yaml.template, which are the templates for the OpenTelemetry
collectors the operator manages.
The directory `test/util` contains additional Go code only used in unit tests.
The directory `test/e2e` contains the end-to-end test suite.
The directory `test-resources` contains a collection of scripts for running semi-manual tests scenarios.

## Make Commands

Build, lint and test tasks in this repository are performed via the Makefile in the root of the repository.

- make build: build the Go code
- make lint: run all static code analysis checks (Go, Helm, shell scripts, Zig).
- make test: run all unit tests (Go, Zig, Helm chart unit tests)
- make images: build all container images used by the operator.

## Common Workflows

### Linting

After changing Go code, run `make golangci-lint`.
After changing the Helm chart, run `make helm-chart-lint`.
After changing Zig code, run `make zig-fmt-check`.
After changing or creating bash scripts, run `make shellcheck-lint` to verify they have no issues.

### Changing the Kubernetes Custom Resource Definitions

Start by making changes to the Go code in `api/operator`.
Then run `make manifests generate` to update the Kustomize source files in `config/crd`.
The resulting changes in the directory `config/crd` need to be carried over to the respective files in
`helm-chart/dash0-operator/templates/operator`, which are the Helm chart templates.
