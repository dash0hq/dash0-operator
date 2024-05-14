Contributing
============

## Prerequisites
- Go (version >= v1.22)
- Docker
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

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
`dash0-operator-controller:latest`.

When deploying an image that has only been built locally (and has not been pushed to any remote registry), the file
`config/manager/manager.yaml` needs to be modified locally: Find the `controller-manager` `Deployment` specification
and `spec.template.spec.containers[command]` and add the attribute `imagePullPolicy: Never`, like this:

```
      # ...
      containers:
        - command:
            - /manager
          args:
            - --leader-elect
          image: dash0-operator-controller:latest
          name: manager

          imagePullPolicy: Never
      # ...
```

Alternatively, you can also push the image to a remote registry, specified by the variable `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/dash0-operator:tag
```

Then the mentioned modification of `config/manager/manager.yaml` is not necessary. 
In this case your test cluster needs to be configured to have pull access for the image in the remote registry.

After that, you can deploy the operator to your cluster:

* Install the CRDs into the cluster: `make install`
* Deploy the locally built image `dash0-operator-controller:latest` to the cluster: `make deploy`
* Alternatively, deploy the image pushed to the remote registry with the image specified by `IMG`: `make deploy IMG=<some-registry>/dash0-operator:tag`

**NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as
admin.

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Run Tests

```
make test
```

```
make test-e2e
```

## Make Targets

Run `make help` for more information on all potential `make` targets.
More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Contributing

No contribution guidelines are available at this point.

