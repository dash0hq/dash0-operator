# Auto-instrumentation components

This file applies when the update changes one of these pins:

| Component | Pin | Updated by | Upstream |
|---|---|---|---|
| OpenTelemetry injector | `images/instrumentation/opentelemetry-injector/version` | `update-opentelemetry-injector.yaml` | `open-telemetry/opentelemetry-injector` |
| OpenTelemetry Java agent | `images/instrumentation/jvm/pom.xml` | Dependabot (maven) | `open-telemetry/opentelemetry-java-instrumentation` |
| Dash0 Node.js distribution | `images/instrumentation/node.js/package.json` and `package-lock.json` | Dependabot (npm) | npm `@dash0/opentelemetry` (`@dash0hq/opentelemetry` before July 2025), `dash0hq/opentelemetry-js-distribution`; transitive `@opentelemetry/*` packages from `open-telemetry/opentelemetry-js` and `opentelemetry-js-contrib` |
| Dash0 .NET distribution | `images/instrumentation/dotnet/dash0-dotnet-distribution-version` | `update-dotnet-distribution.yaml` | `dash0hq/opentelemetry-dotnet-distribution` |
| Dash0 Python distribution | `images/instrumentation/python/dash0-python-distribution-version` | `update-python-distribution.yaml` | `dash0hq/opentelemetry-python-distribution` |
| Dash0 Ruby distribution | `images/instrumentation/ruby/dash0-ruby-distribution-version` | `update-ruby-distribution.yaml` | `dash0hq/opentelemetry-ruby-distribution` |

The `update.sh` scripts of the four version-file pins take the latest release, including pre-releases.

## Node.js packages

The Node.js distribution delegates most of its behavior to the OpenTelemetry JS packages. For an update of the
distribution, these transitive packages are tier A: `@opentelemetry/sdk-node`,
`@opentelemetry/auto-instrumentations-node`, `@opentelemetry/instrumentation-http`, `@opentelemetry/resources` and the
OTLP exporters. The other transitive packages are tier B.

A distribution update can change dozens of these packages and add new ones. Only the release notes of the tier A
packages are read (step 2 of SKILL.md). They are in two monorepos:

* `open-telemetry/opentelemetry-js`: the stable packages in `CHANGELOG.md`, the experimental ones in
  `experimental/CHANGELOG.md`
* `open-telemetry/opentelemetry-js-contrib`: one `CHANGELOG.md` per package

Download the archive of each repository once, at the latest release tag: `experimental/v<sdk-node version>` in
`opentelemetry-js`, and `auto-instrumentations-node-v<version>` in `opentelemetry-js-contrib`. Extract the sections of
the version range, and of the newer releases (step 3 of SKILL.md), from the changelog of each tier A package with a
script, and skip the entries that only update dependencies.

## Python and Ruby packages

The PR of a Python or Ruby distribution update only changes the git ref in the version file. The Dockerfile builds
from the source archive of that ref, and the distribution pins the OpenTelemetry packages there:

* Python: the `pyproject.toml` files under `packages/`. The one of `packages/dash0-opentelemetry` pins the whole
  dependency tree with exact versions.
* Ruby: `packaging/Gemfile.lock`

Compare these files between the old and the new ref, and treat the changed and added packages like the Node.js ones:
the SDK and the instrumentation base package are tier A, the individual instrumentations are tier B. The release notes
of the tier A packages are in `open-telemetry/opentelemetry-python` and `opentelemetry-python-contrib`, and in
`open-telemetry/opentelemetry-ruby` and `opentelemetry-ruby-contrib`.

## Usage inventory

The operator does not start the agents itself. It sets `LD_PRELOAD` to the injector. The injector reads its config file
and the `OTEL_INJECTOR_*` variables, and sets the runtime-specific variables at process start (`JAVA_TOOL_OPTIONS`,
`NODE_OPTIONS`, `PYTHONPATH`, `RUBYOPT`, `CORECLR_*`, `DOTNET_STARTUP_HOOKS`, `OTEL_DOTNET_AUTO_HOME`, ...).
Every hand-over between these pieces is an interface:

* `internal/workloads/workload_modifier.go`:
  * the env vars the operator sets: `LD_PRELOAD`, `OTEL_INJECTOR_*`, `OTEL_EXPORTER_OTLP_ENDPOINT`,
    `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_PROPAGATORS`, `OTEL_LOGS_EXPORTER`, `DASH0_OTEL_COLLECTOR_BASE_URL`,
    `DASH0_NODE_IP`, and the runtime-specific flags such as the SQL query parameter capture flags
  * the init container, volume and mount paths
  * the `subPath` for image volume delivery, which makes the `/dash0-instrumentation` layout of the instrumentation
    image an interface as well
* `images/instrumentation/opentelemetry-injector/injector*.conf`: the config keys, the agent paths, and
  `auto_instrumentation_disabled`.
