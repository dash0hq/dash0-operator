# Dash0 Operator Helm Chart

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/dash0-operator)](https://artifacthub.io/packages/search?repo=dash0-operator)

This repository contains the [Helm](https://helm.sh/) chart for the Dash0 operator.

The Dash0 Operator makes observability for Kubernetes _easy_.
Simply install the operator into your cluster to get OpenTelemetry data flowing from your Kubernetes workloads to Dash0.

## Description

The Dash0 operator installs an OpenTelemetry collector into your cluster that sends data to your Dash0
ingress endpoint, with authentication already configured out of the box. Additionally, it will enable gathering
OpenTelemetry data from applications deployed to the cluster for a selection of supported runtimes, plus automatic log
collection and metrics.

The Dash0 operator is currently available as a technical preview.

## Supported Runtimes

Supported runtimes for automatic workload instrumentation:

* Node.js 18 and beyond

Other features like metrics and log collection are independent of the runtime of workloads.

## Prerequisites

- [Kubernetes](https://kubernetes.io/) >= 1.xx
- [Helm](https://helm.sh) >= 3.x, please refer to Helm's [documentation](https://helm.sh/docs/) for more information
  on installing Helm.

To use the operator, you will need provide two configuration values:
* `endpoint`: The URL of the Dash0 ingress endpoint backend to which telemetry data will be sent.
  This property is mandatory when installing the operator.
  This is the OTLP/gRPC endpoint of your Dash0 organization.
  The correct OTLP/gRPC endpoint can be copied fom https://app.dash0.com -> organization settings -> "Endpoints"
  -> "OTLP/gRPC".
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
  --set operator.dash0Export.apiEndpoint=REPLACE THIS WITH YOUR DASH0 API ENDPOINT \
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
  --set operator.dash0Export.apiEndpoint=REPLACE THIS WITH YOUR DASH0 API ENDPOINT \
  --set operator.dash0Export.secretRef.name=REPLACE THIS WITH THE NAME OF AN EXISTING KUBERNETES SECRET \
  --set operator.dash0Export.secretRef.key=REPLACE THIS WITH THE PROPERTY KEY IN THAT SECRET \
  dash0-operator \
  dash0-operator/dash0-operator
```

See the section
[Using a Kubernetes Secret for the Dash0 Authorization Token](#using-a-kubernetes-secret-for-the-dash0-authorization-token)
for more information on using a Kubernetes secrets with the Dash0 operator.

You can consult the chart's
[values.yaml](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/values.yaml) for a complete
list of available configuration settings.

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

**You can skip this step if you provided `--set operator.dash0Export.enabled=true` together with the endpoint and either
a token or a secret reference when running `helm install`.** In that case, proceed to the next section,
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

      apiEndpoint: https://api.....dash0.com # TODO needs to be replaced with the actual value, see below
```

Here is a list of configuration options for this resource:
* `spec.export.dash0.endpoint`: The URL of the Dash0 ingress endpoint to which telemetry data will be sent.
  This property is mandatory.
  Replace the value in the example above with the OTLP/gRPC endpoint of your Dash0 organization.
  The correct OTLP/gRPC endpoint can be copied fom https://app.dash0.com -> organization settings -> "Endpoints"
  -> "OTLP/gRPC".
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
* `spec.export.dash0.apiEndpoint`: The base URL of the Dash0 API to talk to. This is not where telemetry will be sent,
  but it is used for managing dashboards and check rules via the operator. This property is optional. The value needs
  to be the API endpoint of your Dash0 organization. The correct API endpoint can be copied fom https://app.dash0.com
  -> organization settings -> "Endpoints" -> "API". The correct endpoint value will always start with "https://api." and
  end in ".dash0.com". If this property is omitted, managing dashboards and check rules via the operator will not work.
* `spec.selfMonitoring.enabled`: An opt-out for self-monitoring for the operator.
  If enabled, the operator will collect self-monitoring telemetry and send it to the Dash0 Insights dataset of the
  configured Dash0 backend.
  This setting is optional, it defaults to true.
* `spec.kubernetesInfrastructureMetricsCollectionEnabled`: If enabled, the operator will collect Kubernetes
  infrastructure metrics.
  This setting is optional, it defaults to true.

After providing the required values (at least `endpoint` and `authorization`), save the file and apply the resource to
the Kubernetes cluster you want to monitor:

```console
kubectl apply -f dash0-operator-configuration.yaml
```

The Dash0 operator configuration resource is cluster-scoped, so a specific namespace should not be provided when
applying it.

### Enable Dash0 Monitoring For a Namespace

For _each namespace_ that you want to monitor with Dash0, enable monitoring by installing a _Dash0 monitoring
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
  There are three possible settings: `all`, `created-and-updated` and `none`.
  By default, the setting `all` is assumed.

  * `all`: If set to `all` (or omitted), the operator will:
      * instrument existing workloads in the target namespace (i.e. workloads already running in the namespace) when
        the Dash0 monitoring resource is deployed,
      * instrument existing workloads or update the instrumentation of already instrumented workloads in the target
        namespace when the Dash0 operator is first started or restarted (for example when updating the
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
        resource or restarting the Dash0 operator.

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

  Automatic workload instrumentation will automatically add tracing to your workloads. You can read more about what
  exactly this feature entails in the section [Automatic Workload Instrumentation](#automatic-workload-instrumentation).

* `spec.synchronizePersesDashboards`: A namespace-wide opt-out for synchronizing Perses dashboard resources found in the
  target namespace. If enabled, the operator will watch Perses dashboard resources in this namespace and create
  corresponding dashboards in Dash0 via the Dash0 API.
  See https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md#managing-dash0-dashboards
  for details. This setting is optional, it defaults to true.

* `spec.synchronizePrometheusRules`: A namespace-wide opt-out for synchronizing Prometheus rule resources found in the
  target namespace. If enabled, the operator will watch Prometheus rule resources in this namespace and create
  corresponding check rules in Dash0 via the Dash0 API.
  See https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md#managing-dash0-check-rules
  for details. This setting is optional, it defaults to true.

* `spec.prometheusScrapingEnabled`: A namespace-wide opt-out for Prometheus scraping for the target namespace.
  If enabled, the operator will configure its OpenTelemetry collector to scrape metrics from pods in the namespace
  of this Dash0Monitoring resource according to their prometheus.io/scrape annotations via the OpenTelemetry Prometheus
  receiver. This setting is optional, it defaults to true.

Here is an example file for a monitoring resource that sets the `spec.instrumentWorkloads` property
to `created-and-updated` and disables Perses dashboard synchronization, Prometheus rule synchronization as well as
Prometheus scraping:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0Monitoring
metadata:
  name: dash0-monitoring-resource
spec:
  instrumentWorkloads: created-and-updated
  synchronizePersesDashboards: false
  synchronizePrometheusRules: false
  prometheusScrapingEnabled: false
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
  --set operator.dash0Export.apiEndpoint=REPLACE THIS WITH YOUR DASH0 API ENDPOINT \
  --set operator.dash0Export.secretRef.name=dash0-authorization-secret \
  --set operator.dash0Export.secretRef.key=token \
  dash0-operator \
  dash0-operator/dash0-operator
```

The Helm chart will validate whether the referenced secret exists and contains the required key.
You can disable this check by setting `--set operator.dash0Export.disableSecretValidation=true`.
This can be useful if you want to render the manifests via the Helm chart without accessing a live Kubernetes cluster.

If you do not want to install the operator configuration resource via `helm install` but instead deploy it manually,
and use a secret reference for the auth token, the following example YAML file would work with the secret created
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

      apiEndpoint=https://api... # optional, see above
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

      apiEndpoint=https://api... # optional, see above
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

      apiEndpoint=https://api... # optional, see above
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

By default, self-monitoring is enabled for the Dash0 operator as soon as you deploy a Das0 operator
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

To upgrade the Dash0 Operator to a newer version, run the following commands:

```console
helm repo update dash0-operator
helm upgrade --namespace dash0-system dash0-operator dash0-operator/dash0-operator
```

## Uninstallation

To remove the Dash0 Operator from your cluster, run the following command:

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

## Automatic Workload Instrumentation

This section provides a quick behind-the-scenes glimpse into how the Dash0 operator's workload instrumentation for
tracing works, intended for the technically curious reader.
You can safely skip this section if you are not interested in the technical details.

Workloads in [monitored namespaces](#enable-dash0-monitoring-for-a-namespace) are instrumented by the Dash0 operator
to enable tracing for [supported runtimes](#supported-runtimes) out of the box.
This allows Dash0 users to avoid the hassle of manually adding the OpenTelemetry SDK to their applications—Dash0 takes
care of it automatically.

To achieve this, the Dash0 operator adds an
[init container](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) with the [Dash0 instrumentation
image](https://github.com/dash0hq/dash0-operator/tree/main/images/instrumentation) to the pod spec of workloads.

The instrumentation image contains the Dash0 OpenTelemetry distributions for all supported runtimes.
When the init container starts, it copies the Dash0 OpenTelemetry distributions to a dedicated shared volume mount that
has been added by the operator, so they are available in the target container's file system.

The operator also adds environment variables to the target container to ensure that the Dash0 OpenTelemetry distribution
has the correct configuration and will get activated at startup.

The activation of the Dash0 OpenTelemetry distribution happens via an `LD_PRELOAD` hook.
`LD_PRELOAD` is an environment variable that is evaluated by the
[dynamic linker/loader](https://man7.org/linux/man-pages/man8/ld.so.8.html) when a Linux executable starts.
It specifies a list of additional shared objects to be loaded before the actual code of the executable.
The Dash0 instrumentation image adds the Dash0 injector shared object to `LD_PRELOAD`.
The Dash0 injector is a small binary that adds additional environment variables to the running process by hooking into
the `getenv` function of the standard library.
For example, it sets (or appends to) `NODE_OPTIONS` to activate the
[Dash0 OpenTelemetry distribution for Node.js](https://github.com/dash0hq/opentelemetry-js-distribution) to collect 
tracing data from all Node.js workloads.

If you are curious, the source code for the injector is open source and can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/images/instrumentation/injector/src/dash0_injector.c).

## Scraping Prometheus endpoints

The Dash0 operator automatically scrapes Prometheus endpoints on pods labelled with the `prometheus.io/*` annotations as defined by the [Prometheus Helm chart](https://github.com/prometheus-community/helm-charts/tree/main/charts/prometheus#scraping-pod-metrics-via-annotations).

The supported annotations are:
* `prometheus.io/scrape`: Only scrape pods that have a value of `true`, except if `prometheus.io/scrape-slow` is set to `true` as well. Endpoints on pods annotated with this annotation are scraped every minute, i.e., scrape interval is 1 minute, unless `prometheus.io/scrape-slow` is also set to `true`.
* `prometheus.io/scrape-slow`: If set to `true`, enables scraping for the pod with scrape interval of 5 minutes. If both `prometheus.io/scrape` and `prometheus.io/scrape-slow` are annotated on a pod with both values set to `true`, the pod will be scraped every 5 minutes.
* `prometheus.io/scheme`: If the metrics endpoint is secured then you will need to set this to `https`.
* `prometheus.io/path`: Override the metrics endpoint path if it is not the default `/metrics`.
* `prometheus.io/port`: Override the metrics endpoint port if it is not the default `9102`.

To be scraped, a pod annotated with the `prometheus.io/scrape` or `prometheus.io/scrape-slow` annotations must belong to namespaces that are configured to be monitored by the Dash0 operator (see [Enable Dash0 Monitoring For a Namespace](#enable-dash0-monitoring-for-a-namespace) section).

The scraping of a pod from is executed from the same Kubernetes node as the pod's.

## Managing Dash0 Dashboards

You can manage your Dash0 dashboards via the Dash0 operator.

Pre-requisites for this feature:
* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have a Dash0 export configured with authorization
  (either `token` or `secret-ref`).
* The operator will only pick up Perses dashboard resources in namespaces that have a Dash0 monitoring resource
  deployed.
* The operator will not synchronize Perses dashboard resources in namespaces where the Dash0 monitoring resource
  has the setting `synchronizePersesDashboards` set to `false`. (This setting is optional and defaults to `true` when
  omitted.)

Furthermore, the custom resource definition for Perses dashboards needs to be installed in the cluster. There are two
ways to achieve this:
* Install the Perses dashboard custom resource definition with the following command:
```console
kubectl apply --server-side -f https://raw.githubusercontent.com/perses/perses-operator/main/config/crd/bases/perses.dev_persesdashboards.yaml
```
* Alternatively, install the full Perses operator: Go to <https://github.com/perses/perses-operator> and follow the
  installation instructions there.

With the prerequisites in place, you can manage Dash0 dashboards via the operator.
The Dash0 operator will watch for Perses dashboard resources in all namespaces that have a Dash0 monitoring resource
deployed, and synchronize the Perses dashboard resources with the Dash0 backend:
* When a new Perses dashboard resource is created, the operator will create a corresponding dashboard via Dash0's API.
* When a Perses dashboard resource is changed, the operator will update the corresponding dashboard via Dash0's API.
* When a Perses dashboard resource is deleted, the operator will delete the corresponding dashboard via Dash0's API.

The dashboards created by the operator will be in read-only mode in the Dash0 UI.

If the Dash0 operator configuration resource has the `dataset` property set, the operator will create the dashboards
in that dataset, otherwise they will be created in the `default` dataset.

When a Perses dashboard resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to the status of the Dash0 monitoring resource in the same namespace. This summary will also
show whether the dashboard had any validation issues or an error occurred during synchronization:
```
Perses Dashboard Synchronization Results:
  test-namespace/perses-dashboard-test:
    Synchronization Status:     successful
    Synchronized At:            2024-10-25T12:02:12Z
```

## Managing Dash0 Check Rules

You can manage your Dash0 check rules via the Dash0 operator.

Pre-requisites for this feature:
* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have a Dash0 export configured with authorization
  (either `token` or `secret-ref`).
* The operator will only pick up Prometheus rule resources in namespaces that have a Dash0 monitoring resource
  deployed.
* The operator will not synchronize Prometheus rule resources in namespaces where the Dash0 monitoring resource
  has the setting `synchronizePrometheusRules` set to `false`. (This setting is optional and defaults to `true` when
  omitted.)

Furthermore, the custom resource definition for Prometheus rules needs to be installed in the cluster. There are two
ways to achieve this:
* Install the Prometheus rules custom resource definition with the following command:
```console
kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.77.2/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml
```
* Alternatively, install the full kube-prometheus stack Helm chart: Go to
  <https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack> and follow the
  installation instructions there.

With the prerequisites in place, you can manage Dash0 check rules via the operator.
The Dash0 operator will watch for Prometheus rule resources in all namespaces that have a Dash0 monitoring resource
deployed, and synchronize the Prometheus rule resources with the Dash0 backend:
* When a new Prometheus rule resource is created, the operator will create corresponding check rules via Dash0's API.
* When a Prometheus rule resource is changed, the operator will update the corresponding check rules via Dash0's API.
* When a Prometheus rule resource is deleted, the operator will delete the corresponding check rules via Dash0's API.

Note that a Prometheus rule resource can contain multiple groups, and each of those groups can have multiple rules.
The Dash0 operator will create individual check rules for each rule in each group.

If the Dash0 operator configuration resource has the `dataset` property set, the operator will create the rules
in that dataset, otherwise they will be created in the `default` dataset.

Prometheus rules will be mapped to Dash0 check rules as follows:
* Each `rules` list item with an `alert` attribute in all `groups` will be converted to an individual check rule in
  Dash0.
* Rules that have the `record` attribute set instead of the `alert` attribute will be ignored.
* The name of the Dash0 check rule will be `"${group_name} - ${alert}"`, where `${group_name}` is the `name` attribute
  of the group in the Prometheus rule resource, and `${alert}` is value of the `alert` attribute.
* The `interval` attribute of the group will be used for the setting "Evaluate every" for each Dash0 check rule in that
  group.
* Other attributes in the Prometheus rule are converted to Dash0 check rule attributes as described in the table below.
* Some annotations and labels are interpreted by Dash0, these are described in the conversion table below.
  For example, to set the summary of the Dash0 check rule, add an annotation `summary` to the `rules` item in the
  Prometheus rule resource.
* If `expr` contains the token `$__threshold`, and neither annotation `threshold-degraded` nor `threshold-critical` is
  present, the rule will be considered invalid and will not be synchronized to Dash0.
* The group attribute `limit` is not supported by Dash0 and will be ignored.
* The group attribute `partial_response_strategy` is not supported by Dash0 and will be ignored.
* All labels (except for the ones explicitly mentioned in the conversion table below) will be listed under
  "Additional labels".
* All annotations (except for the ones explicitly mentioned in the conversion table below) will be listed under
  "Annotations".

| Prometheus alert rule attribute  | Dash0 Check rule field                                                                                                         | Notes                                                                                                                                                      |
|----------------------------------|--------------------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `alert`                          | Name of check rule, prefixed by the group name                                                                                 | must be a non-empty string                                                                                                                                 |
| `expr`                           | "Expression"                                                                                                                   | must be a non-empty string                                                                                                                                 |
| `interval` (from group)          | "Evaluate every"                                                                                                               |                                                                                                                                                            |
| `for`                            | "Grace periods"/"For"                                                                                                          | default: "0s"                                                                                                                                              |
| `keep_firing_for`                | "Grace periods"/"Keep firing for"                                                                                              | default: "0s"                                                                                                                                              |
| `annotations/summary`            | "Summary"                                                                                                                      |                                                                                                                                                            |
| `annotations/description`        | "Description"                                                                                                                  |                                                                                                                                                            |
| `annotations/threshold-degraded` | Will be used in place of the token `$__threshold` in the expression, to determine whether the check is _degraded_              | If present, needs to a be string that can be parsed to a float value according to the syntax described in https://go.dev/ref/spec#Floating-point_literals. |
| `annotations/threshold-critical` | Will be used in place of the token `$__threshold` in the expression, to determine whether the check is _critical_              | If present, needs to a be string that can be parsed to a float value according to the syntax described in https://go.dev/ref/spec#Floating-point_literals. |
| `annotations/*`                  | "Annotations"                                                                                                                  |
| `labels/*`                       | "Additional labels"                                                                                                            |

When a Prometheus ruls resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to the status of the Dash0 monitoring resource in the same namespace. This summary will also
show whether any of the rules had validation issues or errors occurred during synchronization:
```
Prometheus Rule Synchronization Results:
  my-namespace/prometheus-example-rules:
    Synchronization Status:        successful
    Synchronized At:               2024-10-25T11:59:49Z
    Alerting Rules Total:          3
    Synchronized Rules Total:      3
    Synchronized Rules:
      dash0/k8s - K8s Deployment replicas mismatch
      dash0/k8s - K8s pod crash looping
      dash0/collector - exporter send failed spans
    Invalid Rules Total:           0
    Synchronization Errors Total:  0
```
