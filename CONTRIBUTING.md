Contributing
============

## Prerequisites

- Signing the [Contributor License Agreement](https://github.com/cla-assistant/cla-assistant)
- Go (version >= v1.26)
- Docker
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.
- [helm](https://helm.sh/docs/intro/install/)
- [helm unittest plug-in](https://github.com/helm-unittest/helm-unittest/tree/main)

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

## Setting Up a Kind Cluster for Local Testing

Note: When using Docker Desktop with its integrated Kubernetes support via `kubeadm`, you can skip this section.

This section describes the recommended way to set up a local [kind](https://kind.sigs.k8s.io/) cluster suitable for
testing the operator; that is, for using the [semi-manual test scenarios](#semi-manual-test-scenarios) as well as
running [end-to-end-tests](#end-to-end-tests).

In order to use the scripts mentioned below, it is required to set `LOCAL_REGISTRY_VOLUME_PATH` to a local directory
path that will be used as storage by the registry container. In addition, `DASH0_KIND_CLUSTER` and `DASH0_KIND_CONFIG`
can be used to customize the created cluster if needed. By default, the cluster's name will be `dash0-operator-lab` and
it will be configured using `test-resources/kind-config.yaml`.
Run `test-resources/bin/create_cluster_and_registry.sh` to create the cluster and the local registry.

The resulting cluster also has a port mapping for the ingress-nginx in the cluster, so that services in the cluster can
be reached via `http://localhost:8080/...`.

If you do no longer need the cluster, run `test-resources/bin/delete_cluster_and_registry.sh` to discard it.

## Using Minikube for Local Testing

In general, the instructions for Docker Desktop should work for Minikube as well.
There is one additional step though:
* Run `eval $(minikube docker-env)` before running any scripts from `test-resources/bin` or before running end-to-end
  tests.
  This makes container images you build locally available in the cluster.
  Be aware that this only takes effect in the current shell, that is, if you open a new shell, you need to execute this
  again.
  (You will run into `ErrImageNeverPull` when you build images in a shell where this has not been executed, or your
  tests will use outdated imgages.)
* To undo this, you might want to run `eval $(minikube docker-env -u)` when you are done testing, or alternatively close
  the current shell.

See https://minikube.sigs.k8s.io/docs/commands/docker-env/ for more information.

## Deploying to a Local Cluster for Testing Purposes

This approach is suitable for deploying the operator to a cluster running locally on your machine.

As an alternative to the steps outlined in this section, there is also a suite of scripts available that take care of
building and deploying everything required for a full local test, including the Dash0OperatorConfiguration resource,
Dash0Monitoring resources, test workloads, third-party CRDs etc.)
See section [Semi-Manual Test Scenarios](#semi-manual-test-scenarios) for more information on that.

In contrast to the semi-manual test scenarios, this section only describes the individual steps to build and deploy a
bare-bones Dash0 operator.

**With Docker Desktop:**

Run `make images deploy IMAGE_REPOSITORY_PREFIX="" PULL_POLICY=Never` to build all required container images locally and
deploy the Helm chart.
This will tag the image as `operator-controller:latest`, `collector:latest` etc.

Note: No Dash0OperatorConfiguration or Dash0Monitoring resource will be created automatically, so the operator will
not really do anything.

**With Kind and a Local Registry:**

Run `make images push-images deploy IMAGE_REPOSITORY_PREFIX="localhost:5001/" PULL_POLICY=Always` to build all
required container images locally, push them to the local registry and deploy the Helm chart.
This will tag the image as `localhost:5001/operator-controller:latest`, `localhost:5001/collector:latest` etc.

**Using images from a remote registry:**

Run `make deploy IMAGE_REPOSITORY_PREFIX=ghcr.io/dash0hq/ IMAGE_TAG=main-dev PULL_POLICY=""`

**NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as
admin.

**Undeploy the controller from the cluster:**

```sh
make undeploy
```

This will also remove the custom resource definitions.

## Run Tests

```
make test
```

This will run the go unit tests as well as the helm chart tests.

## Semi-Manual Test Scenarios

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

When using [kind](https://kind.sigs.k8s.io/) for local testing, make sure to follow the instructions in
[Setting Up a Kind Cluster for Local Testing](#setting-up-a-kind-cluster-for-local-testing) first.
When using Docker Desktop, no additional setup steps for the cluster are necessary.

If you haven't created `test-resources/.env` yet, you can copy the file `test-resources/.env.template` to
`test-resources/.env`, or set the environment variables listed in `test-resources/.env.template` via other means.
Make sure the comma-separated list `ALLOWED_KUBECTXS` contains the name of the kubernetes you want to use for the test
scripts.
If you want to report telemetry to a Dash0 backend, make sure that `DASH0_INGRESS_ENDPOINT` and
`DASH0_AUTHORIZATION_TOKEN` are set correctly.
Alternatively, instead of providing `test-resources/.env`, make sure that the environment variables mentioned in
`test-resources/.env.template` are set via other means.
Use `kubectx` or `kubectl config set-context` to switch to the desired Kubernetes context before running the scripts.

**Quickstart for Docker Desktop with kubeadm:**
```
IMAGE_REPOSITORY_PREFIX="" \
  PULL_POLICY="Never" \
  test-resources/bin/test-scenario-02-operator-cr-aum.sh
```

The test application can be called via `curl http://localhost/deployment/node.js/dash0-k8s-operator-test` (for
example to create spans and logs).
Run `test-resources/bin/test-cleanup.sh` to remove the operator and the test application afterward.
If you are using `test-resources/.env`, you can also add the environment variables (`IMAGE_REPOSITORY_PREFIX` etc.)
there to always have them set automatically instead of providing them with every script invocation.

**Quickstart for Kind with a [local container registry](#setting-up-a-kind-cluster-for-local-testing):**:
```
IMAGE_REPOSITORY_PREFIX="localhost:5001/" \
  PUSH_BUILT_IMAGES=true \
  test-resources/bin/test-scenario-02-operator-cr-aum.sh
```

The test application can be called via `curl http://localhost:8080/deployment/node.js/dash0-k8s-operator-test` (for
example to create spans and logs).
Run `test-resources/bin/test-cleanup.sh` to remove the operator and the test application afterward.
If you are using `test-resources/.env`, you can also add the environment variables (`IMAGE_REPOSITORY_PREFIX` etc.)
there to always have them set automatically instead of providing them with every script invocation.

Moving beyond the quickstart instructions, here are more details on the test scripts and all available parameters:

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
  accept three command line parameters to override these defaults. For example, use
  `test-resources/bin/test-scenario-01-aum-operator-cr.sh another-namespace replicaset jvm` to run the scenario with
  the target namespace `another-namespace` and a JVM based replica set workload.
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
    * `COLLECTOR_ENABLE_PPROF`: Set to "true" to enable the pprof extension in the collector containers.
      See <helm-chart/dash0-operator/README.md#create-heap-profiles> for instructions for creating heap profiles.
    * `DASH0_API_ENDPOINT`: The endpoint for API requests (for synchronizing Perses dashboards, Prometheus check rules,
      synthetic checks and views). It is recommended to set this in `test-resources/.env`.
    * `DASH0_AUTHORIZATION_TOKEN`: The authorization token for sending telemetry to the Dash0 ingress endpoint and
      making API requests to the Dash0 API endpoint.
      It is recommended to set this in `test-resources/.env`.
    * `DASH0_INGRESS_ENDPOINT`: The ingress endpoint where telemetry is sent.
      It is recommended to set this in `test-resources/.env`.
    * `DASH0_NAMESPACED_DATASET`: Dataset used exclusively by the test script for namespaced exporters. It will be
      placed in the monitoring resource of the test namespace to override the default config from the operator
      configuration resource.
    * `DASH0_NAMESPACED_AUTHORIZATION_TOKEN`: Token used exclusively by the test script for namespaced exporters. It
      will be placed in the monitoring resource of the test namespace to override the default config from the operator
      configuration resource.
    * `DEPLOY_APPLICATION_UNDER_MONITORING`: Set this to "false" to skip deploying a workload in the test namespace.
      This is assumed to be "true" by default.
    * `DEPLOY_NGINX_INGRESS`: Set this to "false" to skip deploying an nginx ingress to the cluster.
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
      based storage. Possible values:
        * `FILELOG_OFFSETS_PVC=kind`: deploy a PersistentVolume and PersistentVolumeClaim suitable for kind clusters
          created via test-resources/bin/create_cluster_and_registry.sh or based on `test-resources/kind-config.yaml`
          (i.e. clusters that have the `offset-storage` extra mounts), then use this for filelog offset storage
        * `FILELOG_OFFSETS_PVC=docker`: deploy a StorageClass and PersistentVolumeClaim suitable for Docker Desktop,
          then use this for filelog offset storage
        * `FILELOG_OFFSETS_PVC=gke`: deploy a PersistentVolumeClaim suitable for GCP GKE clusters, then use this for
          filelog offset storage
        * `FILELOG_OFFSETS_PVC=aws-efs-dp`: deploy a StorageClass and PersistentVolumeClaim suitable for an AWS EKS
          cluster, based on an AWS EFS file system, with dynamic provisioning (see
          https://docs.aws.amazon.com/eks/latest/userguide/efs-csi.html and
          https://github.com/kubernetes-sigs/aws-efs-csi-driver/tree/master/examples/kubernetes/dynamic_provisioning)
      Take a look at the files in `test-resources/filelog-offset-storage` for details.
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
    * `OPERATOR_MANAGER_PPROF_PORT`: Set this to a numeric value to enable pprof in the operator manager container.
      See <helm-chart/dash0-operator/README.md#create-heap-profiles> for instructions for creating heap profiles.
    * `OTEL_COLLECTOR_DEBUG_VERBOSITY_DETAILED`: Add a debug exporter to the OTel collectors with `verbosity: detailed`.
    * `OTEL_COLLECTOR_SEND_BATCH_MAX_SIZE`: Set the `send_batch_max_size parameter` of the batch processor of the
      collectors managed by the operator. There is usually no need to configure this. The value must be greater than or
      equal to 8192, which is the default value for `send_batch_size`.
    * `PROMETHEUS_SCRAPING_ENABLED`: Set this to "false" to disable Prometheus scraping in the test namespace via the
      monitoring resource.
      This defaults to `$TELEMETRY_COLLECTION_ENABLED`, which in turn defaults to "true".
    * `PROMETHEUS_CRD_SUPPORT_ENABLED`: Set this to `true` to enable support for Prometheus CRDs (e.g. ServiceMonitor).
      If at least one namespace has a monitoring resource with Prometheus scraping enabled, the OpenTelemetry
      target-allocator will be deployed.
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
    * `ALLOW_MORE_TIME_FOR_COLLECTOR_STARTUP` set to `true` increases the `failureThreshold` for the startup probes of
      collectors to support slower environments (e.g. running kind with limited resources).
    * `USE_MULTI_CAST` set to `true` to deploy
      `test-resources/customresources/dash0operatorconfiguration/dash0operatorconfiguration.multi.yaml.template`
      which has two Dash0 exports with different datasets and tokens. Note: Setting this only makes sense when also
      setting `DEPLOY_OPERATOR_CONFIGURATION_VIA_HELM="false"` and defining `DASH0_SECOND_DATASET` and
      `DASH0_SECOND_AUTHORIZATION_TOKEN`.
    * `DASH0_SECOND_DATASET` the dataset of the second Dash0 export (when using `USE_MULTI_CAST`)
    * `DASH0_SECOND_AUTHORIZATION_TOKEN` the auth token for the second Dash0 export (when using `USE_MULTI_CAST`)
    * Additional configuration for the Helm deployment can be put into `test-resources/bin/extra-values.yaml` (create
      the file if necessary).
* Last but not least, there are a couple of environment variables that control which images are built and used, and whether
  the built images are pushed automatically:
  * `IMAGE_REPOSITORY_PREFIX`: Set this to `registry.tld/path/` to build images as
    `registry.tld/path/operator-controller` instead of just `operator-controller`. (Default: `""`)
  * `IMAGE_TAG`: Set this to use a different image tag, i.e. set this to `some-tag` to use the images like
     `operator-controller:some-tag` instead of `operator-controller:latest`. (Default: `latest`)
  * `PULL_POLICY`: Set this to use a different default pull policy for all container images. The default is `NEVER`,
     which works with Docker Desktop when building images locally.
  * `SKIP_IMAGE_BUILDS`: Set this to `true` to skip all image builds for operator images (test app images will still be
     built, see `SKIP_TEST_APP_IMAGE_BUILDS`.)
  * `PUSH_BUILT_IMAGES`: when using a local registry, or when testing against a remote cluster, it can be convenient to
    automatically push the built images to the configured registry (defined by `IMAGE_REPOSITORY_PREFIX`)
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
    * `SKIP_TEST_APP_IMAGE_BUILDS`: Set this to true to skip building the images for the test applications.
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
  SKIP_IMAGE_BUILDS=true \
    OPERATOR_HELM_CHART=dash0-operator/dash0-operator \
    test-resources/bin/test-scenario-01-aum-operator-cr.sh
  ```
* You can add `OPERATOR_HELM_CHART_VERSION=x.y.z` to the command above to install a specific version of the
  Helm chart. This can be useful to test upgrade scenarios.

## End-to-End Tests

The end-to-end tests have been tested with
- [Docker Desktop](https://docs.docker.com/desktop/features/kubernetes/) on Mac (with its integrated Kubernetes support
  enabled with the default option `kubeadm`), and
- [kind](https://kind.sigs.k8s.io/).

Copy the file `test-resources/.env.template` to `test-resources/.env` and set `E2E_KUBECTX` to the name of the
Kubernetes context you want to use for the tests, or set `E2E_KUBECTX` via other means (e.g. export it in your shell,
via `direnv` etc.).

**Quickstart for Docker Desktop with kubeadm:** To run the end-to-end tests on Docker Desktop, run
```
E2E_KUBECTX=docker-desktop \
  IMAGE_REPOSITORY_PREFIX="" \
  PULL_POLICY="Never" \
  INGRESS_PORT=80 \
  make build-all-test-e2e
```

**Quickstart for Kind with a [local container registry](#setting-up-a-kind-cluster-for-local-testing):**: To run the
end-to-end tests on a kind cluster with a local registry, run
```
E2E_KUBECTX=kind-dash0-operator-lab \
  IMAGE_REPOSITORY_PREFIX="localhost:5001/" \
  make build-all-push-all-test-e2e
```

If you are using `test-resources/.env`, you can also add the environment variables (`IMAGE_REPOSITORY_PREFIX` etc.)
there to always have them set automatically instead of providing them with every end-to-end test run.

In general, the end-to-end tests can be run via `make test-e2e`.
The make target `test-e2e` assumes that all required images have been built beforehand and are available in the target
Kubernetes cluster (the one associated with the kubectl context determined by `E2E_KUBECTX`).

The tests can also be run with images from any registry, like this:
```
IMAGE_REPOSITORY_PREFIX=ghcr.io/dash0hq/ \
  IMAGE_TAG=main-dev \
  PULL_POLICY="" \
  make test-e2e
```

Note that the `TEST_IMAGE_*` environment variables will also be used for auxiliary images like the Dash0 API mock.
`DASH0_API_MOCK_IMAGE_REPOSITORY`, `DASH0_API_MOCK_IMAGE_TAG`, and `DASH0_API_MOCK_IMAGE_PULL_POLICY` are supported for
individual control over this image.

All of the above use the local Helm chart from the directory `helm-chart/dash0-operator`.
The test suite can also be run with a Helm chart from a remote Helm repository:

```
OPERATOR_HELM_CHART=dash0-operator/dash0-operator \
  OPERATOR_HELM_CHART_URL=https://dash0hq.github.io/dash0-operator \
  make test-e2e
```

When running with a remote Helm chart like this, the images from the chart are used by default, instead of local images.

When an end-to-end test case fails, the test suite automatically collects pod descriptions, config maps and pod logs
from the Kubernetes cluster at the time of the failure.
The collected data can be found in `test-resources/e2e/logs`.
It is often helpful to understand why the test case has failed.
In addition, the data that the OpenTelemetry collectors emitted during the test suite can be reviewed in a couple of
places:
- While the test suite is running: Via the logs of the `otlp-sink` pod's `otelcol` container (the collector
  configuration has a debug exporter with detailed verbosity).
- While the test suite is running: In the `telemetry-files` volume of the `otlp-sink` pod.
  This volume is configured as `telemetryFilesVolume` in `test/e2e/otlp-sink/helm-chart/values.yaml`.
  (For systems that use a virtual machine to run containers like Docker Desktop on macOS, use
  `docker run -it --rm --privileged --pid=host justincormack/nsenter1` or a similar mechanism to get access.)
  The volume contains three files, `logs.jsonl`, `metrics.jsonl`, and `traces.jsonl`, which are newline delimited JSON
  files of the collected telemetry.
- After a failed test: In `test-resources/e2e/logs/kubectl_-n_otlp-sink_logs_otlp-sink-.*_--all-containers=true`.

### Installing Basic Infrastructure for End-to-End Tests

The end-to-end test suite installs a couple of basic infrastructure in the cluster on demand, if it is missing:
* ingress-nginx
* metrics-server
* cert-manager

If these do not exist in the cluster when starting the e2e test suite, the suite will install them on startup and remove
them once the suite is finished.
You can avoid this overhead by installing them once manually, ahead of time.
If these components do exist in the cluster when starting the e2e test suite, the suite will not install or change them,
and also not remove them once the suite is finished.

Execute the following commands to speed up the e2e test suite a bit:
```
test-resources/bin/deploy-ingress-nginx.sh
helm repo add metrics-server https://kubernetes-sigs.github.io/metrics-server/ &&
helm upgrade --install --set args={--kubelet-insecure-tls} metrics-server metrics-server/metrics-server --namespace kube-system
test-resources/cert-manager/deploy.sh
```

### Running End-to-End Tests against a Remote Cluster

Running end-to-end tests with a remote cluster requires network access from the machine running the end-to-end tests
to an ingress in the cluster.
Setting up the ingress in the cluster might depend on the specific cloud that is being used (for example, GKE
Autopilot will not allow deploying a regular ingress-nginx with port 80).
The script `test-resources/bin/deploy-ingress-nginx.sh` and the manifest `test-resources/nginx/kustomization.yaml` can
serve as a starting point, but YMMV.

Once an ingress has been set up, it needs to be made accessible from the machine running the tests.
For the ingress-nginx mentioned above, this could be achieved with port-forwarding:
```
# start this in a separate shell, leave the kubectl port-forward command running while the test suite is running
kubectl --namespace ingress-nginx port-forward $(kubectl get pods --namespace ingress-nginx -l app.kubernetes.io/component=controller -o jsonpath="{.items[0].metadata.name}") 8080:80
```

Note: The commands below assume that the remote cluster uses nodes with CPU architecture AMD64 (hence
`IMAGE_PLATFORMS=linux/amd64`), otherwise `IMAGE_PLATFORMS` might need to be set differently (in particular when the CPU
architecture of the machine running `make build-all-push-all-test-e2e` and the cluster nodes differ).

Assuming worloads in the cluster can be reached, running the end-to-end tests can be done as follows:

**With locally build images and the local Helm chart:**

These require a remote registry that you can push to, which is network-reachable from the test cluster.
If the registry requires authentication, additional steps are necessary.

```
E2E_KUBECTX=$kubectx_for_your_test_cluster \
  IMAGE_PLATFORMS=linux/amd64 \
  IMAGE_REPOSITORY_PREFIX="your-container-image-registry.com/base/path/to/repositories/" \
  make build-all-push-all-test-e2e
```

**With a published Helm chart and officially released images:**

```
E2E_KUBECTX=$kubectx_for_your_test_cluster \
  OPERATOR_HELM_CHART=dash0-operator/dash0-operator \
  OPERATOR_HELM_CHART_URL=https://dash0hq.github.io/dash0-operator \
  IMAGE_PLATFORMS=linux/amd64 \
  TEST_IMAGE_REPOSITORY_PREFIX="your-container-image-registry.com/base/path/to/repositories/" \
  make all-auxiliary-images push-all-auxiliary-images test-e2e
```

## Migration Strategy When Updating Instrumentation Values

When a new release of the operator changes the instrumentation values (new or changed environment variables, new labels,
new volumes etc.), we need to make sure that previously instrumented workloads are updated correctly. This should always
be accompanied by corresponding tests (for example new test cases in `workload_modifier_test.go`, see the test suite
`"when updating instrumentation from 0.5.1 to 0.6.0"` in commit 300a765a64a42d98dcc6d9a66dccc534b610ab65 for an
example).
