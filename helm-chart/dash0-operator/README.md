# Dash0 Kubernetes Operator Helm Chart

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/dash0-operator)](https://artifacthub.io/packages/search?repo=dash0-operator)

This repository contains the [Helm](https://helm.sh/) chart for the Dash0 Kubernetes operator.

The Dash0 Kubernetes Operator makes observability for Kubernetes _easy_.
Simply install the operator into your cluster to get OpenTelemetry data flowing from your Kubernetes workloads to Dash0.

## Description

The Dash0 Kubernetes operator installs an OpenTelemetry collector into your cluster that sends data to your Dash0
ingress endpoint, with authentication already configured out of the box. Additionally, it will enable gathering
OpenTelemetry data from applications deployed to the cluster for a selection of supported runtimes, plus automatic log
collection and metrics.

The Dash0 Kubernetes operator is currently available as a technical preview.

Supported runtimes for automatic instrumentation:

* Node.js 18 and beyond

## Prerequisites

- [Kubernetes](https://kubernetes.io/) >= 1.xx
- [Helm](https://helm.sh) >= 3.x, please refer to Helm's [documentation](https://helm.sh/docs/) for more information
  on installing Helm.

To use the operator, you will need provide two configuration values:
* `endpoint`: The URL of the Dash0 ingress endpoint backend to which telemetry data will be sent.
  This property is mandatory when installing the operator.
  This is the OTLP/gRPC endpoint of your Dash0 organization.
  The correct OTLP/gRPC endpoint can be copied fom https://app.dash0.com -> organization settings -> "Endpoints".
  Note that the correct endpoint value will always start with `ingress.` and end in `dash0.com:4317`.
  Including a protocol prefix (e.g. `https://`) is optional.
* Either `token` or `secretRef`: Exactly one of these two properties needs to be provided when installing the operator.
    * `token`: This is the Dash0 authorization token of your organization.
      The authorization token for your Dash0 organization can be copied from https://app.dash0.com -> organization
      settings -> "Auth Tokens".
      The prefix `Bearer ` must *not* be included in the value.
      Note that when you provide a token, it will be rendered verbatim into a Kubernetes ConfigMap object.
      Anyone with API access to the Kubernetes cluster will be able to read the value.
      Use a secret reference and a Kubernetes secret if you want to avoid that.
    * `secretRef`: A reference to an existing Kubernetes secret in the Dash0 operator's namespace.
      The secret needs to contain the Dash0 authorization token.
      See below for details on how exactly the secret should be created and configured.

## Installation

Before installing the operator, add the Dash0 operator's Helm repository as follows:

```console
helm repo add dash0-operator https://dash0hq.github.io/dash0-operator
helm repo update dash0-operator
```

Now you can install the operator into your cluster via Helm with the following command:

```console
helm install \
  --namespace dash0-system \
  --create-namespace \
  --set operator.dash0Export.enabled=true \
  --set operator.dash0Export.endpoint=REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT \
  --set operator.dash0Export.token=REPLACE THIS WITH YOUR DASH0 AUTH TOKEN \
  dash0-operator \
  dash0-operator/dash0-operator
```

Instead of providing the auth token directly, you can also use a secret reference:

```console
helm install \
  --namespace dash0-system \
  --create-namespace \
  --set operator.dash0Export.enabled=true \
  --set operator.dash0Export.endpoint=REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT \
  --set operator.dash0Export.secretRef.name=REPLACE THIS WITH THE NAME OF AN EXISTING KUBERNETES SECRET \
  --set operator.dash0Export.secretRef.key=REPLACE THIS WITH THE PROPERTY KEY IN THAT SECRET \
  dash0-operator \
  dash0-operator/dash0-operator
```

See the section
[Using a Kubernetes Secret for the Dash0 Authorization Token](#using-a-kubernetes-secret-for-the-dash0-authorization-token)
for more information on using a Kubernetes secrets with the Dash0 operator.

Last but not least, you can also install the operator without providing a Dash0 backend configuration:

```console
helm install \
  --namespace dash0-system \
  --create-namespace \
  dash0-operator \
  dash0-operator/dash0-operator
```

However, you will need to create a Dash0 operator configuration resource later that provides the backend connection
settings.
That is, providing `--set operator.dash0Export.enabled=true` and the other backend-related settings when running
`helm install` is simply a shortcut to deploy the Dash0 operator configuration resource automatically at startup.

On its own, the operator will not do much.
To actually have the operator monitor your cluster, two more things need to be set up:
1. a [Dash0 backend connection](#configuring-the-dash0-backend-connection) has to be configured and 
2. monitoring workloads has to be [enabled per namespace](#enable-dash0-monitoring-for-a-namespace).

Both steps are described in the following sections.

## Configuration

### Configuring the Dash0 Backend Connection

You can skip this step if you provided `--set operator.dash0Export.enabled=true` together with the endpoint and either
a token or a secret reference when running `helm install`. In that case, proceed to the next section, 
[Enable Dash0 Monitoring For a Namespace](#enable-dash0-monitoring-for-a-namespace).

Otherwise, configure the backend connection now by creating a file `dash0-operator-configuration.yaml` with the
following content:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  export:
    dash0:
      # Replace this value with the actual OTLP/gRPC endpoint of your Dash0 organization.
      endpoint: ingress... # TODO needs to be replaced with the actual value, see below

      authorization:
        # Provide the Dash0 authorization token as a string via the token property:
        token: auth_... # TODO needs to be replaced with the actual value, see below
```

You need to provide two configuration settings:
* `spec.export.dash0.endpoint`: The URL of the Dash0 ingress endpoint backend to which telemetry data will be sent.
  This property is mandatory.
  Replace the value in the example above with the OTLP/gRPC endpoint of your Dash0 organization.
  The correct OTLP/gRPC endpoint can be copied fom https://app.dash0.com -> organization settings -> "Endpoints".
  Note that the correct endpoint value will always start with `ingress.` and end in `dash0.com:4317`.
  Including a protocol prefix (e.g. `https://`) is optional.
* `spec.export.dash0.authorization.token` or `spec.export.dash0.authorization.secretRef`: Exactly one of these two
  properties needs to be provided.
  Providing both will cause a validation error when installing the Dash0Monitoring resource.
    * `spec.export.dash0.authorization.token`: Replace the value in the example above with the Dash0 authorization token
      of your organization.
      The authorization token for your Dash0 organization can be copied from https://app.dash0.com -> organization 
      settings -> "Auth Tokens".
      The prefix `Bearer ` must *not* be included in the value.
      Note that the value will be rendered verbatim into a Kubernetes ConfigMap object.
      Anyone with API access to the Kubernetes cluster will be able to read the value.
      Use the `secretRef` property and a Kubernetes secret if you want to avoid that.
    * `spec.export.dash0.authorization.secretRef`: A reference to an existing Kubernetes secret in the Dash0 operator's
      namespace.
      See the section [Using a Kubernetes Secret for the Dash0 Authorization Token](#using-a-kubernetes-secret-for-the-dash0-authorization-token)
      for an example file that uses a `secretRef`.
      The secret needs to contain the Dash0 authorization token.
      See below for details on how exactly the secret should be created and configured.
      Note that by default, Kubernetes secrets are stored _unencrypted_, and anyone with API access to the Kubernetes
      cluster will be able to read the value.
      Additional steps are required to make sure secret values are encrypted.
      See https://kubernetes.io/docs/concepts/configuration/secret/ for more information on Kubernetes secrets.

After providing the required values, save the file and apply the resource to the Kubernetes cluster you want to monitor:

```console
kubectl apply -f dash0-operator-configuration.yaml
```

The Dash0 operator configuration resource is cluster-scoped, so a specific namespace should not be provided when
applying it.

### Enable Dash0 Monitoring For a Namespace

For _each namespace_ that you want to monitor with Dash0, enable workload monitoring by installing a _Dash0 monitoring
resource_ into that namespace:

Create a file `dash0-monitoring.yaml` with the following content:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0Monitoring
metadata:
  name: dash0-monitoring-resource
```

Save the file and apply the resource to the namespace you want to monitor.
For example, if you want to monitor workloads in the namespace `my-nodejs-applications`, use the following command:

```console
kubectl apply --namespace my-nodejs-applications -f dash0-monitoring.yaml
```

If you want to monitor the `default` namespace with Dash0, use the following command:

```console
kubectl apply -f dash0-monitoring.yaml
```

The Dash0 monitoring resource supports additional configuration settings:

* `spec.instrumentWorkloads`: A namespace-wide opt-out for workload instrumentation for the target namespace.
  There are threepossible settings: `all`, `created-and-updated` and `none`.
  By default, the setting `all` is assumed.

  * `all`: If set to `all` (or omitted), the operator will:
      * instrument existing workloads in the target namespace (i.e. workloads already running in the namespace) when
        the Dash0 monitoring resource is deployed,
      * instrument existing workloads or update the instrumentation of already instrumented workloads in the target
        namespace when the Dash0 Kubernetes operator is first started or restarted (for example when updating the
        operator),
      * instrument new workloads in the target namespace when they are deployed, and
      * instrument changed workloads in the target namespace when changes are applied to them.
  Note that the first two actions (instrumenting existing workloads) will result in restarting the pods of the
  affected workloads.

  * `created-and-updated`: If set to `created-and-updated`, the operator will not instrument existing workloads in the
    target namespace.
    Instead, it will only:
      * instrument new workloads in the target namespace when they are deployed, and
      * instrument changed workloads in the target namespace when changes are applied to them.
        This setting is useful if you want to avoid pod restarts as a side effect of deploying the Dash0 monitoring
        resource or restarting the Dash0 Kubernetes operator.

  * `none`: You can opt out of instrumenting workloads entirely by setting this option to `none`.
     With `spec.instrumentWorkloads: none`, workloads in the target namespace will never be instrumented to send
     telemetry to Dash0.

  If this setting is omitted, the value `all` is assumed and new/updated as well as existing Kubernetes workloads
  will be intrumented by the operator to send telemetry to Dash0, as described above.

  More fine-grained per-workload control over instrumentation is available by setting the label
  `dash0.com/enable=false` on individual workloads.

  The behavior when changing this setting for an existing Dash0 monitoring resource is as follows:
    * When this setting is updated to `spec.instrumentWorkloads=all` (and it had a different value before): All existing
      uninstrumented workloads will be instrumented.
    * When this setting is updated to `spec.instrumentWorkloads=none` (and it had a different value before): The
      instrumentation will be removed from all instrumented workloads.
    * Updating this value to `spec.instrumentWorkloads=created-and-updated` has no immediate effect; existing
      uninstrumented workloads will not be instrumented, existing instrumented workloads will not be uninstrumented.
      Newly deployed or updated workloads will be instrumented from the point of the configuration change onwards as
      described above.

Here is an example file for a monitoring resource that sets the `spec.instrumentWorkloads` property 
to `created-and-updated`:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0Monitoring
metadata:
  name: dash0-monitoring-resource
spec:
  # Opt-out settings for particular use cases. The default value is "all". Other possible values are
  # "created-and-updated" and "none".
  instrumentWorkloads: created-and-updated
```

### Using a Kubernetes Secret for the Dash0 Authorization Token

If you want to provide the Dash0 authorization token via a Kubernetes secret instead of providing the token as a string,
create the secret in the namespace where the Dash0 operator is installed.
If you followed the guide above, the name of that namespace is `dash0-system`.
The authorization token for your Dash0 organization can be copied from https://app.dash0.com -> organization settings 
-> "Auth Tokens".
You can freely choose the name of the secret and the key of the token within the secret.

Create the secret by using the following command:

```console
kubectl create secret generic \
  dash0-authorization-secret \
  --namespace dash0-system \
  --from-literal=token=auth_...your-token-here...
```

With this example command, you would create a secret with the name `dash0-authorization-secret` in the namespace
`dash0-system`.
If you installed (or plan to install) the operator into a different namespace, replace the `--namespace` parameter
accordingly.

The name of the secret as well as the key of the token value within the secret must be provided when referencing the 
secret during `helm install`, or in the YAML file for the Dash0 operator configuration resource (in the `secretRef`
property).

For creating the operator configuration resource with `helm install`, the command would look like this, assuming the 
secret has been created as shown above:

```console
helm install \
  --namespace dash0-system \
  --set operator.dash0Export.enabled=true \
  --set operator.dash0Export.endpoint=REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT \
  --set operator.dash0Export.secretRef.name=dash0-authorization-secret \
  --set operator.dash0Export.secretRef.key=token \
  dash0-operator \
  dash0-operator/dash0-operator
```

If you do not want to install the operator configuration resource via `helm install` but instead deploy it manually,
and use a secret reference for the auth token, the following example YAML file would work work with the secret created
above:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  export:
    dash0:
      endpoint: ingress... # TODO REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT

      authorization:
        secretRef:
          name: dash0-authorization-secret
          key: token
```

When deploying the operator configuration resource via `kubectl`, the following defaults apply:
* If the `name` property is omitted, the name `dash0-authorization-secret` will be assumed.
* If the `key` property is omitted, the key `token` will be assumed.

With these defaults in mind, the `secretRef` could have also been written as follows:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  export:
    dash0:
      endpoint: ingress... # TODO needs to be replaced with the actual value, see above

      authorization:
        secretRef: {}
```

Note: There are no defaults when using `--set operator.dash0Export.secretRef.name` and 
`--set operator.dash0Export.secretRef.key` with `helm install`, so for that approach the values must always be
provided explicitly.

Note that by default, Kubernetes secrets are stored _unencrypted_, and anyone with API access to the Kubernetes cluster
will be able to read the value.
Additional steps are required to make sure secret values are encrypted, if that is desired.
See https://kubernetes.io/docs/concepts/configuration/secret/ for more information on Kubernetes secrets.

### Dash0 Dataset Configuration

Use the `spec.export.dash0.dataset` property to configure the dataset that should be used for the telemetry data.
By default, data will be sent to the dataset `default`.
Here is an example for a configuration that uses a different Dash0 dataset:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  export:
    dash0:
      endpoint: ingress... # see above

      dataset: my-custom-dataset # This optional setting determines the Dash0 dataset to which telemetry will be sent.

      authorization: # see above
        ...
```

### Exporting Data to Other Observability Backends

Instead of `spec.export.dash0` in the Dash0 operator configuration resource, you can also provide `spec.export.http` or
`spec.export.grpc` to export telemetry data to arbitrary OTLP-compatible backends, or to another local OpenTelemetry
collector.

Here is an example for HTTP:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  export:
    http:
      endpoint: ... # provide the OTLP HTTP endpoint of your observability backend here
      headers: # you can optionally provide additional headers, for example for authorization
        - name: X-My-Header
          value: my-value
      encoding: json # optional, can be "json" or "proto", defaults to "proto"
```

Here is an example for gRPC:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  export:
    grpc:
      endpoint: ... # provide the OTLP gRPC endpoint of your observability backend here
      headers: # you can optionally provide additional headers, for example for authorization
        - name: X-My-Header
          value: my-value
```

You can combine up to three exporters (i.e. Dash0 plus gRPC plus HTTP) to send data to multiple backends. This allows
sending the same data to two or three targets simultaneously. At least one exporter has to be defined. More than three
exporters cannot be defined. Listing two or more exporters of the same type (i.e. providing `spec.export.grpc` twice)
is not supported.

Here is an example that combines three exporters:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  export:
    dash0:
      endpoint: ingress... # TODO needs to be replaced with the actual value, see above
      authorization:
        token: auth_... # TODO needs to be replaced with the actual value, see above
    http:
      endpoint: ... # provide the OTLP HTTP endpoint of your observability backend here
    grpc:
      endpoint: ... # provide the OTLP gRPC endpoint of your observability backend here
```

#### Exporting Telemetry to Different Backends Per Namespace

Exporting telemetry to different backends per namespace is not yet implemented.
This feature will be available in a future release.
The export settings are available on the per-namespace resource (the Dash0 monitoring resource) to prepare for this
feature.

**Important**: For that reason, having different export settings on the Dash0 monitoring resources in your cluster is
currently strongly discouraged -- the export settings of one Dash0 monitoring resource would overwrite the settings of
the other Dash0 monitoring resources, leading to non-deterministic behavior.
For now, we recommend to only set the `export` attribute on the cluster's Dash0 operator configuration resource.

This restriction will be lifted once exporting telemetry to different backends per namespace is implemented.

## Disable Self-Monitoring

By default, self-monitoring is enabled for the Dash0 Kubernetes operator as soon as you deploy a Das0 operator
configuration resource with an export.
That means, the operator will send self-monitoring telemetry to the Dash0 Insights dataset of the configured backend.
Disabling self-monitoring is available as a setting on the Dash0 operator configuration resource.
Dash0 does not recommend to disable the operator's self-monitoring.

Here is an example with self-monitoring disabled:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration-resource
spec:
  selfMonitoring:
    enabled: false
  export:
    # ... see above for details on the export settings
```

## Disable Dash0 Monitoring For a Namespace

If you want to stop monitoring a namespace with Dash0, remove the Dash0 monitoring resource from that namespace.
For example, if you want to stop monitoring workloads in the namespace `my-nodejs-applications`, use the following
command:

```console
kubectl delete --namespace my-nodejs-applications Dash0Monitoring dash0-monitoring-resource
```

or, alternatively, by using the `dash0-monitoring.yaml` file created earlier:

```console
kubectl delete --namespace my-nodejs-applications -f dash0-monitoring.yaml
```

## Upgrading

To upgrade the Dash0 Kubernetes Operator to a newer version, run the following commands:

```console
helm repo update dash0-operator
helm upgrade --namespace dash0-system dash0-operator dash0-operator/dash0-operator
```

## Uninstallation

To remove the Dash0 Kubernetes Operator from your cluster, run the following command:

```
helm uninstall dash0-operator --namespace dash0-system
```

Depending on the command you used to install the operator, you may need to use a different Helm release name or
namespace.

This will also automatically disable Dash0 monitoring for all namespaces by deleting the Dash0 monitoring resources in all
namespaces.
Optionally, remove the namespace that has been created for the operator:

```
kubectl delete namespace dash0-system
```

If you choose to not remove the namespace, you might want to consider removing the secret with the Dash0 authorization
token (if such a secret has been created):

```console
kubectl delete secret --namespace dash0-system dash0-authorization-secret
```

If you later decide to install the operator again, you will need to perform the [initial configuration](#configuration)
steps again:

1. set up a [Dash0 backend connection](#configuring-the-dash0-backend-connection) and
2. enable Dash0 monitoring in each namespace you want to monitor, see [Enable Dash0 Monitoring For a Namespace](#enable-dash0-monitoring-for-a-namespace).
