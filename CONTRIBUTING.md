Contributing
============

## Prerequisites
- Go (version >= v1.23)
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
    FILELOG_OFFSET_SYNCH_IMG_REPOSITORY=ghcr.io/dash0hq/filelog-offset-synch \
    FILELOG_OFFSET_SYNCH_IMG_TAG=main-dev \
    FILELOG_OFFSET_SYNCH_IMG_PULL_POLICY=""
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
  CONTROLLER_IMG_PULL_POLICY="" \
  INSTRUMENTATION_IMG_REPOSITORY=ghcr.io/dash0hq/instrumentation \
  INSTRUMENTATION_IMG_TAG=main-dev \
  INSTRUMENTATION_IMG_PULL_POLICY="" \
  COLLECTOR_IMG_REPOSITORY=ghcr.io/dash0hq/collector \
  COLLECTOR_IMG_TAG=main-dev \
  COLLECTOR_IMG_PULL_POLICY="" \
  CONFIGURATION_RELOADER_IMG_REPOSITORY=ghcr.io/dash0hq/configuration-reloader \
  CONFIGURATION_RELOADER_IMG_TAG=main-dev \
  CONFIGURATION_RELOADER_IMG_PULL_POLICY="" \
  FILELOG_OFFSET_SYNCH_IMG_REPOSITORY=ghcr.io/dash0hq/filelog-offset-synch \
  FILELOG_OFFSET_SYNCH_IMG_TAG=main-dev \
  FILELOG_OFFSET_SYNCH_IMG_PULL_POLICY="" \
  make test-e2e
```

The test suite can also be run with a Helm chart from a remote repository:

```
OPERATOR_HELM_CHART=dash0-operator/dash0-operator \
  OPERATOR_HELM_CHART_URL=https://dash0hq.github.io/dash0-operator \
  CONTROLLER_IMG_REPOSITORY="" \
  CONTROLLER_IMG_TAG="" \
  CONTROLLER_IMG_PULL_POLICY="" \
  INSTRUMENTATION_IMG_REPOSITORY="" \
  INSTRUMENTATION_IMG_TAG="" \
  INSTRUMENTATION_IMG_PULL_POLICY="" \
  COLLECTOR_IMG_REPOSITORY="" \
  COLLECTOR_IMG_TAG="" \
  COLLECTOR_IMG_PULL_POLICY="" \
  CONFIGURATION_RELOADER_IMG_REPOSITORY="" \
  CONFIGURATION_RELOADER_IMG_TAG="" \
  CONFIGURATION_RELOADER_IMG_PULL_POLICY="" \
  FILELOG_OFFSET_SYNCH_IMG_REPOSITORY="" \
  FILELOG_OFFSET_SYNCH_IMG_TAG="" \
  FILELOG_OFFSET_SYNCH_IMG_PULL_POLICY="" \
  make test-e2e
```

Note: Unsetting parameters like `CONTROLLER_IMG_REPOSITORY` explicitly (by setting them to an empty string) will lead to
the end-to-end test not setting those values when deploying via helm, so that the default value from the chart will be
used.  Otherwise, without `CONTROLLER_IMG_REPOSITORY=""` being present, the test suite will use
`CONTROLLER_IMG_REPOSITORY=operator-controller` (the image built from local sources) as the default setting.

#### Running End-to-End Tests on kind

To use kind for running the end-to-end tests, you need to create a kind cluster first.
The file <test-resources/kind-config.yaml> file can be used as a blueprint to create a cluster.

Before creating the cluster, the two hostPath settings in `test-resources/kind-config.yaml` need to be aligned with your
local file system structure.
(Alternatively, create a symbolic link from
`/Users/username/dash0/code/dash0-operator/test-resources/e2e-test-volumes/` to the actual path).
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
  the namespace `dash0-system`, and finally it deploys the Dash0 monitoring resource to `test-namespace`. This is a test
  scenario for instrumenting _existing_ workloads via the controller's reconcile loop.
* `test-resources/bin/test-scenario-02-operator-cr-aum.sh`: Deploys the operator to `dash0-system`, then the
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
  * Additional parameterization can be achieved via environment variables, for example:
      * To run the scenario with the images that have been built from the main branch and pushed to ghcr.io most
        recently:
        ```
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
          CONFIGURATION_RELOADER_IMG_PULL_POLICY="" \
          FILELOG_OFFSET_SYNCH_IMG_REPOSITORY=ghcr.io/dash0hq/filelog-offset-synch \
          FILELOG_OFFSET_SYNCH_IMG_TAG=main-dev \
          FILELOG_OFFSET_SYNCH_IMG_PULL_POLICY="" \
          test-resources/bin/test-scenario-01-aum-operator-cr.sh
        ```
      * To run the scenario with the helm chart from the official remote repository and the default images referenced in
        that chart (the Helm repository must have been installed beforehand):
        ```
        OPERATOR_HELM_CHART=dash0-operator/dash0-operator \
          OPERATOR_HELM_CHART_URL=https://dash0hq.github.io/dash0-operator \
          CONTROLLER_IMG_REPOSITORY="" \
          CONTROLLER_IMG_TAG="" \
          CONTROLLER_IMG_PULL_POLICY="" \
          INSTRUMENTATION_IMG_REPOSITORY="" \
          INSTRUMENTATION_IMG_TAG="" \
          INSTRUMENTATION_IMG_PULL_POLICY="" \
          COLLECTOR_IMG_REPOSITORY="" \
          COLLECTOR_IMG_TAG="" \
          COLLECTOR_IMG_PULL_POLICY="" \
          CONFIGURATION_RELOADER_IMG_REPOSITORY="" \
          CONFIGURATION_RELOADER_IMG_TAG="" \
          CONFIGURATION_RELOADER_IMG_PULL_POLICY="" \
          FILELOG_OFFSET_SYNCH_IMG_REPOSITORY="" \
          FILELOG_OFFSET_SYNCH_IMG_TAG="" \
          FILELOG_OFFSET_SYNCH_IMG_PULL_POLICY="" \
          test-resources/bin/test-scenario-01-aum-operator-cr.sh
        ```
        Note: Unsetting parameters like `CONTROLLER_IMG_REPOSITORY` explicitly (by setting them to an empty string) will
        lead to the scenario not setting those values when deploying via helm, so that the default value from the chart
        will actually be used. Otherwise, without `CONTROLLER_IMG_REPOSITORY=""` being present, the test script will use
        `CONTROLLER_IMG_REPOSITORY=operator-controller` (the image built from local sources) as the default setting.
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
