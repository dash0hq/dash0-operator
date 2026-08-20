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

## Code Comments

When making changes, never add comments regarding the state before your changes - code comments are not a place for
history lessons.
The motiviation for a specific change can be part of the commit comment (if you have been asked to commit).
Add godoc comments for public functions.
Adding implementation comments in function bodies should be used very sparingly: If the code is understandable without a
comment, prefer to not add implementation comments at all.
You may use implementation comments to explain non-obvious aspects, but keep it as short as possible.

## Formatting

Use the current year in the license header comment when adding new files.
Do not update the copyright year when editing files.
Use lines of 120 characters when formatting files with line breaks.
Use 120 characters per line when formatting comments in Go code.

## Make Commands

Build, lint and test tasks in this repository are performed via the Makefile in the root of the repository.

- make build: build the Go code
- make lint: run all static code analysis checks (Go, Helm, shell scripts).
- make test: run all unit tests (Go, Helm chart unit tests)
    - To only run the Go unit tests, run `make go-unit-tests`.
    - To only run the Helm chart unit tests, run `make helm-unit-tests`.
- make images: build all container images used by the operator.

## Common Workflows

### Linting

After changing Go code, run `make golangci-lint`.
After changing the Helm chart, run `make helm-chart-lint`.
After changing or creating bash scripts, run `make shellcheck-lint` to verify they have no issues.

### Changing the Kubernetes Custom Resource Definitions

Start by making changes to the Go code in `api/operator`.
Then run `make manifests generate` to update the Kustomize source files in `config/crd`.
The resulting changes in the directory `config/crd` need to be carried over to the respective files in
`helm-chart/dash0-operator/templates/operator`, which are the Helm chart templates.

CRD specs must be fully typed with structured Go types (enums, nested objects, required/optional markers, and
kubebuilder validation annotations). Do not use opaque string fields to hold structured data. Validate the Go types
against the canonical OpenAPI spec in `https://github.com/dash0hq/dash0hq/dash0/tree/main/modules/openapi-types/internal/spec/` to ensure they match the Dash0 API
object schemas.

### Adding or changing a CRD: secret redaction in the agent0-connector

The agent0-connector executes read-only kubectl commands on behalf of an upstream agent and redacts the credentials of
Dash0 custom resources from the responses before they leave the cluster. It knows which resource types and which fields
hold credentials from hardcoded lists in `images/agent0-connector/src/kubectl/redaction.go`, which are a copy of
knowledge that actually lives in `api/operator`. CRD changes need to be checked against them:

- `dash0ResourceTypesWithSecrets` - the resource types whose content can contain a credential, in singular and plural
  form. A new CRD with a credential field has to be added here.
- `credentialFieldsPerConfigObject` - the fields that only hold a credential within a particular configuration object,
  keyed by the name of that object (e.g. `slackConfig` -> `webhookURL`). Generic field names such as `url` or `key` are
  credentials in one object and harmless in another, which is why they are keyed this way.
- `urlFieldsPerConfigObject` - the fields that hold a URL which is not a credential itself, but can contain one. Only
  sensitive parts of the URL are redacted. A URL that is a credential as a whole, such as a webhook URL, belongs in
  `credentialFieldsPerConfigObject` instead.
- The field names that are credentials wherever they occur are handled in `redactDocumentNodeRecursively`: `token`,
  `password`, and the header/query parameter values under `headers` and `queryParameters`.

Missing that step can be silent: the new credential is simply not matched, the response is still considered fully
redacted, and the credential is sent to the backend in plaintext. The test
`api/operator/credential_field_coverage_test.go` is a heuristic that protects against this drift, but there is no
guarantee that it will flag every change that would require updating the secret redaction. It matches field names
against a list of fragments (`token`, `key`, `header`, ...) and only looks at fields that hold a value directly - a
string, or a map of strings. It does not see a credential inside a list of name/value pairs, nor one in a free-text
field, so a new credential field whose name matches no fragment passes it unnoticed.

The same list also drives what the connector allows at all (`targetsResourceTypeWithSecrets`, used in
`images/agent0-connector/src/kubectl/validation.go`): for a resource type that can contain secrets, `kubectl describe`
and the output formats that can reshape a value (`-o go-template/jsonpath/custom-columns`, `--template`) are rejected,
because their output cannot be redacted. A credential-bearing CRD that is missing from the list therefore stays fully
readable through those formats as well.

When adding or changing a CRD, check all four lists above - grep for `dash0ResourceTypesWithSecrets`,
`credentialFieldsPerConfigObject` and `urlFieldsPerConfigObject`, and read the `case` clauses of
`redactDocumentNodeRecursively` for the field names that are credentials wherever they occur - and extend the fixtures
in `images/agent0-connector/src/kubectl/redaction_test.go` for any new credential field. Then run
`go test ./api/operator/... -run TestAgent0ConnectorRedactsEveryCredentialField` to check the CRDs against the lists.

Note which of the lists a field belongs in: a field listed in `credentialFieldsPerConfigObject` is redacted
unconditionally, while a header or query parameter value - including a query parameter of a URL listed in
`urlFieldsPerConfigObject` - is only redacted when it does not look like a well-known non-secret value (see
`wellKnownNonSecretValues`). A field that always holds a credential belongs in the former, even when it is a header -
which is why `incidentioConfig` lists `headers`.

### Adding a new reconciler with self-monitoring metrics

Any reconciler that implements `InitializeSelfMonitoringMetrics` (i.e. exposes counters or other OpenTelemetry
instruments) must be added to the `selfMonitoringClients` slice in `internal/startup/operator_manager_startup.go`.
Missing that step is silent: the metric handle stays `nil`, `Reconcile` skips the counter update via its nil-guard,
and the reconciler emits nothing for the entire process lifetime — no build error, no test failure, no runtime
warning. When wiring a new reconciler, grep for `selfMonitoringClients` and add the new entry before opening the PR.
