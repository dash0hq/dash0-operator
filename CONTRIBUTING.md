Contributing
============

## Prerequisites
- Go (version >= v1.22)
- Docker
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.
- [helm](https://helm.sh/docs/intro/install/)
- [helm unittest plug-in](https://github.com/helm-unittest/helm-unittest/tree/main)

## Deploying to a Local Cluster for Testing Purposes

Make sure your cluster has cert-manager running. If not, refer to https://cert-manager.io/docs/installation/.

E.g.:

```
helm repo add jetstack https://charts.jetstack.io --force-update
helm repo update
helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --version v1.14.5 --set installCRDs=true
```

This approach is suitable for deploying the operator to a cluster running locally on your machine, for example
via the Kubernetes support included in Docker Desktop.

Run `make docker-build` to build the container image locally, this will tag the image as
`operator-controller:latest`.

After that, you can deploy the operator to your cluster:

* Deploy the locally built image `operator-controller:latest` to the cluster: `make deploy-via-helm`
  (or `make deploy-via-kustomize`)
* Alternatively, deploy with images from a remote registry:
  `make deploy-via-helm IMG_REPOSITORY=ghcr.io/dash0hq/operator-controller IMG_TAG=main-dev IMG_PULL_POLICY="" INSTRUMENTATION_IMG_REPOSITORY=ghcr.io/dash0hq/instrumentation INSTRUMENTATION_IMG_TAG=main-dev INSTRUMENTATION_IMG_PULL_POLICY=""`
* The custom resource definition will automatically be installed when deploying the operator. However, you can also do
  that separately via kustomize if required via `make install`.

**NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as
admin.

**Undeploy the controller from the cluster:**

```sh
make undeploy-via-helm
```

or

```sh
make undeploy-via-kustomize
```

When undeploying the controllor, the same tool (helm vs. kustomiz) should be used as when deploying it.

This will also remove the custom resource definition. However, the custom resource definition can also be removed
separately via `make uninstall` without removing the operator.

## Run Tests

```
make test
```

This will run the go unit tests as well as the helm chart tests.

### End-to-End Tests

The end-to-end tests currently only support Kubernetes via Docker Desktop on Mac.

To run the end-to-end tests:
```
make test-e2e
```

The tests can also be run with remote images, like this:
```
BUILD_OPERATOR_CONTROLLER_IMAGE=false \
  BUILD_INSTRUMENTATION_IMAGE=false \
  IMG_REPOSITORY=ghcr.io/dash0hq/operator-controller \
  IMG_TAG=main-dev \
  IMG_PULL_POLICY="" \
  INSTRUMENTATION_IMG_REPOSITORY=ghcr.io/dash0hq/instrumentation \
  INSTRUMENTATION_IMG_TAG=main-dev \
  INSTRUMENTATION_IMG_PULL_POLICY="" \
  make test-e2e
```

The settings `BUILD_OPERATOR_CONTROLLER_IMAGE=false` plus `BUILD_INSTRUMENTATION_IMAGE=false` will skip building these
images locally, which is not required when using remote images from a registry.

The test suite can also be run with a Helm chart from a remote repository:

```
BUILD_OPERATOR_CONTROLLER_IMAGE=false \
  BUILD_INSTRUMENTATION_IMAGE=false \
  OPERATOR_HELM_CHART=dash0-operator/dash0-operator \
  OPERATOR_HELM_CHART_URL=https://dash0hq.github.io/dash0-operator \
  IMG_REPOSITORY="" \
  IMG_TAG="" \
  IMG_PULL_POLICY="" \
  INSTRUMENTATION_IMG_REPOSITORY="" \
  INSTRUMENTATION_IMG_TAG="" \
  INSTRUMENTATION_IMG_PULL_POLICY="" \
  make test-e2e
```

Note: Unsetting parameters like `IMG_REPOSITORY` explicitly (by setting them to an empty string) will lead to the
end-to-end test not setting those values when deploying via helm, so that the default value from the chart will be used.
Otherwise, without `IMG_REPOSITORY=""` being present, the test suite will use `IMG_REPOSITORY=operator-controller` (the
image built from local sources) as the default setting.

### Semi-Manual Test Scenarios

The e2e tests might sometimes not be the best tool to troubleshoot the operator, simply because they remove everything
they deploy in their `AfterAll`/`AfterEach` hooks. The scripts in `test-resources/bin` can be used for these cases:
* `test-resources/bin/test-roundtrip-01-aum-operator-cr.sh`: Deploys an application under monitoring (this is 
  abbreviated to "aum" in the name of the script) to the namespace `test-namespace`, then it deploys the operator to
  the namespace `dash-operator-system`, and finally it deploys the Dash0 custom resource to `test-namespace`. This is a
  test scenario for instrumenting _existing_ workloads via the controller's reconcile loop.   
* `test-resources/bin/test-roundtrip-02-operator-cr-aum.sh`: Deploys the operator to `dash0-operator-system`, then the
  Dash0 custom resource to namespace `test-namespace`, and finally an application under monitoring to the namespace
  `test-namespace`. This is a test scenario for instrumenting _new_ workloads at deploy time via the admission webhook.
* `test-resources/bin/test-cleanup.sh`: This script removes all resources created by the other scripts. **You should
  always this script after running any of the other scripts, when you are done with your tests, otherwise the e2e
  tests will fail the next time you start them.** Note that all above the scripts call this script at the beginning, so
  there is no need to clean up between individual invocations of the test scripts.
* All scripts will, by default, use the target namespace `test-namespace` and the workload type `deployment`. They all
  accept two command line parameters to override these defaults. For example, use 
  `test-resources/bin/test-roundtrip-01-aum-operator-cr.sh another-namespace replicaset` to run the scenario with 
  the target namespace `another-namespace` and a replica set workload.
  * Additional parameterization can be achieved via environment variables, for example:
      * To run the scenario with the images that have been built from the main branch and pushed to ghcr.io most 
        recently:
        ```
        BUILD_OPERATOR_CONTROLLER_IMAGE=false \
          BUILD_INSTRUMENTATION_IMAGE=false \
          IMG_REPOSITORY=ghcr.io/dash0hq/operator-controller \
          IMG_TAG=main-dev \
          IMG_PULL_POLICY="" \
          INSTRUMENTATION_IMG_REPOSITORY=ghcr.io/dash0hq/instrumentation \
          INSTRUMENTATION_IMG_TAG=main-dev \
          INSTRUMENTATION_IMG_PULL_POLICY="" \
          test-resources/bin/test-roundtrip-01-aum-operator-cr.sh
        ```
      * To run the scenario with the helm chart from the official remote repository and the default images referenced in
        that chart (the Helm repository must have been installed beforehand): 
        ```
        BUILD_OPERATOR_CONTROLLER_IMAGE=false \
          BUILD_INSTRUMENTATION_IMAGE=false \
          OPERATOR_HELM_CHART=dash0-operator/dash0-operator \
          IMG_REPOSITORY="" \
          IMG_TAG="" \
          IMG_PULL_POLICY="" \
          INSTRUMENTATION_IMG_REPOSITORY="" \
          INSTRUMENTATION_IMG_TAG="" \
          INSTRUMENTATION_IMG_PULL_POLICY="" \
          test-resources/bin/test-roundtrip-01-aum-operator-cr.sh
        ```
        Note: Unsetting parameters like `IMG_REPOSITORY` explicitly (by setting them to an empty string) will lead to
        the scenario not setting those values when deploying via helm, so that the default value from the chart will
        actually be used. Otherwise, without `IMG_REPOSITORY=""` being present, the test script will use 
        `IMG_REPOSITORY=operator-controller` (the image built from local sources) as the default setting.

## Make Targets

Run `make help` for more information on all potential `make` targets.
More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Contributing

No contribution guidelines are available at this point.