* `images/instrumentation/Dockerfile`:
  * release asset names and digest verification
  * the source layout that the build stages assume (the paths they copy or run)
  * post-processing (deleted files and directories)
  * the destination layout under `/dash0-instrumentation`
* Build scripts: `images/instrumentation/dotnet/download-instrumentation.sh`,
  `images/instrumentation/python/extract-flattened-requirements.sh`, `images/instrumentation/ruby/install-bundle.sh`.
* Documented behavior and supported runtime versions: `helm-chart/dash0-operator/docs/auto-instrumentation.md` and
  `helm-chart/dash0-operator/README.md`.
* Test expectations, which also show which behavior the operator relies on:
  * `images/instrumentation/test/<runtime>/test-cases/*`: env var formats, the composition of
    `OTEL_RESOURCE_ATTRIBUTES`, agent paths, and checks that the distribution was loaded
  * `test/e2e/otlp-sink/telemetrymatcher/spans.go` and `logs.go`: span kinds, the HTTP span attributes and the resource
    attributes the e2e tests require

## Lookups

Check every item of the usage inventory against the new version. Every path, entry point, asset name, env var and
config key that the operator, the Dockerfile, the build scripts, the injector configuration or the tests reference has
to exist in the new artifact and mean the same as before.

* **Injector:**
  * the env vars and config keys that the operator sets and that `injector*.conf` uses: still read, same meaning
  * how it composes the runtime-specific variables
  * the directory suffixes it appends to the agent path prefixes (libc flavor, architecture)
  * how it builds `OTEL_RESOURCE_ATTRIBUTES` from the `OTEL_INJECTOR_*` variables
  * libc detection
  * release asset names and the digest field that the Dockerfile verifies
* **Java agent:**
  * the `OTEL_INSTRUMENTATION_*` settings that the operator sets: still read, same meaning
  * which instrumentations are enabled by default
  * semantic convention defaults (renamed span and metric attributes)
  * exporter defaults (protocol, endpoint)
  * the minimum Java version
  * the bundled OpenTelemetry Java SDK version and resource providers
* **Node.js distribution:**
  * the package entry point, and every path under `dist` that the Dockerfile and the tests reference
  * the `exports` map and the `esm`/`esnext` directories that the Dockerfile deletes
  * the `DASH0_*` variables it reads
  * the minimum Node.js version (`engines`)
  * the versions of the transitive `@opentelemetry/*` packages (from the `package-lock.json` change of the PR), and the
    packages that the update adds
  * in the tier A packages: how they read the `OTEL_*` variables that the operator sets (for example
    `OTEL_PROPAGATORS`), which instrumentations are enabled by default, and the span and resource attributes that the
    tests check
* **.NET distribution:**
  * release asset names, and the paths in the archive that the Dockerfile, the build script and the injector reference
    (native profiler, startup hook)
  * the `OTEL_DOTNET_*` settings that the operator sets (the SQL query parameter capture flags): still read, same
    meaning
  * the env vars that the injector sets for it
  * the minimum .NET version
* **Python distribution:**
  * the source paths the Dockerfile builds from: the packages it installs, the `sitecustomize.py` it copies
  * dependency pins that can conflict with applications (for example `protobuf`, `opentelemetry-api`)
  * the minimum Python version, and the self-deactivation behavior
* **Ruby distribution:**
  * the gemspec, `packaging/Gemfile` and `Gemfile.lock`
  * the entry point that `install-bundle.sh` links
  * native extensions
  * the minimum Ruby version

## Verification techniques

* Download the artifact of the new version into the scratchpad, and the old one where a lookup needs the comparison.
  List its files (`tar -tzf`, `unzip -l`) and check the referenced paths against the list. Search it for the env var
  and config key names. Do not diff the artifacts as a whole. Sources:
  * release assets and source tarballs from GitHub
  * `npm pack <package>@<version> --pack-destination <scratchpad>`
  * the Java agent jar from Maven Central: list the bundled instrumentation modules and read the default configuration
* For native binaries such as `libotelinject.so`, check the env var and config key names with `strings`.
* **Node.js distribution:** install the head's `package.json` and `package-lock.json` in the scratchpad with the flags
  of `images/instrumentation/Dockerfile` (`npm ci --ignore-scripts --omit=dev`), and delete the `esm` and `esnext`
  directories with the same `find` command as the Dockerfile. Then load the distribution with
  `node --require <scratchpad>/node_modules/<package>` into a small script, with the env vars that the operator sets.
  Point the endpoints to an address that nothing listens on, for example `http://127.0.0.1:1`.
  Check the output for initialization errors, the loaded files (`require.cache`) and whether the global tracer provider
  is a real one. Do the same for the base state if a difference needs confirmation.
* The behavior tests of the instrumentation image need docker. CI runs them (`instrumentation-image-tests.yaml`) when
  `images/instrumentation` changes. Read their result instead of running them (step 6 of SKILL.md).
