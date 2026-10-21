# Collector components and the target-allocator

This file applies when the update changes one of:

* `images/collector/src/builder/config.yaml`: the collector components, `dist.version`, and the version of the builder
  (`images/collector/Dockerfile` installs the builder in the version of the first connector's `gomod`).
* `images/collector/src/telemetry/go.mod`: the custom internal-telemetry factory that is built into the collector.
* Collector modules in the root `go.mod` (`go.opentelemetry.io/collector/...`,
  `github.com/open-telemetry/opentelemetry-collector-contrib/...`). These are compiled into the operator manager, which
  uses them to validate the filters and transforms that customers configure.
* `images/target-allocator/Dockerfile`: a re-tag of `ghcr.io/open-telemetry/opentelemetry-operator/target-allocator`.

The Signal Control collector (`signalcontrol.config.yaml.template`) is out of scope. It runs a different image. It does
render `exporters.partial.yaml.template`, so a proposed fix in that partial changes its config as well.

## Upstream sources

* Repositories:
  * Core: `open-telemetry/opentelemetry-collector` (modules `go.opentelemetry.io/collector/...`). It has two version
    lines: `v1.x` for stable modules and `v0.x` for the others.
  * Contrib: `open-telemetry/opentelemetry-collector-contrib` (modules
    `github.com/open-telemetry/opentelemetry-collector-contrib/...`). Contrib components that have reached 1.0 have
    their own `v1.x` version.
* Changelogs: `CHANGELOG.md` (user-facing changes) and `CHANGELOG-API.md` (Go API changes), in both repositories.
  * Read them at the latest release tag (`https://raw.githubusercontent.com/<owner>/<repo>/v0.X.Y/CHANGELOG.md`). They
    also have the sections of the releases newer than the new version (step 3 of SKILL.md).
  * Read the section of every release in the range, not only the newest one.
  * Each release has its own section, headed `## v0.X.Y` or `## v1.A.B/v0.X.Y`. Match the `v0.X.Y` part. Entries are
    grouped into breaking changes, deprecations, new components, enhancements and bug fixes, and each entry names the
    component.
* Every component documents its stability, feature gates, metrics and resource attributes in `metadata.yaml` and in the
  generated `documentation.md` in its module directory.

## Determine the changed modules

The builder config lists only the components. The collector also links the modules these components depend on, and a
change in one of them (for example `pkg/ottl`, `pkg/stanza`, `internal/k8sconfig`, Prometheus or client-go) changes
the behavior of the collector just as well. Determine the full list like this, once for the old and once for the new
state of the PR:

1. Recreate the layout of the `builder` stage of `images/collector/Dockerfile` in
   `<scratchpad>/<old|new>/src`.
   * Put the builder config at `src/builder/config.yaml`.
   * Copy `images/collector/src/telemetry` to `src/telemetry` and `images/pkg/nodeuid` to `src/pkg/nodeuid`, if they
     exist in that state.
   * The relative paths in the builder config (`replaces`, the telemetry `path`) only resolve in this layout.
   * Take the files of the old state from the merge base, and those of the new state from the head copy (both in step 1
     of SKILL.md).
2. Install the builder version of that state into a separate directory:
   `GOBIN=<scratchpad>/bin-<old|new> go install go.opentelemetry.io/collector/cmd/builder@v<version>`.
3. Run the builder with `src` as the working directory, in one command:
   `cd <scratchpad>/<old|new>/src && <scratchpad>/bin-<old|new>/builder --config=builder/config.yaml --skip-compilation`.
   The working directory of the shell is not guaranteed to persist between commands.
   * This generates the main module in `src/dist` and runs `go mod tidy` on it, but does not compile it.
   * It takes about a minute, plus the time to download the modules.
4. For both states, write the linked modules to a file:
   `GOOS=linux CGO_ENABLED=0 go -C <scratchpad>/<old|new>/src/dist list -deps -f '{{with .Module}}{{.Path}} {{.Version}}{{end}}' . | sort -u > <scratchpad>/mods-<old|new>.txt`.
   Compare the two files with `diff`. Do not use `join`: it expects a different sort order. The modules whose version
   differs are the ones to analyze. To find out why a module is linked, for example one that only the new state links,
   run `go -C <scratchpad>/new/src/dist mod why -m <module>`.
5. Build the collector binary of both states once:
   `CGO_ENABLED=0 go -C <scratchpad>/<old|new>/src/dist build -o <scratchpad>/otelcol-<old|new> .`. This takes about a
   minute per state with a warm module cache, and several minutes otherwise. Step 6 uses these binaries (see
   "Verification techniques"). Skip this step if the "Collector Configuration Validation" CI job passed for the head
   sha, unless a hit needs the binaries for its confirmation.

The generated `src/dist` modules are also a ready-made place for the scratch tests of step 6: they require exactly the
module versions that the collector is built with.

Tier assignment within the collector:

* Tier A: the modules of the components in the builder config, the contrib and core modules that they use
  (`pkg/ottl`, `pkg/stanza`, `internal/k8sconfig`, `internal/kubelet`, `pkg/translator/prometheus`, the core
  `config/...` modules, `exporter/exporterhelper`, `service`, `otelconf`, `confmap` and its providers, `featuregate`),
  and the target-allocator.
* Tier B: third-party modules (Prometheus, client-go, the OTel Go SDK, gRPC, ...). No release notes (step 2 of
  SKILL.md), only the Prometheus scrape config lookup below. The Prometheus module version `v0.3XX.Y` belongs to
  Prometheus `v3.XX.Y`, for example `v0.314.0` to `v3.14.0`.

## Diff set

First list the changed files of every tier A module except the target-allocator, which has its own section:
`diff -rq -x go.mod -x go.sum -x '*_test.go' -x testdata <old dir> <new dir>`. Do not read all of them. Read the diff
(`diff -u`) of exactly these changed files, also when the changelog says nothing about them:

* `metadata.yaml` and `factory.go` of every component in the builder config.
* Every Go file that declares config fields (`mapstructure:"` tags) of a config section that the operator renders, or
  that customers set through the operator (filters and transforms). This includes nested configs and configs in
  shared modules, for example `transformprocessor/internal/common/config.go`, the configs of the detectors that the
  templates list, the configs of the `pkg/stanza` operators that the filelog receiver config uses, and
  `config/confighttp`.
* Every file with an `Unmarshal` method for config types: `grep -rln 'Unmarshal(.*confmap.Conf' <module dir>`.
* For OTTL: `grammar.go` and `parser.go` of `pkg/ottl` (the syntax), and `ottlfuncs/functions.go` (the list of standard
  functions). Customer filters and transforms can use any syntax and any function, so changes there affect customers
  even if the operator's own statements do not use them.

New files (`Only in <new dir>`) that fall into this set are read in full. For a new directory, `diff -rq` reports only
the directory, so list its files. The lookups below and the release notes cover all other changed files. Ignore
changes that the `mdatagen` code generator makes to `metadata.yaml` without changing gates, metrics, attributes or
stability (for example `skip_strict_validation`).

What to look for in the diff set:

* `metadata.yaml`:
  * feature gates: id, stage, `from_version`, `to_version`
  * the `enabled` default of every metric and resource attribute
  * metric names, units and types
  * stability levels, and deprecated type names (aliases)
* Config files: removed or renamed fields (the `mapstructure` tags), fields marked as deprecated, and `Validate`.
* `factory.go`: `createDefaultConfig` (the default of every key that the operator does not set explicitly), the
  component type name, and aliases.
* A new `Unmarshal` method on a config type that other configs embed with `squash`. `confmap` then decodes the
  embedding config without rejecting unknown keys, including its nested sections. Nested sections with an `Unmarshal`
  method of their own, for example `configoptional.Optional`, are decoded by that method and can still reject unknown
  keys. From that version on, a key that upstream removes there and that the operator still renders is ignored
  without an error. It is no longer reported at startup or by `otelcol validate`. Since core v0.160.0, this applies to
  `ClientConfig` and `ServerConfig` of `confighttp`, which affects for example:
  * the `protocols.http` section of the otlp receiver
  * the `otlp_http` exporters
  * the resourcedetection processor, including the `resource_attributes` of every detector
  * the `target_allocator` section of the prometheus receiver

## Lookups

For part 2 of step 5 of SKILL.md, look up the definition of every item of the usage inventory in the code of both
versions. This includes the Go API that the webhooks and `images/collector/src/telemetry` call, the OTTL context
inference that the mutating webhook relies on, and the SIGHUP reload that `images/configreloader` relies on. Changes
outside of the diff set and of these lookups are checked only through the release notes. In particular:

* Every config key that the operator renders, including keys in conditional template branches: does the field with that
  `mapstructure` tag still exist, and is its value used the same way?
* Every value that the operator generates for a key, such as endpoints, header values, regular expressions and
  durations: how does the new version parse and validate it? Find the code that reads the field.
* Every feature gate that the operator passes: its stage in both versions (`metadata.yaml`,
  `generated_feature_gates.go` or the `MustRegister` call).
* Every metric and attribute that the operator references:
  * the names in `metrics.<name>.enabled` toggles, filters, routing conditions and transform statements
  * the names in `extract.metadata`

  Check that each one is still registered, with the same name and type. For `extract.metadata`, also check how the
  attribute is emitted. A feature gate can suppress an attribute that is still listed as valid.
* Every OTTL function and path that the operator's own statements and the OTTL examples of the chart documentation
  use: the implementation in `pkg/ottl/ottlfuncs/func_*.go` and in the contexts in `pkg/ottl/contexts/...`.
* Every resourcedetection detector that the templates list: its config (`config.go`) and the resource attributes it
  emits (`metadata.yaml`), in the directory of the detector under `internal/`. Some are nested, for example
  `internal/aws/eks` and `internal/azure/aks`, and `k8s_api` is in `internal/k8sapi`.
* The `service.telemetry` section that the operator renders: the `otelconf` schema and the code that creates the
  exporters from it.
* Every Prometheus scrape config field that the operator renders: its YAML tag, `omitempty` and `MarshalYAML` in the
  Prometheus packages `config`, `model/relabel` and `discovery/kubernetes`. The prometheus receiver marshals the scrape
  configs again and passes them to Prometheus.
* The format of the filelog checkpoints and the file layout of the `file_storage` extension. `filelogoffsetsync`
  backs up and restores that directory as it is, so a collector of the new version has to read what the old version
  wrote. The checkpoints are written by `pkg/stanza/fileconsumer/internal/checkpoint`. They serialize `Metadata` of
  `pkg/stanza/fileconsumer/internal/reader/reader.go`, in an encoding that a feature gate selects.

## Usage inventory

Read these in the head copy:

* The collector configs. Record:
  * the components, every config key and value
  * every OTTL statement and condition (transform, filter, routing)
  * the `extract.metadata` lists of `k8s_attributes`
  * the metrics and resource attributes that are enabled or disabled explicitly

  Cover every branch of the templates, not only what a default installation renders. For each key that only some
  configurations contain, record the Helm value or operator configuration setting behind it. A key that only an opt-in
  setting renders crashes exactly the installations that use that setting when upstream removes it.
  * If the head copy has `collectorConfigMatrix` (`collector_configs_matrix_test.go`), render the matrix (see
    "Verification techniques") and take the inventory from the rendered files. The matrix has a minimal and a maximal
    baseline, plus, for every setting that the templates branch on, one variant per baseline with only that setting
    flipped. The files are named `<baseline>__<collector>.yaml` and
    `<baseline>__<setting>-<enabled|disabled>__<collector>.yaml`. A key that only some files contain depends on the
    setting in their names. `matrixSettings()` maps the setting to the field of the operator configuration. Skip the
    `*__signalcontrol.yaml` files: the Signal Control collector is out of scope, and the exporters it shares with the
    other collectors are in their files as well.
  * Otherwise, read the templates in `internal/collectors/otelcolresources`: `daemonset.config.yaml.template`,
    `deployment.config.yaml.template` and `exporters.partial.yaml.template`. For each key inside a `{{ if ... }}`
    block, record the condition.
* Go code in the same package that renders or extends the configs:
  * `collector_config_maps.go`: template values, the generated namespace filter OTTL, the namespace scoping of
    customer filters and transforms, and error modes.
  * `exporter.go`: exporter type names, headers, balancer and encoding.
  * `kubeletstats_endpoint_probe.go`.
* `internal/selfmonitoringapiaccess/self_monitoring_and_api_access.go`: the rendered `service.telemetry` section.
* The collector workloads in `desired_state.go`:
  * the args, including `--feature-gates`
  * env vars, ports, the health check probe path and port, volumes and host path mounts, `shareProcessNamespace`
  * the RBAC rules of both collectors
  * the constraints of the GKE Autopilot allowlist, which pins the args of some collectors
* `images/collector/src/image/entrypoint.sh`: the env vars that it turns into feature gates, and the PID file.
* Companions that rely on collector behavior:
  * `images/configreloader`: reloads the config with SIGHUP and reads the PID file.
  * `images/filelogoffsetsync`: depends on the file layout of the `file_storage` extension directory.
* Uses of the collector modules from the root `go.mod`:
  * `internal/webhooks/monitoring_validation_webhook.go` and `operator_configuration_validation_webhook.go` validate
    customer OTTL, filter and transform configs through the processor factories and `confmap`.
  * `monitoring_mutating_webhook.go` normalizes transforms and relies on context inference.
  * `api/operator/common/common_monitoring_types.go` mirrors processor config fields and error modes.
* The OTTL examples in `helm-chart/dash0-operator/docs/configuration.md` and `auto-instrumentation.md`, which customers
  copy into their filters and transforms.
* `images/collector/src/telemetry`, if present: wraps `otelconftelemetry` and uses the factory options of
  `service/telemetry`.
* Expectations elsewhere:
  * `test/e2e/otlp-sink/telemetrymatcher/*_receiver_metrics.txt` lists the expected metric names. They were copied
    from the upstream `metadata.yaml` files, so a renamed or newly enabled metric has to be reflected there.
  * the matchers in `test/e2e/otlp-sink/telemetrymatcher/*.go`
* Existing coverage, so that the report can say whether CI already catches a finding:
  * `collector_configs_validation_test.go`, if present in the head copy: runs `otelcol validate` for every rendered
    config in CI.
    * It catches unknown components and feature gates, OTTL parse errors, and unknown keys.
    * It does not catch unknown keys in configs that embed a type with its own `Unmarshal` method (see "Diff set"), and
      it does not catch semantic changes.
  * `collector_config_maps_test.go`: executes some OTTL in-process, with the root `go.mod` version of `pkg/ottl`.
  * `desired_state_test.go`: feature gates, and drift between `desired_state.go` and `entrypoint.sh`.
  * The webhook tests assert exact OTTL parser error messages.

## Version consistency

Report a mismatch in any of the following pairs, together with its consequence, if the update creates or widens it.
A mismatch that exists unchanged in the merge base is a pre-existing issue (step 5 of SKILL.md). The pairs:

* Collector modules in the root `go.mod` vs. the builder config. The webhooks validate customer configs with the root
  `go.mod` versions, while the collector runs the builder versions. If the range between them contains OTTL grammar or
  function changes, the webhook can accept configs that the collector rejects, or reject configs that it would accept.
* `images/collector/src/telemetry/go.mod`, if present, vs. the builder config. The collector components update
  workflow aligns them, but Dependabot does not cover that module.
* The target-allocator vs. the target-allocator client in the prometheus receiver.

## Target-allocator

* Upstream: `open-telemetry/opentelemetry-operator`, directory `cmd/otel-allocator`, and the target-allocator entries in
  its `CHANGELOG.md`.
* Usage inventory:
  * `internal/targetallocator/taresources/desired_state.go`:
    * the rendered config file (allocation and filter strategy, collector selector, `prometheus_cr` selectors, HTTPS
      settings)
    * the config file path that the image reads by default (the operator passes no args)
    * ports, env vars, the `/livez` and `/readyz` probes, and the ClusterRole
  * In `daemonset.config.yaml.template`: the `target_allocator` section of the prometheus receiver and the scrape
    config for the target-allocator's own metrics.
* Lookups:
  * every rendered config key, and its default
  * the default config file path
  * the HTTP endpoints that the receiver's client calls, and the probe endpoints

## Verification techniques

* **Rendered configs.** Write a temporary plain `testing.T` test in `internal/collectors/otelcolresources` of the head
  copy. It calls `RegisterTestingT(t)` first, as `collector_configs_validation_test.go` does, and then
  `renderCollectorConfigurationMatrix` with `DASH0_COLLECTOR_CONFIG_OUTPUT_DIR` set to the scratchpad.
  * That function writes only the YAML. Also write the `featureGates` of every returned config to a file, because the
    "Whole configs" check needs them.
  * Run only this test (`go test -run '^<test name>$'`). The Ginkgo suite of the package, whose `BeforeSuite` starts
    envtest, does not run then, and no docker is needed.
  * The matrix covers both branches of every template condition.
  * If the head copy predates that function, call `assembleDaemonSetCollectorConfigMap` and
    `assembleDeploymentCollectorConfigMap` (in `collector_config_maps.go`) with the settings that switch the relevant
    template branches, and write the YAML out.
* **Which configs.** Check the configs rendered in the head copy with both collector versions. A difference between
  them comes from the update. If the PR also changes the templates or the Go code that renders them, check the configs
  of the merge base with the old version as well. Comparing these with the head configs and the new version shows the
  effect of the whole PR.
* **CI first**: the "Collector Configuration Validation" CI job runs `otelcol validate` on every rendered config of
  the head in a collector image built from the head. If it passed for the head sha, skip "Primary check" and "Whole
  configs", unless a hit needs them for its confirmation. Neither catches the keys that a `squash`-embedded `Unmarshal`
  method ignores (see "Diff set").
* **Primary check**: the config acceptance test (see "Scratch tests") for every component of every rendered config, in
  both states. It checks each component on its own, so that the error of one component does not hide the others.
  `otelcol validate` below checks the pipelines and the feature gates in addition, but it stops at the first error.
* **Whole configs**: run `<scratchpad>/otelcol-<old|new> validate --config=<file> --feature-gates=<gates>` with the
  binaries of "Determine the changed modules", for every rendered config except `*__signalcontrol.yaml` (they use
  components of a different image), with the feature gates of that config.
  * Set the environment as `collectorConfigurationEnvironment` in `collector_configs_validation_test.go` does: a
    placeholder for every variable that the config references (`${env:...}`) or names (`*_from_env_var` keys),
    `K8S_POD_IP=127.0.0.1`, and the variables of `clusterEnvironment`. If the head copy predates that function, take
    the variable names from the rendered configs.
  * Outside a cluster, the validation also fails on environment issues: directories that are volume mounts in the pod
    (for example `/hostfs` and the `file_storage` directory), and the service account files that the resourcedetection
    processor and the kubeletstats receiver read. Only these environment errors can be ignored. Any other error that
    occurs identically in both states is a candidate for a pre-existing issue (step 5 of SKILL.md).
  * The validation stops at the first error. If one state fails earlier than the other, its later errors stay hidden.
    To check the remaining components, point the directories to the scratchpad and remove the components that need
    the service account in a copy of the config. The config acceptance test covers the removed components.
* **Scratch tests** in the generated `src/dist` module of the old and of the new state (see above), with the same test
  code in both:
  * **Config acceptance**: unmarshal the rendered component config with `confmap` into the config struct of the
    component factory, then call `confmap.Validate` (`xconfmap.Validate` if the `confmap` version of that state has
    no `Validate`). Some `Validate` methods read env vars, for example the `node_from_env_var` check of
    `k8s_attributes`. Set the same environment as for "Whole configs", and replace the `${env:...}` references in the
    YAML with the values of that environment before unmarshaling.
  * **OTTL semantics**: parse and execute the operator's statements on crafted input. If the head copy has the tests
    "should derive the service attributes of scraped metrics" and "should let self-monitoring metrics pass through the
    OTTL namespace filter" in `collector_config_maps_test.go`, use them as a template for setting up the parsers.
  * **Processor behavior**: create the processor through its factory with the rendered config, push crafted input
    through it into a `consumertest` sink, and compare the output of both states. Attributes that are created with
    `FromRaw` from a Go map have a random order, so sort them before comparing.
* **Feature gates.** Compare the stage of each gate in `metadata.yaml` (or in its `MustRegister` call) between both
  versions. `featuregate.Registry.Set` rejects three things, and a rejected `--feature-gates` value makes the collector
  refuse to start:
  * disabling a stable gate (`-<gate>`)
  * enabling a deprecated gate
  * any gate id that no longer exists

  A stage change therefore matters for the gates that the operator disables and for the ones it enables. Between alpha
  and beta, the default flips. That matters for all gates the operator does not set at all.
