Contributing
============

## Prerequisites
- Go (version >= v1.24)
- Docker
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.
- [helm](https://helm.sh/docs/intro/install/)
- [helm unittest plug-in](https://github.com/helm-unittest/helm-unittest/tree/main)

## Deploying to a Local Cluster for Testing Purposes

This approach is suitable for deploying the operator to a cluster running locally on your machine, for example
via the Kubernetes support included in Docker Desktop.

Run `make docker-build` to build the container image locally, this will tag the image as
`operator-controller:latest`.

After that, you can deploy the operator to your cluster:

* Deploy the locally built image `operator-controller:latest` to the cluster: `make deploy-via-helm`
* Alternatively, deploy with images from a remote registry:
  ```
  make deploy-via-helm \
    CONTROLLER_IMG_REPOSITORY=ghcr.io/dash0hq/operator-controller \
    CONTROLLER_IMG_TAG=main-dev \
    CONTROLLER_IMG_PULL_POLICY="" \
    INSTRUMENTATION_IMG_REPOSITORY=ghcr.io/dash0hq/instrumentation \
    INSTRUMENTATION_IMG_TAG=main-dev \
    INSTRUMENTATION_IMG_PULL_POLICY="" \
    COLLECTOR_IMG_REPOSITORY=ghcr.io/dash0hq/collector \
    COLLECTOR_IMG_TAG=main-dev \
    COLLECTOR_IMG_PULL_POLICY="" \
    CONFIGURATION_RELOADER_IMG_REPOSITORY=ghcr.io/dash0hq/configuration-reloader \
    CONFIGURATION_RELOADER_IMG_TAG=main-dev \
    CONFIGURATION_RELOADER_IMG_PULL_POLICY=""
    FILELOG_OFFSET_SYNC_IMG_REPOSITORY=ghcr.io/dash0hq/filelog-offset-sync \
    FILELOG_OFFSET_SYNC_IMG_TAG=main-dev \
    FILELOG_OFFSET_SYNC_IMG_PULL_POLICY=""
  ```
* The custom resource definition will automatically be installed when deploying the operator. However, you can also do
  that separately via kustomize if required via `make install`.

**NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as
admin.

**Undeploy the controller from the cluster:**

```sh
make undeploy-via-helm
```

This will also remove the custom resource definition. However, the custom resource definition can also be removed
separately via `make uninstall` without removing the operator.

## Run Tests

```
make test
```

This will run the go unit tests as well as the helm chart tests.

### End-to-End Tests

The end-to-end tests have been tested with [Docker Desktop](https://docs.docker.com/desktop/features/kubernetes/)
on Mac (with Kubernetes support enabled) and [kind](https://kind.sigs.k8s.io/), as well experimentally against remote
clusters.
Docker Desktop is probably the easiest setup to get started with.
See below for additional instructions for [kind](#running-end-to-end-tests-on-kind).

Copy the file `test-resources/.env.template` to `test-resources/.env` and set `E2E_KUBECTX` to the name of the
Kubernetes context you want to use for the tests.

To run the end-to-end tests:
```
make test-e2e
```

The tests can also be run with remote images, like this:
```
CONTROLLER_IMG_REPOSITORY=ghcr.io/dash0hq/operator-controller \
  CONTROLLER_IMG_TAG=main-dev \
  INSTRUMENTATION_IMG_REPOSITORY=ghcr.io/dash0hq/instrumentation \
  INSTRUMENTATION_IMG_TAG=main-dev \
  COLLECTOR_IMG_REPOSITORY=ghcr.io/dash0hq/collector \
  COLLECTOR_IMG_TAG=main-dev \
  CONFIGURATION_RELOADER_IMG_REPOSITORY=ghcr.io/dash0hq/configuration-reloader \
  CONFIGURATION_RELOADER_IMG_TAG=main-dev \
  FILELOG_OFFSET_SYNC_IMG_REPOSITORY=ghcr.io/dash0hq/filelog-offset-sync \
  FILELOG_OFFSET_SYNC_IMG_TAG=main-dev \
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

### Semi-Manual Test Scenarios

The e2e tests might sometimes not be the best tool to troubleshoot the operator, simply because they remove everything
they deploy in their `AfterAll`/`AfterEach` hooks.
The scripts in `test-resources/bin` can be used for creating a specific scenario and inspecting then its behavior.
They are also useful to actually report data to an actual Dash0 backend.

If you haven't created `test-resources/.env` yet, copy the file `test-resources/.env.template` to `test-resources/.env`.
Make sure the comma-separated list `ALLOWED_KUBECTXS` contains the name of the kubernetes you want to use for the test
scripts.
Use `kubectx` or `kubectl config set-context` to switch to the desired Kubernetes context before running the scripts.
If you want to report telemetry to a Dash0 backend, set `DASH0_AUTHORIZATION_TOKEN` as well.

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
* Additionally, there is a set of four variables for each of the container images to determine which version of the
  image is used. By default, all images will be built locally before deploying anything. The `*_IMG_REPOSITORY` together
  with `*_IMG_TAG` will override the fully qualified name of the image. For example, setting
  `CONTROLLER_IMG_REPOSITORY=ghcr.io/dash0hq/operator-controller` and `CONTROLLER_IMG_TAG=0.45.1` will instruct the
  scripts to use `ghcr.io/dash0hq/operator-controller:0.45.1` instead of the locally built operator manager image.
  You can also use `*_IMG_DIGEST` to use a specific image digest instead of an image tag.
  Here is the full list of image related environment variables:
    * `CONTROLLER_IMG_REPOSITORY`
    * `CONTROLLER_IMG_TAG`
    * `CONTROLLER_IMG_DIGEST`
    * `CONTROLLER_IMG_PULL_POLICY`
    * `INSTRUMENTATION_IMG_REPOSITORY`
    * `INSTRUMENTATION_IMG_TAG`
    * `INSTRUMENTATION_IMG_DIGEST`
    * `INSTRUMENTATION_IMG_PULL_POLICY`
    * `COLLECTOR_IMG_REPOSITORY`
    * `COLLECTOR_IMG_TAG`
    * `COLLECTOR_IMG_DIGEST`
    * `COLLECTOR_IMG_PULL_POLICY`
    * `CONFIGURATION_RELOADER_IMG_REPOSITORY`
    * `CONFIGURATION_RELOADER_IMG_TAG`
    * `CONFIGURATION_RELOADER_IMG_DIGEST`
    * `CONFIGURATION_RELOADER_IMG_PULL_POLICY`
    * `FILELOG_OFFSET_SYNC_IMG_REPOSITORY`
    * `FILELOG_OFFSET_SYNC_IMG_TAG`
    * `FILELOG_OFFSET_SYNC_IMG_DIGEST`
    * `FILELOG_OFFSET_SYNC_IMG_PULL_POLICY`
* To run the scenario with the images that have been built from the main branch and pushed to ghcr.io most recently:
    ```
    CONTROLLER_IMG_REPOSITORY=ghcr.io/dash0hq/operator-controller \
      CONTROLLER_IMG_TAG=main-dev \
      INSTRUMENTATION_IMG_REPOSITORY=ghcr.io/dash0hq/instrumentation \
      INSTRUMENTATION_IMG_TAG=main-dev \
      COLLECTOR_IMG_REPOSITORY=ghcr.io/dash0hq/collector \
      COLLECTOR_IMG_TAG=main-dev \
      CONFIGURATION_RELOADER_IMG_REPOSITORY=ghcr.io/dash0hq/configuration-reloader \
      CONFIGURATION_RELOADER_IMG_TAG=main-dev \
      FILELOG_OFFSET_SYNC_IMG_REPOSITORY=ghcr.io/dash0hq/filelog-offset-sync \
      FILELOG_OFFSET_SYNC_IMG_TAG=main-dev \
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

## Make Targets

Run `make help` for more information on all potential `make` targets.
More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Migration Strategy When Updating Instrumentation Values

When a new release of the operator changes the instrumentation values (new or changed environment variables, new labels,
new volumes etc.), we need to make sure that previously instrumented workloads are updated correctly. This should always
be accompanied by corresponding tests (for example new test cases in `workload_modifier_test.go`, see the test suite
`"when updating instrumentation from 0.5.1 to 0.6.0"` in commit 300a765a64a42d98dcc6d9a66dccc534b610ab65 for an
example).

## Contributing

No contribution guidelines are available at this point.
