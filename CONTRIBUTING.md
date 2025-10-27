Contributing
============

## Prerequisites
- Go (version >= v1.25)
- Docker
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.
- [helm](https://helm.sh/docs/intro/install/)
- [helm unittest plug-in](https://github.com/helm-unittest/helm-unittest/tree/main)
- Zig (with the version referenced in the [zig-version file](images/instrumentation/injector/zig-version), see
  [Zig download](https://ziglang.org/download/) or [Zig installation instructions](https://github.com/ziglang/zig/wiki/Install-Zig-from-a-Package-Manager).

## Make Targets

Run `make help` for more information on all potential `make` targets.
More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Install Husky Hooks

When working on the operator code base with the intent of contributing changes, it is recommended to install the 
pre-push git hooks via `make husky-setup-hooks`.
This will install a git pre-push hook that runs `make build`, `make lint` and `make test` before every `git push`,
thereby catching simple syntax, formatting or unit test issues before pushing changes.
Using this facility is optional, all checks will be run in CI anyway, the intent of the pre-push hooks is simply to
reduce the turnaround time.

## Deploying to a Local Cluster for Testing Purposes

This approach is suitable for deploying the operator to a cluster running locally on your machine, for example
via the Kubernetes support included in Docker Desktop.

As an alternative to the steps outlined in this section, there is also a suite of scripts available that take care of
building and deploying everything required for a full local test, including the Dash0OperatorConfiguration resource, 
Dash0Monitoring resources, test workloads, third-party CRDs etc.)
See section [Semi-Manual Test Scenarios](#semi-manual-test-scenarios) for more information on that.

In contrast to the semi-manual test scenarios, this section only describes the individual steps to build and deploy a
bare-bones Dash0 operator.

1. Run `make images` to build all required container images locally, this will tag the image as
`operator-controller:latest`, `collector:latest` etc.
2. Run `make deploy` to deploy the operator with locally built images to the cluster.
   Note: No Dash0OperatorConfiguration or Dash0Monitoring resource will be created automatically, so the operator will
   not really do anything.
* Alternatively, deploy with images from a remote registry:
  ```
  make deploy \
    IMAGE_REPOSITORY_PREFIX=ghcr.io/dash0hq/
    IMAGE_TAG=main-dev \
    PULL_POLICY=""
  ```

**NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as
admin.

**Undeploy the controller from the cluster:**

```sh
make undeploy
```

This will also remove the custom resource definition.

## Run Tests

```
make test
```

This will run the go unit tests as well as the helm chart tests.

### Semi-Manual Test Scenarios

The steps described in the section
[Deploying to a Local Cluster for Testing Purposes](#deploying-to-a-local-cluster-for-testing-purposes) can be used
as a basis for executing manual tests, but they would require a lot of repetitive manual steps to create the full
test scenario (deploy the operator, create a test namespace, deploy a Dash0Monitoring resource, deploy a test workload,
etc.)

On the other hand, the [end-to-end tests](#end-to-end-tests) automate all of that, but might sometimes not be the best
tool to troubleshoot the operator or experiment with new functionality, simply because the end-to-end tests remove
everything they deploy in their `AfterAll`/`AfterEach` hooks.

The scripts in `test-resources/bin` can be used for creating a specific scenario with one shell command, and then
inspecting its behavior.
They are also useful to actually report data to an actual Dash0 backend.

If you haven't created `test-resources/.env` yet, you can copy the file `test-resources/.env.template` to
`test-resources/.env`, or set the environment variables listed in `test-resources/.env.template` via other means.
Make sure the comma-separated list `ALLOWED_KUBECTXS` contains the name of the kubernetes you want to use for the test
scripts.
If you want to report telemetry to a Dash0 backend, make sure that `DASH0_INGRESS_ENDPOINT` and
`DASH0_AUTHORIZATION_TOKEN` are set correctly.
Alternatively, instead of providing `test-resources/.env`, make sure that the environment variables mentioned in
`test-resources/.env.template` are set via other means.
Use `kubectx` or `kubectl config set-context` to switch to the desired Kubernetes context before running the scripts.

* `test-resources/bin/test-scenario-01-aum-operator-cr.sh`: Deploys an application under monitoring (this is
  abbreviated to "aum" in the name of the script) to the namespace `test-namespace`, then it deploys the operator to
  the namespace `operator-namespace`, and finally it deploys the Dash0 monitoring resource to `test-namespace`. This is a test
  scenario for instrumenting _existing_ workloads via the controller's reconcile loop.
* `test-resources/bin/test-scenario-02-operator-cr-aum.sh`: Deploys the operator to `operator-namespace`, then the
  Dash0 monitoring resource to namespace `test-namespace`, and finally an application under monitoring to the namespace
  `test-namespace`. This is a test scenario for instrumenting _new_ workloads at deploy time via the admission webhook.
* `test-resources/bin/test-cleanup.sh`: This script removes all resources created by the other scripts. **You should
  always run this script after running any of the scenario scripts, when you are done with your tests, otherwise the
  e2e tests will fail the next time you start them.** Note that all scenario scripts call the cleanup at the beginning,
  so there is no need to clean up between individual invocations of the scenario scripts.
* All scripts will, by default, use the target namespace `test-namespace` and the workload type `deployment`. They all
  accept two command line parameters to override these defaults. For example, use
  `test-resources/bin/test-scenario-01-aum-operator-cr.sh another-namespace replicaset` to run the scenario with
  the target namespace `another-namespace` and a replica set workload.
* Additional parameterization can be achieved via environment variables. Here is a full list of all available variables.
    * `ADDITIONAL_NAMESPACES`: Create two more test namespaces (in addition to the usual one test namespace) and deploy
      workloads there as well.
    * `ALLOWED_KUBECTXS`: A comma separated lists of Kubernetes contexts that the script are allowed to use. The scripts
      will refuse to deploy to/delete from any context not on this list. This is a protection against accidentally
      deploying something to or deleting something from a production Kubernetes context.
      It is recommended to set this in `test-resources/.env`.
    * `COLLECT_POD_LABELS_AND_ANNOTATIONS_ENABLED`: Set this to "false" to disable collecting pod labels and annotations
      as resource attributes.
      This defaults to `$TELEMETRY_COLLECTION_ENABLED`, which in turn defaults to "true".
    * `DASH0_API_ENDPOINT`: The endpoint for API requests (for synchronizing Perses dashboards, Prometheus check rules,
      synthetic checks and views). It is recommended to set this in `test-resources/.env`.
    * `DASH0_AUTHORIZATION_TOKEN`: The authorization token for sending telemetry to the Dash0 ingress endpoint and
      making API requests to the Dash0 API endpoint.
      It is recommended to set this in `test-resources/.env`.
    * `DASH0_INGRESS_ENDPOINT`: The ingress endpoint where telemetry is sent.
      It is recommended to set this in `test-resources/.env`.
    * `DEPLOY_APPLICATION_UNDER_MONITORING`: Set this to "false" to skip deploying a workload in the test namespace.
      This is assumed to be "true" by default.
    * `DEPLOY_MONITORING_RESOURCE`: Set this to "false" to skip deploying the Dash0 monitoring resource to the test
      namespace.
      This is assumed to be "true" by default.
    * `DEPLOY_OPERATOR_CONFIGURATION_VIA_HELM`: Omit the Helm settings to have the operator's Helm chart deploy the
      Dash0 operator configuration resource (aka auto configuration resource) automatically at operator manager startup.
      This is assumed to be "true" by default.
    * `DEPLOY_PERSES_DASHBOARD`: Set to "true" to deploy a Perses dashboard resource that will be synchronized to Dash0
      via the Dash0 API.
      This defaults to "false".
    * `DEPLOY_PROMETHEUS_RULE`: Set to "true" to deploy Prometheus rule resource that will be synchronized to Dash0 via
      the Dash0 API.
      This defaults to "false".
    * `DEPLOY_SYNTHETIC_CHECK`: Set to "true" to deploy a synthetic check resource that will be synchronized to Dash0
      via the Dash0 API.
      This defaults to "false".
    * `DEPLOY_VIEW`: Set to "true" to deploy a view resource that will be synchronized to Dash0 via the Dash0 API.
      This defaults to "false".
    * `FILELOG_OFFSETS_PVC`: Use a persistent volume claim to store filelog offsets, instead of the default config map
      based storage.
    * `FILELOG_OFFSETS_HOST_PATH_VOLUME`: Use a `hostPath` volume to store filelog offsets, instead of the default
      config map based storage.
    * `INSTRUMENT_WORKLOADS_MODE`: Set this to "all", "created-and-updated" or "none" to control the
      `instrumentWorkloads.mode` setting of the monitoring resource that will be deployed.
      This defaults to "all", unless `$TELEMETRY_COLLECTION_ENABLED` is "false", then it defaults to "none".
    * `LOG_COLLECTION`: Set this to "false" to disable collecting logs in monitored namespaces.
      This defaults to `$TELEMETRY_COLLECTION_ENABLED`, which in turn defaults to "true".
    * `KUBERNETES_INFRASTRUCTURE_METRICS_COLLECTION_ENABLED`: Set this to "false" to disable K8s infra metrics
      collection.
      This defaults to `$TELEMETRY_COLLECTION_ENABLED`, which in turn defaults to "true".
    * `OPERATOR_CONFIGURATION_VIA_HELM_DATASET`: Use this to set a custom dataset in the auto operator configuration
      resource.
    * `OPERATOR_HELM_CHART_VERSION`: Set this to use a specific version of the Helm chart. This is meant to be used
      together with `OPERATOR_HELM_CHART=dash0-operator/dash0-operator` or similar, where `OPERATOR_HELM_CHART` refers
      to an already installed remote Helm repository (e.g. https://dash0hq.github.io/dash0-operator) that contains the
      requested chart version.
    * `OPERATOR_HELM_CHART`: The name of the Helm chart to use for deploying the operator. Defaults to the local Helm
      chart sources in `helm-chart/dash0-operator`.
    * `OTEL_COLLECTOR_DEBUG_VERBOSITY_DETAILED`: Add a debug exporter to the OTel collectors with `verbosity: detailed`.
    * `OTEL_COLLECTOR_SEND_BATCH_MAX_SIZE`: Set the `send_batch_max_size parameter` of the batch processor of the
      collectors managed by the operator. There is usually no need to configure this. The value must be greater than or
      equal to 8192, which is the default value for `send_batch_size`.
    * `PROMETHEUS_SCRAPING_ENABLED`: Set this to "false" to disable Prometheus scraping in the test namespace via the
      monitoring resource.
      This defaults to `$TELEMETRY_COLLECTION_ENABLED`, which in turn defaults to "true".
    * `SELF_MONITORING_ENABLED`: Set this to "false" to disable the operator's self monitoring.
      This defaults to "true".
    * `SYNCHRONIZE_PERSES_DASHBOARDS`: Set this to "false" to disable synchronizing Perses dashboard resources via the
      Dash0 API.
      This defaults to "true".
    * `SYNCHRONIZE_PROMETHEUS_RULES`: Set this to "false" to disable synchronizing Prometheus rule resources via the
      Dash0 API.
      This defaults to "true".
    * `TELEMETRY_COLLECTION_ENABLED`: Set this to "false" to instruct the operator to not deploy OpenTelemetry
      collectors.
      This defaults to "true".
    * `USE_CERT_MANAGER`: Set this to "true" to have the operator use cert-manager to manage TLS certificates, instead
      of generating certificates on the fly during Helm install.
      If this is set to "true", the test scenario scripts will also make sure cert-manager is installed in the
      cert-manager namespace. (Note: After installing it once, it will not be automatically uninstalled again in
      `test-cleanup.sh`. This is deliberate since deploying cert-manager can take a while.)
    * `USE_OTLP_SINK`: Set this to "true" to deploy a local collector named OTLP sink and send telemetry there instead
      of sending it to an actual Dash0 backend.
      This defaults to "false".
    * `USE_TOKEN`: Set this to true to let use the auth token
      (`DASH0_AUTHORIZATION_TOKEN`) directly in the operator configuration resource instead of a secret ref.
      The default is to use the secret ref.
      This setting is used in both modes, that is, independent of whether `DEPLOY_OPERATOR_CONFIGURATION_VIA_HELM=false`
      has been provided.
    * Additional configuration for the Helm deployment can be put into `test-resources/bin/extra-values.yaml` (create
      the file if necessary).
* Last but not least, there are a couple of environment variables that control which images are built, potentially
  pushed, and used:
  * `IMAGE_REPOSITORY_PREFIX`: Set this to `registry.tld/path/` to build images as
    `registry.tld/path/operator-controller` instead of just `operator-controller`. (Default: `""`)
  * `IMAGE_TAG`: Set this to use a different image tag, i.e. set this to `some-tag` to use the images like
     `operator-controller:some-tag` instead of `operator-controller:latest`. (Default: `latest`)
  * `PULL_POLICY`: Set this to use a different default pull policy for all container images. The default is `NEVER`,
     which works with Docker Desktop when building images locally.
* There are also sets of four variables for each of the container images to override the values provided by
  `IMAGE_REPOSITORY_PREFIX`, `IMAGE_TAG`, and `PULL_POLICY`. These can be used if you need a different
  container image registry, tag or pull policy per image. The `*_IMAGE_REPOSITORY` variables together
  with `*_IMAGE_TAG` will override the fully qualified name of the image. For example, setting
  `CONTROLLER_IMAGE_REPOSITORY=ghcr.io/dash0hq/operator-controller` and `CONTROLLER_IMAGE_TAG=0.45.1` will instruct the
  scripts to use `ghcr.io/dash0hq/operator-controller:0.45.1`.
  You can also use `*_IMAGE_DIGEST` to use a specific image digest instead of an image tag.
  Here is the full list of image related environment variables:
    * `CONTROLLER_IMAGE_REPOSITORY`
    * `CONTROLLER_IMAGE_TAG`
    * `CONTROLLER_IMAGE_DIGEST`
    * `CONTROLLER_IMAGE_PULL_POLICY`
    * `INSTRUMENTATION_IMAGE_REPOSITORY`
    * `INSTRUMENTATION_IMAGE_TAG`
    * `INSTRUMENTATION_IMAGE_DIGEST`
    * `INSTRUMENTATION_IMAGE_PULL_POLICY`
    * `COLLECTOR_IMAGE_REPOSITORY`
    * `COLLECTOR_IMAGE_TAG`
    * `COLLECTOR_IMAGE_DIGEST`
    * `COLLECTOR_IMAGE_PULL_POLICY`
    * `CONFIGURATION_RELOADER_IMAGE_REPOSITORY`
    * `CONFIGURATION_RELOADER_IMAGE_TAG`
    * `CONFIGURATION_RELOADER_IMAGE_DIGEST`
    * `CONFIGURATION_RELOADER_IMAGE_PULL_POLICY`
    * `FILELOG_OFFSET_SYNC_IMAGE_REPOSITORY`
    * `FILELOG_OFFSET_SYNC_IMAGE_TAG`
    * `FILELOG_OFFSET_SYNC_IMAGE_DIGEST`
    * `FILELOG_OFFSET_SYNC_IMAGE_PULL_POLICY`
    * `FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_REPOSITORY`
    * `FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_TAG`
    * `FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_DIGEST`
    * `FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_PULL_POLICY`
* Similar environment variables are available for the test applications:
    * `TEST_IMAGE_REPOSITORY_PREFIX`: container registry for all test applications (defaults to "").
    * `TEST_IMAGE_TAG`: image tag for all test applications (defaults to "latest").
    * `TEST_IMAGE_PULL_POLICY`: pull policy for all test applications (defaults to "Never").
    * To override any of the previous three values for a specific test application image:
        * `TEST_APP_DOTNET_IMAGE_REPOSITORY`, `TEST_APP_DOTNET_IMAGE_TAG`, `TEST_APP_DOTNET_IMAGE`, and `TEST_APP_DOTNET_IMAGE_PULL_POLICY`.
        * `TEST_APP_JVM_IMAGE_REPOSITORY`, `TEST_APP_JVM_IMAGE_TAG`, `TEST_APP_JVM_IMAGE`, and `TEST_APP_JVM_IMAGE_PULL_POLICY`.
        * `TEST_APP_NODEJS_IMAGE_REPOSITORY`, `TEST_APP_NODEJS_IMAGE_TAG`, `TEST_APP_NODEJS_IMAGE`, and `TEST_APP_NODEJS_IMAGE_PULL_POLICY`.
* To run the scenario with the images that have been built from the main branch and pushed to ghcr.io most recently:
    ```
    IMAGE_REPOSITORY_PREFIX=ghcr.io/dash0hq/ \
      IMAGE_TAG=main-dev \
      PULL_POLICY="" \
      test-resources/bin/test-scenario-01-aum-operator-cr.sh
    ```
    * To run the scenario with the helm chart from the official remote repository and the default images referenced in
      that chart (the Helm repository must have been installed beforehand):
      ```
      OPERATOR_HELM_CHART=dash0-operator/dash0-operator \
        test-resources/bin/test-scenario-01-aum-operator-cr.sh
      ```
    * You can add `OPERATOR_HELM_CHART_VERSION=0.11.0` to the command above to install a specific version of the
      Helm chart. This can be useful to test upgrade scenarios.

### End-to-End Tests

The end-to-end tests have been tested with [Docker Desktop](https://docs.docker.com/desktop/features/kubernetes/)
on Mac (with Kubernetes support enabled) and [kind](https://kind.sigs.k8s.io/), as well experimentally against remote
clusters.
Docker Desktop is probably the easiest setup to get started with.
See below for additional instructions for [kind](#running-end-to-end-tests-on-kind).

Copy the file `test-resources/.env.template` to `test-resources/.env` and set `E2E_KUBECTX` to the name of the
Kubernetes context you want to use for the tests, or set `E2E_KUBECTX` via other means (e.g. export it in your shell,
via `direnv` etc.).

The end-to-end tests can be run via `make test-e2e`.
This assumes that all required images have been built beforehand.
Use `make build-images-test-e2e` to build all images from local sources and then run the end-to-end tests using those
images.

The tests can also be run with remote images, like this:
```
CONTROLLER_IMAGE_REPOSITORY=ghcr.io/dash0hq/operator-controller \
  CONTROLLER_IMAGE_TAG=main-dev \
  INSTRUMENTATION_IMAGE_REPOSITORY=ghcr.io/dash0hq/instrumentation \
  INSTRUMENTATION_IMAGE_TAG=main-dev \
  COLLECTOR_IMAGE_REPOSITORY=ghcr.io/dash0hq/collector \
  COLLECTOR_IMAGE_TAG=main-dev \
  CONFIGURATION_RELOADER_IMAGE_REPOSITORY=ghcr.io/dash0hq/configuration-reloader \
  CONFIGURATION_RELOADER_IMAGE_TAG=main-dev \
  FILELOG_OFFSET_SYNC_IMAGE_REPOSITORY=ghcr.io/dash0hq/filelog-offset-sync \
  FILELOG_OFFSET_SYNC_IMAGE_TAG=main-dev \
  FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_REPOSITORY=ghcr.io/dash0hq/filelog-offset-volume-ownership \
  FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_TAG=main-dev \
  make test-e2e
```

The test suite can also be run with a Helm chart from a remote repository:

```
OPERATOR_HELM_CHART=dash0-operator/dash0-operator \
  OPERATOR_HELM_CHART_URL=https://dash0hq.github.io/dash0-operator \
  make test-e2e
```

When an end-to-end test case fails, the test suite automatically collects pod descriptions, config maps and pod logs
from the Kubernetes cluster at the time of the failure.
The collected data can be found in `test-resources/e2e/logs`.
It is often helpful to understand why the test case has failed.
In addition, the data that the OpenTelemetry collectors emitted during the last test case can be reviewed in
`test-resources/e2e/volumes/otlp-sink`, in `logs.jsonl`, `metrics.jsonl`, and `traces.jsonl` respectively.

#### Running End-to-End Tests on kind

To use kind for running the end-to-end tests, you need to create a kind cluster first.
The file <test-resources/kind-config.yaml> file can be used as a blueprint to create a cluster.

Before creating the cluster, the two hostPath settings in `test-resources/kind-config.yaml` need to be aligned with your
local file system structure.
(Alternatively, create a symbolic link from
`/Users/username/dash0/code/dash0-operator/test-resources/e2e/volumes/` to the actual path, see instructions in
`test-resources/kind-config.yaml` for details).
Also, make sure the path mentioned under `hostPath` is listed in Docker Desktop's settings under
"Resources -> File sharing".

Then execute the following command to create the cluster:

```
kind create cluster --name dash0-operator-playground --config test-resources/kind-config.yaml
```

Also, the [Kubernetes Cloud Provider for KIND](https://github.com/kubernetes-sigs/cloud-provider-kind) needs to be
running. Install it with the following commands:

```
go install sigs.k8s.io/cloud-provider-kind@latest
sudo install ~/go/bin/cloud-provider-kind /usr/local/bin
```

Then, start it (in a separate shell) with the following command and leave it running:
```
sudo cloud-provider-kind
```

Last but not least, set `E2E_KUBECTX` in `test-resources/.env` to the name of the Kubernetes context that corresponds to
your kind cluster (e.g. `kind-dash0-operator-playground`).

Optionally, once you are done, execute the following command to delete the cluster:

```
kind delete cluster --name dash0-operator-playground
```

#### Running End-to-End Tests against a Remote Cluster

TODO: Add instructions for running the e2e tests against a remote cluster.
Currently, this involves some local ad hoc modifications, like
* pushing the test image to the remote registry,
* changing`test-resources/node.js/express/deployment.yaml` and friends
    * to set the container image `ghcr.io/dash0hq/dash0-operator-nodejs-20-express-test-app:latest"`, and
    * removing `imagePullPolicy: Never`

## Migration Strategy When Updating Instrumentation Values

When a new release of the operator changes the instrumentation values (new or changed environment variables, new labels,
new volumes etc.), we need to make sure that previously instrumented workloads are updated correctly. This should always
be accompanied by corresponding tests (for example new test cases in `workload_modifier_test.go`, see the test suite
`"when updating instrumentation from 0.5.1 to 0.6.0"` in commit 300a765a64a42d98dcc6d9a66dccc534b610ab65 for an
example).
