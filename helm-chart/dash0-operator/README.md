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

## Supported Runtimes

Supported runtimes for automatic workload instrumentation:

* Java 8+
* Node.js 16+
* .NET

Other features like metrics and log collection are independent of the runtime of workloads.

## Prerequisites

- [Kubernetes](https://kubernetes.io/) 1.25.16 or later.
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
  --wait \
  --namespace dash0-system \
  --create-namespace \
  --set operator.dash0Export.enabled=true \
  --set operator.dash0Export.endpoint=REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT \
  --set operator.dash0Export.apiEndpoint=REPLACE THIS WITH YOUR DASH0 API ENDPOINT \
  --set operator.dash0Export.token=REPLACE THIS WITH YOUR DASH0 AUTH TOKEN \
  --set operator.clusterName=REPLACE THIS WITH YOUR THE NAME OF THE CLUSTER (OPTIONAL) \
  dash0-operator \
  dash0-operator/dash0-operator
```

Instead of providing the auth token directly, you can also use a secret reference:

```console
helm install \
  --wait \
  --namespace dash0-system \
  --create-namespace \
  --set operator.dash0Export.enabled=true \
  --set operator.dash0Export.endpoint=REPLACE THIS WITH YOUR DASH0 INGRESS ENDPOINT \
  --set operator.dash0Export.apiEndpoint=REPLACE THIS WITH YOUR DASH0 API ENDPOINT \
  --set operator.dash0Export.secretRef.name=REPLACE THIS WITH THE NAME OF AN EXISTING KUBERNETES SECRET \
  --set operator.dash0Export.secretRef.key=REPLACE THIS WITH THE PROPERTY KEY IN THAT SECRET \
  --set operator.clusterName=REPLACE THIS WITH YOUR THE NAME OF THE CLUSTER (OPTIONAL) \
  dash0-operator \
  dash0-operator/dash0-operator
```

See the section
[Using a Kubernetes Secret for the Dash0 Authorization Token](#using-a-kubernetes-secret-for-the-dash0-authorization-token)
for more information on using a Kubernetes secrets with the Dash0 operator.

You can consult the chart's
[values.yaml](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/values.yaml) file for a
complete list of available configuration settings.

See the section
[Notes on Creating the Operator Configuration Resource Via Helm](#notes-on-creating-the-operator-configuration-resource-via-helm)
for more information on providing Dash0 export settings via Helm and how it affects manual changes to the operator
configuration resource.

Last but not least, you can also install the operator without providing a Dash0 backend configuration:

```console
helm install \
  --wait \
  --namespace dash0-system \
  --create-namespace \
  dash0-operator \
  dash0-operator/dash0-operator
```

However, you will need to create a Dash0 operator configuration resource later that provides the backend connection
settings.
That is, providing `--set operator.dash0Export.enabled=true` and the other backend-related settings when running
`helm install` is simply a shortcut to deploy the Dash0 operator configuration resource automatically at startup.

On its own, the operator will only collect Kubernetes metrics.
To actually have the operator properly monitor your workloads, two more things need to be set up:
1. a [Dash0 backend connection](#configuring-the-dash0-backend-connection) has to be configured (unless you did that
   already with the Helm values `operator.dash0Export.*`), and
2. monitoring namespaces and their workloads to collect logs, traces and metrics has to be
   [enabled per namespace](#enable-dash0-monitoring-for-a-namespace).

Both steps are described in the following sections.

### Support for Prometheus CRDs

If you would like to enable support for Prometheus CRDs
1. ensure the CRDs (`ServiceMonitor`, `PodMonitor`, `ScrapeConfig`) are installed in the cluster
2. include `--set operator.prometheusCrdSupportEnabled=true` when running `helm install`

Alternatively, if you are creating the operator configuration resource manually, refer to the [Configuration](#configuration)
section below.

The operator supports the following CRDs
- `ServiceMonitor`
- `PodMonitor`
- `ScrapeConfig` with `kubernetesSDConfigs`

The scraping of endpoints requiring authorization is currently not supported, but will be added in an upcoming release.

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

  clusterName: my-kubernetes-cluster # optional, see below
```

Here is a list of configuration options for this resource:
* <a href="#operatorconfigurationresource.spec.export.dash0.endpoint"><span id="operatorconfigurationresource.spec.export.dash0.endpoint">`spec.export.dash0.endpoint`</span></a>: The URL of the Dash0 ingress endpoint to which telemetry data will be sent.
  This property is mandatory.
  Replace the value in the example above with the OTLP/gRPC endpoint of your Dash0 organization.
  The correct OTLP/gRPC endpoint can be copied fom https://app.dash0.com -> organization settings -> "Endpoints"
  -> "OTLP/gRPC".
  Note that the correct endpoint value will always start with `ingress.` and end in `dash0.com:4317`.
  Including a protocol prefix (e.g. `https://`) is optional.
* `spec.export.dash0.authorization.token` or `spec.export.dash0.authorization.secretRef`:
  Exactly one of these two properties needs to be provided.
  Providing both will cause a validation error when installing the Dash0Monitoring resource.
    * <a href="#operatorconfigurationresource.spec.export.dash0.authorization.token"><span id="operatorconfigurationresource.spec.export.dash0.authorization.token">`spec.export.dash0.authorization.token`</span></a>:
      Replace the value in the example above with the Dash0 authorization token of your organization.
      The authorization token for your Dash0 organization can be copied from https://app.dash0.com -> organization
      settings -> "Auth Tokens".
      The prefix `Bearer ` must *not* be included in the value.
      Note that the value will be rendered verbatim into a Kubernetes ConfigMap object.
      Anyone with API access to the Kubernetes cluster will be able to read the value.
      Use the `secretRef` property and a Kubernetes secret if you want to avoid that.
    * <a href="#operatorconfigurationresource.spec.export.dash0.authorization.secretRef"><span id="operatorconfigurationresource.spec.export.dash0.authorization.secretRef">`spec.export.dash0.authorization.secretRef`</span></a>:
      A reference to an existing Kubernetes secret in the Dash0 operator's namespace.
      See the section [Using a Kubernetes Secret for the Dash0 Authorization Token](#using-a-kubernetes-secret-for-the-dash0-authorization-token)
      for an example file that uses a `secretRef`.
      The secret needs to contain the Dash0 authorization token.
      See below for details on how exactly the secret should be created and configured.
      Note that by default, Kubernetes secrets are stored _unencrypted_, and anyone with API access to the Kubernetes
      cluster will be able to read the value.
      Additional steps are required to make sure secret values are encrypted.
      See https://kubernetes.io/docs/concepts/configuration/secret/ for more information on Kubernetes secrets.
* <a href="#operatorconfigurationresource.spec.export.dash0.apiEndpoint"><span id="operatorconfigurationresource.spec.export.dash0.apiEndpoint">`spec.export.dash0.apiEndpoint`</span></a>:
  The base URL of the Dash0 API to talk to. This is not where telemetry will be sent, but it is used for managing
  dashboards, check rules, synthetic checks and views via the operator.
  This property is optional.
  The value needs to be the API endpoint of your Dash0 organization.
  The correct API endpoint can be copied fom https://app.dash0.com -> organization settings -> "Endpoints" -> "API".
  The correct endpoint value will always start with "https://api." and end in ".dash0.com".
  If this property is omitted, managing dashboards, check rules, synthetic checks and views via the operator will not
  work.
* <a href="#operatorconfigurationresource.spec.selfMonitoring.enabled"><span id="operatorconfigurationresource.spec.selfMonitoring.enabled">`spec.selfMonitoring.enabled`</span></a>:
  An opt-out for self-monitoring for the operator.
  If enabled, the operator will collect self-monitoring telemetry and send it to the configured Dash0 backend.
  This setting is optional, it defaults to true.
* <a href="#operatorconfigurationresource.spec.kubernetesInfrastructureMetricsCollection.enabled"><span id="operatorconfigurationresource.spec.kubernetesInfrastructureMetricsCollection.enabled">`spec.kubernetesInfrastructureMetricsCollection.enabled`</span></a>:
  If enabled, the operator will collect Kubernetes infrastructure metrics.
  This setting is optional, it defaults to true; unless `telemetryCollection.enabled` is set to `false`, then
  `kubernetesInfrastructureMetricsCollection.enabled` defaults to `false` as well.
  It is a validation error to set `telemetryCollection.enabled=false` and
  `kubernetesInfrastructureMetricsCollection.enabled=true` at the same time.
* <a href="#operatorconfigurationresource.spec.collectPodLabelsAndAnnotations.enabled"><span id="operatorconfigurationresource.spec.collectPodLabelsAndAnnotations.enabled">`spec.collectPodLabelsAndAnnotations.enabled`</span></a>:
  If enabled, the operator will collect all Kubernetes pod labels and annotations and convert them to resource attributes
  for all spans, log records and metrics.
  The resulting resource attributes are prefixed with `k8s.pod.label.` or `k8s.pod.annotation.` respectively.
  This setting is optional, it defaults to true; unless `telemetryCollection.enabled` is set to `false`, then
  `collectPodLabelsAndAnnotations.enabled` defaults to `false` as well.
  It is a validation error to set `telemetryCollection.enabled=false` and `collectPodLabelsAndAnnotations.enabled=true`
  at the same time.
* <a href="#operatorconfigurationresource.spec.clusterName"><span id="operatorconfigurationresource.spec.clusterName">`spec.clusterName`</span></a>:
  If set, the value will be added as the resource attribute `k8s.cluster.name` to all telemetry.
  This setting is optional. By default, `k8s.cluster.name` will not be added to telemetry.
* <a href="#operatorconfigurationresource.spec.telemetryCollection.enabled"><span id="operatorconfigurationresource.spec.telemetryCollection.enabled">`spec.telemetryCollection.enabled`</span></a>:
  An opt-out switch for all telemetry collection, and to avoid having the operator deploy OpenTelemetry collectors in
  the cluster.
  This setting is optional, it defaults to true (that is, by default, OpenTelemetry collectors will be deployed and
  telemetry will be collected).
  If telemetry collection is disabled via this switch, the operator will not collect any telemetry, in particular it
  will not deploy any OpenTelemetry collectors in the cluster.
  This is useful if you want to use the operator for infrastructure-as-code (e.g. to synchronize dashboards & check
  rules), but do not want it to deploy the OpenTelemetry collector.
  Note that setting this to false does not disable the operator's self-monitoring telemetry, use the setting
  `spec.selfMonitoring.enabled` to disable self-monitoring if required (self-monitoring does not require an
  OpenTelemetry collector).
  Also note that this setting is not exposed via Helm, i.e. if you want to set this to false you need to deploy the
  operator configuration resource manually, i.e. omit the Helm value `operator.dash0Export.enabled` or set it to false,
  then deploy an operator configuration resource via `kubectl apply -f` or similar.
* <a href="#operatorconfigurationresource.spec.prometheusCrdSupport.enabled"><span id="operatorconfigurationresource.spec.prometheusCrdSupport.enabled">`spec.prometheusCrdSupport.enabled`</span></a>:
  A flag controlling whether support for Prometheus CRDs will be enabled. This setting is optional and the default is `false`.
  Setting it to `true` and having at least one namespace with `prometheusScraping` enabled, will deploy the OpenTelemetry
  target-allocator and update the `prometheusreceiver` in the OpenTelemetry collectors, so they query the allocator for targets
  to be scraped.

After providing the required values (at least `endpoint` and `authorization`), save the file and apply the resource to
the Kubernetes cluster you want to monitor:

```console
kubectl apply -f dash0-operator-configuration.yaml
```

The Dash0 operator configuration resource is cluster-scoped, so a specific namespace should not be provided when
applying it.

Note: All configuration options available in the operator configuration resource can also be configured when letting the
Helm chart auto-create this resource, as explained in the section [Installation](#installation). You can consult the
chart's [values.yaml](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/values.yaml) file
for a complete list of available configuration settings.

### Notes on Creating the Operator Configuration Resource Via Helm

Providing the backend connection settings to the operator via Helm parameters is a _convenience mechanism_ to get
monitoring started right away when installing the operator.
Setting `operator.dash0Export.enabled` to `true` and providing other necessary `operator.dash0Export.*` values like
`operator.dash0Export.endpoint` will instruct the operator manager to create an operator configuration resource with the
provided values at startup.
This automatically created operator configuration resource will have the name
`dash0-operator-configuration-auto-resource`.

If an operator configuration resource with any other name already exists in the cluster (e.g. a manually created
operator configuration resource), the operator will treat this as an error and refuse to overwrite the existing operator
configuration resource with the values provided via Helm.

If an operator configuration resource with the name `dash0-operator-configuration-auto-resource` already exists in the
cluster (e.g. a previous startup of the operator manager has created the resource), the operator manager will
update/overwrite this resource with the values provided via Helm.

Manual changes to the `dash0-operator-configuration-auto-resource` are permissible for quickly experimenting with
configuration changes, without an operator restart, but you need to be aware that they will be overwritten with the
settings provided via Helm the next time the operator manager pod is restarted.
Possible reasons for a restart of the operator manager pod include upgrading to a new operator version, running
`helm upgrade ...  dash0-operator dash0-operator/dash0-operator`, or Kubernetes moving the operator manager pod to a
different node.

For this reason, when using this feature, it is recommended to treat the _Helm values_ as the source of truth for the
operator configuration.
Any changes you want to be permanent should be applied via Helm and the `operator.dash0Export.*` settings.

If you would rather retain manual control over the operator configuration resource, you should omit any
`operator.dash0Export.*` Helm values and create and manage the operator configuration resource manually (that is, via
kubectl, ArgoCD etc.).

### Enable Dash0 Monitoring For a Namespace

*Note:* By default, when enabling Dash0 monitoring for a namespace, all workloads in this namespace will be restarted
to apply the Dash0 instrumentation.
If you want to avoid this, set the `instrumentWorkloads` property in the monitoring resource spec to
`created-and-updated`.
See below for more information on the `instrumentWorkloads` modes.

For _each namespace_ that you want to monitor with Dash0, enable monitoring by installing a _Dash0 monitoring
resource_ into that namespace:

Create a file `dash0-monitoring.yaml` with the following content:

```yaml
apiVersion: operator.dash0.com/v1beta1
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

Note: Even when no monitoring resources has been installed and no namespace is being monitored by Dash0, the Dash0
operator's collector will collect Kubernetes infrastructure metrics that are not namespace scoped, like node-related
metrics. The only prerequisite for this is an [operator configuration](#configuring-the-dash0-backend-connection) with
export settings.

### Additional Configuration Per Namespace

The Dash0 monitoring resource supports additional configuration settings:

* <a href="#monitoringresource.spec.instrumentWorkloads.mode"><span id="monitoringresource.spec.instrumentWorkloads.mode"><span id="monitoringresource.spec.instrumentWorkloads">`spec.instrumentWorkloads.mode`</span></span></a>:
  A namespace-wide configuration for the workload instrumentation strategy for the target namespace.
  There are three possible settings: `all`, `created-and-updated` and `none`.
  By default, the setting `all` is assumed; unless there is an operator
  configuration resource with `telemetryCollection.enabled=false`, then the setting `none` is assumed by default.
  Note that `spec.instrumentWorkloads.mode` is the path for this setting starting with version `v1beta1` of the Dash0
  monitoring resource; when using `v1alpha1`, the path is `spec.instrumentWorkloads`.

  * `all`: If set to `all`, the operator will:
      * instrument existing workloads in the target namespace (i.e. workloads already running in the namespace) when
        the Dash0 monitoring resource is deployed,
      * instrument existing workloads or update the instrumentation of already instrumented workloads in the target
        namespace when the Dash0 operator is first started or updated to a newer version,,
      * instrument new workloads in the target namespace when they are deployed, and
      * instrument changed workloads in the target namespace when changes are applied to them.
    **Note that the first two actions (instrumenting existing workloads) will result in restarting the pods of the
    affected workloads.**
    Use `created-and-updated` if you want to avoid pod restarts.

  * `created-and-updated`: If set to `created-and-updated`, the operator will not instrument existing workloads in the
    target namespace.
    Instead, it will only:
      * instrument new workloads in the target namespace when they are deployed, and
      * instrument changed workloads in the target namespace when changes are applied to them.
    This setting is useful if you want to avoid pod restarts as a side effect of deploying the Dash0 monitoring
    resource or restarting the Dash0 operator.

  * `none`: You can opt out of instrumenting workloads entirely by setting this option to `none`.
     With `spec.instrumentWorkloads: none`, workloads in the target namespace will never be instrumented to emit
     telemetry.

  If this setting is omitted, the value `all` is assumed and new/updated as well as existing Kubernetes workloads
  will be intrumented by the operator to emit telemetry, as described above.
  There is one exception to this rule: If there is an operator configuration resource with
  `telemetryCollection.enabled=false`, then the default setting is `none` instead of `all`, and no workloads will be
  instrumented by the Dash0 operator.

  More fine-grained per-workload control over instrumentation is available by setting the label
  `dash0.com/enable=false` on individual workloads, see
  [Disabling Auto-Instrumentation for Specific Workloads](#disabling-auto-instrumentation-for-specific-workloads).

  The behavior when changing this setting for an existing Dash0 monitoring resource is as follows:
    * When this setting is updated to `spec.instrumentWorkloads=all` (and it had a different value before): All existing
      uninstrumented workloads will be instrumented. Their pods will be restarted to apply the instrumentation.
    * When this setting is updated to `spec.instrumentWorkloads=none` (and it had a different value before): The
      instrumentation will be removed from all instrumented workloads.
      Their pods will be restarted to remove the instrumentation.
      (After this change, the operator will no longer instrument any workloads nor will it restart any pods.)
    * Updating this value to `spec.instrumentWorkloads=created-and-updated` has no immediate effect; existing
      uninstrumented workloads will not be instrumented, existing instrumented workloads will not be uninstrumented.
      Newly deployed or updated workloads will be instrumented from the point of the configuration change onwards as
      described above.

  Automatic workload instrumentation will automatically add tracing to your workloads.
  You can read more about what exactly this feature entails in the section
  [Automatic Workload Instrumentation](#automatic-workload-instrumentation).

* <a href="#monitoringresource.spec.instrumentWorkloads.labelSelector"><span id="monitoringresource.spec.instrumentWorkloads.labelSelector">`spec.instrumentWorkloads.labelSelector`</span></a>:
  A custom Kubernetes [label selector](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors)
  for controlling the workload instrumentation on the level of individual workloads, see
  [Using a Custom Label Selector to Control Auto-Instrumentation](#using-a-custom-label-selector-to-control-auto-instrumentation).

* <a href="#monitoringresource.spec.instrumentWorkloads.traceContext.propagators"><span id="monitoringresource.spec.instrumentWorkloads.traceContext.propagators">`spec.instrumentWorkloads.traceContext.propagators`</span></a>:
  When set, the operator will add the environment variable `OTEL_PROPAGATORS` to all instrumented workloads in the
  target namespace.
  This environment variable determines which trace context propagation headers an OTel SDK uses.
  Setting this can be useful if the workloads in this namespace interact with services do not use the W3C trace context
  standard header `traceparent` for trace context propagation, but for example AWS X-Ray (`X-Amzn-Trace-Id`).
  The value of the setting will be validated, it needs to be a comma-separated list of valid propagators.
  See <https://opentelemetry.io/docs/languages/sdk-configuration/general/#otel_propagators> for more information.

  When the option is no set, the operator will not set the environment variable `OTEL_PROPAGATORS`.
  If the option is set in the monitoring resource at some point and then later removed again, the operator will remove
  the environment variable from instrumented workloads if and only if the value of the environment variable matches the
  previously used setting in the monitoring resource.
  This is done to prevent accidentally removing an `OTEL_PROPAGATORS` environment variable that has been set manually
  and not by the operator.
  (For that purpose, the previous setting is stored in the monitoring resource's status.)

* <a href="#monitoringresource.spec.logCollection.enabled"><span id="monitoringresource.spec.logCollection.enabled">`spec.logCollection.enabled`</span></a>:
  A namespace-wide opt-out for collecting pod logs via the `filelog` receiver.
  If enabled, the operator will configure its OpenTelemetry collector to watch the log output of all pods in the
  namespace and send the resulting log records to Dash0.
  This setting is optional, it defaults to true; unless there is an operator configuration resource with
  `telemetryCollection.enabled=false`, then log collection is off by default.
  It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and
  `logCollection.enabled=true` in any monitoring resource at the same time.

* <a href="#monitoringresource.spec.prometheusScraping.enabled"><span id="monitoringresource.spec.prometheusScraping.enabled">`spec.prometheusScraping.enabled`</span></a>:
  A namespace-wide opt-out for Prometheus scraping for the target namespace.
  If enabled, the operator will configure its OpenTelemetry collector to scrape metrics from pods in the namespace
  of this Dash0Monitoring resource according to their prometheus.io/scrape annotations via the OpenTelemetry Prometheus
  receiver.
  In addition, if the operator configuration resource has `prometheusCrdSupport.enabled=true`, the collectors will scrape metrics
  from endpoints defined in Prometheus CRs (`ServiceMonitor`, `PodMonitor`, `ScrapeConfig`) present in this namespace.
  This setting is optional, it defaults to true; unless there is an operator configuration resource with
  `telemetryCollection.enabled=false`, then Prometheus scraping is off by default.
  It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and
  `prometheusScraping.enabled=true` in any monitoring resource at the same time.
  Note that the collection of OpenTelemetry-native metrics is not
  affected by setting `prometheusScraping.enabled=false` for a namespace.

* <a href="#monitoringresource.spec.filter"><span id="monitoringresource.spec.filter">`spec.filter`</span></a>:
  An optional custom filter configuration to drop some of the collected telemetry before sending it to the configured
  telemetry backend.
  Filters for a specific telemetry object type (e.g. spans) are lists of
  [OTTL](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/pkg/ottl/README.md) expressions.
  If at least one of the one conditions of a list evaluates to true, the object will be dropped.
  (That is, conditions are implicitly connected by a logical OR.)
  The configuration structure is identical to the configuration of the OpenTelemetry collector's
  [filter processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/filterprocessor/README.md).
  One difference to the filter processor is that the filter rules configured in a Dash0 monitoring resource will only be
  applied to the telemetry collected in the namespace the monitoring resource is installed in.
  Telemetry from other namespaces is not affected.
  Existing configurations for the filter processor can be copied and pasted without syntactical changes.
    * `spec.filter.traces.span`:
       A list of OTTL conditions for filtering spans.
       All spans where at least one condition evaluates to true will be dropped.
	   (That is, conditions are implicitly connected by a logical OR.)
    * `spec.filter.traces.spanevent`:
       A list of OTTL conditions for filtering span events.
       All span events where at least one condition evaluates to true will be dropped.
	   If all span events for a span are dropped, the span will be left intact.
    * `spec.filter.metrics.metric`:
       A list of OTTL conditions for filtering metrics.
       All metrics where at least one condition evaluates to true will be dropped.
    * `spec.filter.metrics.datapoint`:
       A list of OTTL conditions for filtering individual data points of metrics.
       All data points where at least one condition evaluates to true will be dropped.
	   If all datapoints for a metric are dropped, the metric will also be dropped.
    * `spec.filter.logs.log_record`:
       A list of OTTL conditions for filtering log records.
       All log records where at least one condition evaluates to true will be dropped.
  This setting is optional, by default, no filters are applied.
  It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and set
  filters in any monitoring resource at the same time.

  Note that although `error_mode` can be specified per namespace, the filter conditions will be aggregated into one
  single filter processor in the resulting OpenTelemetry collector configuration; if different error modes are
  specified in different namespaces, the "most severe" error mode will be used (propagate > ignore > silent).

* <a href="#monitoringresource.spec.transform"><span id="monitoringresource.spec.transform">`spec.transform`</span></a>:
  An optional custom transformation configuration that will be applied to the collected telemetry before sending it to
  the configured telemetry backend.
  Transformations for a specific telemetry signal (e.g. traces, metrics, logs) are lists of
  [OTTL](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/pkg/ottl/README.md) statements.
  All telemetry for the respective signal will be routed through all transformation statements.
  The statements are executed in the order they are listed.
  The configuration structure is identical to the configuration of the OpenTelemetry collector's
  [transform processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/transformprocessor/README.md).
  Both the
  [basic configuration style](#https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/transformprocessor/README.md#basic-config),
  and the
  [advanced configuration style](#https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/transformprocessor/README.md#advanced-config)
  of the transform processor are supported.
  One difference to the transform processor is that the transform rules configured in a Dash0 monitoring resource will
  only be applied to the telemetry collected in the namespace the monitoring resource is installed in.
  Telemetry from other namespaces is not affected.
  If both `spec.filter` and `spec.transform` are configured, the filtering for a given signal (traces, metrics, logs)
  will be executed _before_ the transform processor.
  (That is, you cannot assume that transformations have already been applied when writing filter rules.)
  Existing configurations for the transform processor can be copied and pasted without syntactical changes.
    * `spec.transform.trace_statements`:
      A list of OTTL statements (or a list of groups in the advanced config style) for transforming trace telemetry.
    * `spec.transform.metric_statements`:
      A list of OTTL statements (or a list of groups in the advanced config style) for filtering metric telemetry.
    * `spec.transform.log_statements`:
      A list of OTTL statements (or a list of groups in the advanced config style) for filtering log telemetry.
  This setting is optional, by default, no transformations are applied.
  It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and set
  transforms in any monitoring resource at the same time.

  Note that although `error_mode` can be specified per namespace, the transform statements will be aggregated into one
  single transform processor in the resulting OpenTelemetry collector configuration; if different error modes are
  specified in different namespaces, the "most severe" error mode will be used (propagate > ignore > silent).

* <a href="#monitoringresource.spec.synchronizePersesDashboards"><span id="monitoringresource.spec.synchronizePersesDashboards">`spec.synchronizePersesDashboards`</span></a>:
  A namespace-wide opt-out for synchronizing Perses dashboard resources found in the target namespace.
  If enabled, the operator will watch Perses dashboard resources in this namespace and create corresponding dashboards
  in Dash0 via the Dash0 API.
  More fine-grained per-resource control over synchronization is available by setting the label
  `dash0.com/enable=false` on individual Perses dashboard resources.
  See [Managing Dash0 Dashboards](#managing-dash0-dashboards) for details. This setting is optional, it defaults to
  true.

* <a href="#monitoringresource.spec.synchronizePrometheusRules"><span id="monitoringresource.spec.synchronizePrometheusRules">`spec.synchronizePrometheusRules`</span></a>:
  A namespace-wide opt-out for synchronizing Prometheus rule resources found in the target namespace.
  If enabled, the operator will watch Prometheus rule resources in this namespace and create corresponding check rules
  in Dash0 via the Dash0 API.
  More fine-grained per-resource control over synchronization is available by setting the label
  `dash0.com/enable=false` on individual Prometheus rule resources.
  See [Managing Dash0 Check Rules](#managing-dash0-check-rules) for details.
  This setting is optional, it defaults to true.

Here is comprehensive example for a monitoring resource which
* sets the instrumentation mode to `created-and-updated`,
* disables Prometheus scraping,
* sets a couple of filters for all five telemetry object types,
* applies transformations to limit the length of span attributes, datapoint attributes, and log attributes
  (with the metric transform using the advanced transform config style),
* disables Perses dashboard synchronization, and
* disables Prometheus rule synchronization.

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0Monitoring
metadata:
  name: dash0-monitoring-resource
spec:
  instrumentWorkloads: created-and-updated

  prometheusScraping:
    enabled: false

  filter:
    traces:
      span:
      - 'attributes["http.route"] == "/ready"'
      - 'attributes["http.route"] == "/metrics"'
      spanevent:
      - 'attributes["grpc"] == true'
      - 'IsMatch(name, ".*grpc.*")'
    metrics:
      metric:
      - 'name == "k8s.replicaset.available"'
      - 'name == "k8s.replicaset.desired"'
      datapoint:
      - 'metric.type == METRIC_DATA_TYPE_SUMMARY'
      - 'resource.attributes["service.name"] == "my_service_name"'
    logs:
      log_records:
      - 'IsMatch(body, ".*password.*")'
      - 'severity_number < SEVERITY_NUMBER_WARN'

  transform:
    trace_statements:
    - 'truncate_all(span.attributes, 1024)'
    metric_statements:
      - conditions:
        - 'metric.type == METRIC_DATA_TYPE_SUM'
        statements:
        - 'truncate_all(datapoint.attributes, 1024)'
    log_statements:
    - 'truncate_all(log.attributes, 1024)'

  synchronizePersesDashboards: false

  synchronizePrometheusRules: false
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
  --wait \
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

      apiEndpoint: https://api... # optional, see above
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

      apiEndpoint: https://api... # optional, see above
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

      apiEndpoint: https://api... # optional, see above
```

### Configure Metrics Collection

By default, the operator collects metrics as follows:
* The operator collects node, pod, container, and volume metrics from the API server on
  [kubelets](https://kubernetes.io/docs/concepts/architecture/#kubelet)
  via the
  [Kubelet Stats Receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/kubeletstatsreceiver/README.md),
  cluster-level metrics from the Kubernetes API server via the
  [Kubernetes Cluster Receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/k8sclusterreceiver/README.md),
  and system metrics from the underlying nodes via the
  [Host Metrics Receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/hostmetricsreceiver/README.md).
  Collecting these metrics can be disabled per cluster by setting
  [`kubernetesInfrastructureMetricsCollection.enabled: false`](#operatorconfigurationresource.spec.kubernetesInfrastructureMetricsCollection.enabled)
  in the Dash0 operator configuration resource (or setting the value
  `operator.kubernetesInfrastructureMetricsCollectionEnabled` to `false` when deploying the operator configuration
  resource via the Helm chart).
* Namespace-scoped metrics (e.g. metrics related to a workload running in a specific namespace) will only be collected
  if the namespace is monitored, that is, there is a Dash0 monitoring resource in that namespace.
* The Dash0 operator scrapes Prometheus endpoints on pods annotated with the `prometheus.io/*` annotations in monitored
  namespaces, as described in the section [Scraping Prometheus endpoints](#scraping-prometheus-endpoints).
  This can be disabled per namespace by explicitly setting
  [`prometheusScraping.enabled: false`](#monitoringresource.spec.prometheusScraping.enabled) in the Dash0 monitoring
  resource.
* Metrics which are not namespace-scoped (for example node metrics like `k8s.node.*` or host metrics like
  `system.cpu.utilization`) will always be collected, unless metrics collection is disabled globally for the cluster
  (`kubernetesInfrastructureMetricsCollection.enabled: false`, see above).
  An operator configuration resource with [export settings](#configuring-the-dash0-backend-connection) has to be present
  in the cluster, otherwise no metrics collection takes place.

Disabling or enabling individual metrics via configuration is not supported.

#### Resource Attributes for Prometheus Scraping

When the operator scrapes Prometheus endpoints on pods, it does not have access to all the same metadata that is
available to the OpenTelemetry SDK in an instrumented application.
For that reason, resource attributes including the service name might be different.
The operator makes an effort to derive reasonable resource attributes.

The _service name_ is derived as follows:

1. If the scraped service provides the `target_info` metric with a `service_name` attribute, that service name will be
   used.
   See https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/compatibility/prometheus_and_openmetrics.md#resource-attributes
2. If no service name was found via (1.), but the pod has the `app.kubernetes.io/name` label, the value of that label
   will be used as the service name.
   If the service name is derived from this pod label, the following pod labels (if present) will be mapped to resource
   attributes as well:
    * `app.kubernetes.io/version` to `service.version`
    * `app.kubernetes.io/part_of` to `service.namespace`
3. If no service name was found via (1.) or (2.), no service name is set for the Prometheus metrics.
   If there is other telemetry (tracing, logs, OpenTelemetry metrics) for the same pod in Dash0, and these other signals
   carry a service name, the Prometheus metrics for this pod will be associated with that service name as well.
   This is actually the recommendation for handling Prometheus metrics for most users:
   If you do not have specific reasons to set the service name for Prometheus metrics, the best option is usually to not
   use the `target_info` metric or the `app.kubernetes.io/name` pod label, but let the Dash0 backend aggregate all
   telemetry into one service (to see everything in one place), with the service name taken from other signals than
   Prometheus metrics.

Note that in contrast to [Resource Attributes for Workloads via Labels and Annotations](#specifying-additional-resource-attributes-for-workloads-via-labels-and-annotations),
Prometheus scraping can only see pod labels, not workload level (deployment, daemonset, ...) labels.

### Providing a Filelog Offset Volume

The operator's collector uses the filelog receiver to read pod log files for monitored workloads.
When the collector is restarted (which can happen for various reasons, for example to apply configuration changes),
it is important that the filelog receiver can continue reading the log files from where it left off.
If the filelog receiver started to read all log files from the beginning again after a restart, log records would be
duplicated, that is, they would appear multiple times in Dash0.
For that purpose, the filelog receiver stores the log file offsets in persistent storage.
By default, the offsets are stored in a config map in the operator's namespace.
For small- to medium-sized clusters, this is usually sufficient, and it requires no additional configuration by users.
For larger clusters or clusters with many short-lived pods, we recommend providing a persistent volume for storing
offsets.

Any persistent volume that is accessible from the collector pods can be used for this purpose.

Here is an example with a `hostPath` volume (see also https://kubernetes.io/docs/concepts/storage/volumes/#hostpath
for considerations around using `hostPath` volumes):
```yaml
operator:
  collectors:
   filelogOffsetSyncStorageVolume:
     name: filelogreceiver-offsets
     hostPath:
       path: /data/dash0-operator/offset-storage
       type: DirectoryOrCreate
```

The directory in the hostPath volume will automatically be created with the correct permissions so that the
OpenTelemetry collector container can write to it.

Here is another example based on persistent volume claims. (This assumes that a PersistentVolumeClaim named
`offset-storage-claim exists`.) See also
https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims and
https://kubernetes.io/docs/concepts/storage/persistent-volumes/#reclaiming.
```yaml
operator:
  collectors:
    filelogOffsetSyncStorageVolume:
      name: filelogreceiver-offsets
      persistentVolumeClaim:
        claimName: offset-storage-claim
```

**Important:** Since this volume is needed by a Daemonset run by the Dash0 operator, the PersistentVolumeClaim needs
to be set with the `ReadWriteMany` access mode.

Using a volume instead of the default config map approach is also helpful if you have webhooks in your cluster which
process every config map update.

Known examples for this are:
* The [Open Police Agent](https://www.openpolicyagent.org/), in particular the
  [OPA gatekeeeper](https://github.com/open-policy-agent/gatekeeper).
  When operating the OPA gatekeeper in the same cluster as the Dash0 operator, it is highly recommended to use a volume
  for filelog offsets.
  Using the default config map filelog offset storage in clusters with the OPA gatekeeper can lead to severe performance
  issues, since the default config map for filelog offsets is updated very frequently. This can cause the OPA gatekeeper
  to consume a lot of CPU and memory resources, potentially even leading to OOMKills of the OPA gatekeeper.
* [AKS](https://azure.microsoft.com/products/kubernetes-service) clusters with the
  [Azure Policy add-on](https://learn.microsoft.com/azure/aks/use-azure-policy): A managed instance of the OPA
  gatekeeper (see above) is installed by the Azure Policy add-on for AKS, i.e. the OPA gatekeeper is active in AKS
  cluster that have enabled the Azure Policy add-on. It is highly recommended to use a volume for filelog offsets for
  AKS clusters with the Azure Policy add-on.
* The
  [Kyverno admission controller](https://kyverno.io/docs/introduction/how-kyverno-works/#kubernetes-admission-controls).
  When operating Kyverno in the same cluster as the Dash0 operator, it is highly recommended to either use a volume for
  filelog offsets, or to [exclude](https://kyverno.io/docs/installation/customization/#resource-filters) ConfigMaps (or
  all resource types) in the Dash0 operator's namespace from Kyverno's processing.
  Leaving Kyverno processing in place and using the config map filelog offset storage can lead to severe performance
  issues, since the default config map for filelog offsets is updated very frequently.
  This can cause Kyverno to consume a lot of CPU and memory resources, potentially even leading to OOMKills of the
  Kyverno admission controller.

### Using cert-manager

When installing the Helm chart, it generates TLS certificates on the fly for all components that need certificates
(the operator's webhook service and its metrics service).
This also happens when updating the operator, e.g. via `helm upgrade`.
This is the default behavior, and it works out of the box without the need to manage certificates with a third-party
solution.

As an alternative, you can use [`cert-manager`](https://cert-manager.io/) to manage the certificates.
To let `cert-manager` handle TLS certificates, provide the following additional settings when applying the Helm chart:

```yaml
operator:

  # Settings for using cert-manager instead of auto-generating TLS certificates.
  certManager:

    # This disables the usage of automatically generated certificates by the Helm chart.
    # If this is set to true, the assumption is that certificates are managed by
    # cert-manager (see https://cert-manager.io/), and the other settings shown in this
    # snippet (certManager.secretName, certManager.certManagerAnnotations) become
    # required settings. It is recommended to set webhookService.name as well.
    useCertManager: true

    # The name of the secret used by cert-manager for the certificate.
    # The provided name must match the `secretName` in the `cert-manager.io/v1.Certificate`
    # resource's spec (see below).
    # Note: This secret is created and managed by cert-manager, you do not need to create
    # it manually.
    secretName: dash0-operator-certificate-secret

    # A map of additional annotations that are added to all Kubernetes resources that need
    # the certificate.
    # Usually this will be a single `cert-manager.io/inject-ca-from` annotation with the
    # namespace and name of the Certificate resource (see below), but other annotations
    # are possible, see https://cert-manager.io/docs/reference/annotations/.
    certManagerAnnotations:
      cert-manager.io/inject-ca-from: "dash0-system/dash0-operator-serving-certificate"

  webhookService:
    # A name override for the webhook service, defaults to dash0-operator-webhook-service.
    # The name of the webhook service must match the DNS names provided in the
    # cert-manager's Certificate resource.
    # For that reason it is recommended to set this value when using cert-manager, as it
    # guarantees that the names match, even if the Dash0 operator helm chart would change
    # the default name of the webhook service in a future release.
    name: dash0-operator-webhook-service-name
```

You will also need to provide `cert-manager` resources, for example a `Certificate` and an `Issuer`.
Explaining all configuration options of `cert-manager` is out of scope for this documentation.
Please refer to the [cert-manager documentation](https://cert-manager.io/docs/) for details on how to install and
configure `cert-manager` in your cluster.

The following annotated example is a minimal configuration that matches the Dash0 operator configuration snippet
shown above.
Both configuration snippets assume that the Dash0 operator is installed in the default `dash0-system` namespace,
and the Certificate and Issuer resource are also deployed into that namespace.

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:

  # NOTE: This value, together with the namespace this resource is deployed into, must
  # match the value of the cert-manager.io/inject-ca-from annotation provided in the
  # certManager.certManagerAnnotations setting, i.e. this matches the following annotation:
  #   cert-manager.io/inject-ca-from: "dash0-system/dash0-operator-serving-certificate"
  name: dash0-operator-serving-certificate

  labels:
    app.kubernetes.io/name: certificate
    app.kubernetes.io/instance: serving-cert
    app.kubernetes.io/component: certificate

spec:

  # NOTE: Provide all DNS names that are used to access the webhook service.
  # The service names usually follow the patterns <service-name>.<namespace>.svc and
  # <service-name>.<namespace>.svc.cluster.local.
  # If you use a custom name for the Dash0 operator's webhook service (recommended), the
  # first part of the DNS names must match that name.
  # If you use the default name for the webhook service, the first part of the DNS names
  # must match the default name (dash0-operator-webhook-service).
  # If the Dash0 operator is installed into a different namespace, you need to change the
  # ".dash0-system." part of the DNS names accordingly.
  dnsNames:
  - dash0-operator-webhook-service-name.dash0-system.svc
  - dash0-operator-webhook-service-name.dash0-system.svc.cluster.local

  issuerRef:
    kind: Issuer

    # NOTE: Must match the name of the Issuer resource provided below.
    name: dash0-operator-selfsigned-issuer

  # NOTE: The secret name must match the operator.certManager.secretName setting above
  # provided to the Dash0 operator Helm chart. This secret is created and managed by
  # cert-manager, you do not need to create it manually.
  secretName: dash0-operator-certificate-secret

---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:

  # NOTE: This issuer's name must match the issuerRef.name in the Certificate resource above.
  name: dash0-operator-selfsigned-issuer

  labels:
    app.kubernetes.io/name: certificate
    app.kubernetes.io/instance: serving-cert
    app.kubernetes.io/component: certificate
spec:

  # There are many options for configuring an Issuer, this example uses a simple self-signed
  # issuer. Make sure to configure the issuer according to your requirements.
  selfSigned: {}
```

### Controlling On Which Nodes the Operator's Collector Pods Are Scheduled

#### Allow Scheduling on Tainted Nodes

The operator uses a Kubernetes daemonset to deploy the OpenTelemetry collector on each node; to collect telemetry from
that node and workloads running on that node.
If you use [taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) on certain nodes,
Kubernetes will not schedule any pods there, preventing the daemonset collector pods to be present on these nodes.
You can allow the daemonset collector pods to be scheduled there by configuring tolerations matching your taints for the
collector pods.
Tolerations can be configured as follows:
```
operator:
  collectors:
    daemonSetTolerations:
    - key: key1
      operator: Equal
      value: value1
      effect: NoSchedule
    - key: key2
      operator: Exists
      effect: NoSchedule
```

In the same fashion, tolerations can also be configured for the Dash0 operator manager (Helm value
`operator.tolerations`), the OpenTelemetry collector deployment for collecting cluster metrics
(Helm value `operator.collectors.deploymentTolerations`) and the OpenTelemetry target-allocator deployment (Helm value `operator.targetAllocator.tolerations`).

Changing Helm settings while the operator is already running requires a `helm upgrade`/`helm upgrade --reuse-values` or
similar to take effect.

#### Preventing Operator Scheduling on Specific Nodes

All the pods deployed by the operator have a default node anti-affinity for the `dash0.com/enable=false` node label.
That is, if you add the `dash0.com/enable=false` label to a node, none of the pods owned by the operator will be
scheduled on that node.

**IMPORTANT:** This includes the daemonset that the operator will set up to receive telemetry from the pods, which might
leads to situations in which instrumented pods cannot send telemetry because the local node does not have a daemonset
collector pod.
In other words, if you want to monitor workloads with the Dash0 operator and use the `dash0.com/enable=false` node
anti-affinity, make sure that the workloads you want to monitor have the same anti-affinity:

```yaml
# Add this to your workloads
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: "dash0.com/enable"
            operator: "NotIn"
            values: ["false"]
```

#### Custom Node Affinity

The node affinity for all pods deployed by the operator can be customized by setting the `nodeAffinity` field for the respective component.

```yaml
operator:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
        - matchExpressions:
            - key: "a-custom-label"
              operator: "In"
              values: ["custom_value"]

  collectors:
    daemonSetNodeAffinity: <custom_node_affinity>
    deploymentNodeAffinity: <custom_node_affinity>

  targetAllocator:
    nodeAffinity: <custom_node_affinity>
```

### Disabling Auto-Instrumentation for Specific Workloads

In namespaces that are Dash0-monitoring enabled, all workloads are automatically instrumented for tracing and to improve
OpenTelemetry resource attributes.
This process will modify the Pod spec, e.g. by adding environment variables, Kubernetes labels and an init container.
The modifications are described in detail in the section
[Automatic Workload Instrumentation](#automatic-workload-instrumentation).

You can disable these workload modifications for specific workloads by setting the label `dash0.com/enable: "false"` in
the top level metadata section of the workload specification.

Note: The actual label selector for enabling or disabling workload modification can be customized in the Dash0 
monitoring resource.
The label `dash0.com/enable: "false"` can be used when no custom label selector has been configured in the Dash0
monitoring resource, see [Using a Custom Label Selector to Control Auto-Instrumentation](#using-a-custom-label-selector-to-control-auto-instrumentation).

Here is an example for a deployment with this label:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deployment
  labels:
    app: my-deployment-app
    dash0.com/enable: "false"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-deployment-app
  template:
    metadata:
      labels:
        app: my-deployment-app
    spec:
      containers:
        - name: my-deployment-app
          image: "some-image:latest"
```

The label can also be applied by using `kubectl`:
```
kubectl label --namespace $YOUR_NAMESPACE --overwrite deployment $YOUR_DEPLOYMENT_NAME dash0.com/enable=false
```

Note that setting `dash0.com/enable: "false"` will _not_ prevent log collection for pods of workloads with that label,
in namespaces that have Dash0 [log collection](#monitoringresource.spec.logCollection.enabled) enabled.
Also, log collection is enabled by default for all monitored namespaces, unless `spec.logCollection.enabled` has been
set to `false` explicitly in the respective Dash0 monitoring resource.
Controlling log collection for individual workloads via Kubernetes labels is not supported.
To disable log collection for specific workloads in namespaces where log collection is enabled, you can add a
[filter](#monitoringresource.spec.filter) rule to the monitoring resource.
Here is an example:

```yaml
apiVersion: operator.dash0.com/v1beta1
kind: Dash0Monitoring
metadata:
  name: dash0-monitoring-resource
spec:
  filter:
    logs:
      log_records:
      - 'IsMatch(resource.attributes["k8s.pod.name"], "my-workload-name-*")'
```

#### Using a Custom Label Selector to Control Auto-Instrumentation

By providing a custom Kubernetes
[label selector](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors) in
[`spec.instrumentWorkloads.labelSelector`](#monitoringresource.spec.instrumentWorkloads.labelSelector) in a Dash0
monitoring resource, you can control which workloads in this namespace will be instrumented by the Dash0 operator.

- Workloads which match this label selector will be instrumented, subject to the value of
  `spec.instrumentWorkloads.mode`.
- Workloads which do not match this label selector will never be instrumented, regardless of the value of
  `spec.instrumentWorkloads.mode`.
- The setting `spec.instrumentWorkloads.labelSelector` setting is ignored if `spec.instrumentWorkloads.mode=none`.

If not set explicitly, this label selector assumes the value `"dash0.com/enable!=false"` by default.
That is, when no expicit label selector is provided via `spec.instrumentWorkloads.labelSelector`, workloads which

- do not have the label `dash0.com/enable` at all, or
- have the label `dash0.com/enable` with a value other than `"false"`

will be instrumented, as explained in the [previous section](#disabling-auto-instrumentation-for-specific-workloads).

It is recommended to leave this setting unset (i.e. leave the default `"dash0.com/enable!=false"` in place), unless you
have a specific use case that requires a different label selector.

One such use case is implementing an opt-in model for workload instrumentation instead of the usual opt-out model.
That is, instead of instrumenting all workloads in a namespace by default and only disabling instrumentation for a few
specific workloads, you want to deliberately turn on auto-instrumentation for a few specific workloads and leave all
others uninstrumented.
Use a label selector with equals (`=`) instead of not-equals (`!=`) to achieve this, for example
`auto-instrument-this-workload-with-dash0="true"`.

Note that opting out of auto-instrumentation and workload modification via a label/label selector will _not_ prevent log
collection for pods in namespaces that have Dash0 [log collection](#monitoringresource.spec.logCollection.enabled)
enabled, see previous section for details.

### Specifying Additional Resource Attributes via Labels and Annotations

_Note:_ The labels and annotations listed in this section can be specified at the pod level, or at the workload level
(i.e., the cronjob, deployment, daemonset, job, replicaset, or statefulset).
Pod labels and annotations take precedence over workload labels and annotations.

The following
[standard Kubernetes labels](https://kubernetes.io/docs/reference/labels-annotations-taints/#labels-annotations-and-taints-used-on-api-objects)
are mapped to resource attributes as follows:

* the label `app.kubernetes.io/name` is mapped to `service.name`
* if `app.kubernetes.io/name` is set, and the label `app.kubernetes.io/version` is also set, it is mapped to `service.version`
* if `app.kubernetes.io/name` is set, and the label `app.kubernetes.io/part-of` is also set, it is mapped to `service.namespace`

The operator will not combine pod labels with workload labels for this mapping.
The labels `app.kubernetes.io/version` and `app.kubernetes.io/part-of` are only read from the pod labels if
`app.kubernetes.io/name` is present on the pod.
Similarly, the labels `app.kubernetes.io/version` and `app.kubernetes.io/part-of` are only read from the workload labels
if `app.kubernetes.io/name` is present on the workload.
Workload labels are not considered at all if `app.kubernetes.io/name` is present on the pod.
This ensures that resource attributes are not partially based on pod and partially on workload labels, giving an
inconsistent result.

Note: The `OTEL_SERVICE_NAME` environment variable and `service.*` key-value pairs specified in the
`OTEL_RESOURCE_ATTRIBUTES` environment variable have precedence over attributes derived from the `app.kubernetes.io/*`
labels.

Additionally, any _annotation_ in the form of `resource.opentelemetry.io/<key>: <value>` will be mapped to the
resource attribute `<key>=<value>`.
For example, the following will result in the `my.attribute=my-value` resource attribute:

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    resource.opentelemetry.io/my.attribute: my-value
```

As with the `app.kubernetes.io/*` labels, the `resource.opentelemetry.io/*` annotations can be set on the pod as well as
on the workload.
In contrast to the `app.kubernetes.io/*` labels, mixing workload level and pod annotations is allowed, that is, you
can set `resource.opentelemetry.io/attribute-one` on the workload and `resource.opentelemetry.io/attribute-two` on the
pod, and both will be used.
In case the same `key` is listed both on the workload and on the pod, the pod annotation takes precedence.

Key-value pairs with a specific `key` set via the `OTEL_RESOURCE_ATTRIBUTES` environment variable will override the
value derived from a `resource.opentelemetry.io/<key>: <value>` annotation.
Resource attributes set via the `resource.opentelemetry.io/<key>: <value>` annotations will override the
resource attributes value set via `app.kubernetes.io/*` labels: for example, `resource.opentelemetry.io/service.name`
has precendence over `app.kubernetes.io/name`.

### Sending Data to the OpenTelemetry Collectors Managed by the Dash0 Operator

Besides automatic workload instrumentation (which will make sure that the instrumented workloads send telemetry to the
OpenTelemetry collectors managed by the operator), you can also send telemetry data from workloads that are not
instrumented by the operator.

To do so, you need to [add an OpenTelemetry SDK](#https://opentelemetry.io/docs/languages/) to your workload.

If the workload is in a namespace that is monitored by Dash0, the OpenTelemetry SDK will automatically be configured to
send telemetry to the OpenTelemetry collectors managed by the Dash0 operator.
This is because the operator automatically sets `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL` to the
correct values when applying the [automatic workload instrumentation](#automatic-workload-instrumentation).

If the workload is in a namespace that is not monitored by Dash0 (or if
[`spec.instrumentWorkloads.mode`](#monitoringresource.spec.instrumentWorkloads.mode) is set to `none` in the respective
Dash0 monitoring resource, or if the workload has opted out of auto-instrumentations via a
[label](#disabling-auto-instrumentation-for-specific-workloads), you need to set the environment variable
[`OTEL_EXPORTER_OTLP_ENDPOINT`](#https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/)
(and optionally also
[`OTEL_EXPORTER_OTLP_PROTOCOL`](https://opentelemetry.io/docs/specs/otel/protocol/exporter/#specify-protocol)) yourself.

The DaemonSet OpenTelemetry collector managed by the Dash0 operator listens on host port 40318 for HTTP traffic and
40317 for gRPC traffic
(unless the Helm chart has been deployed with `operator.collectors.disableHostPorts=true`, which disables the host
ports for the collector pods).
Additionally, there is also a service for the DaemonSet collector, which listens on the standard ports, that is 4318 for
HTTP and 4317 for gRPC.

The preferred way of sending OTLP from your workoad to the Dash0-managed collector is to use node-local traffic via the
host port.
To do so, add the following environment variables to your workload:
```yaml
env:
  - name: K8S_NODE_IP
    valueFrom:
      fieldRef:
        fieldPath: status.hostIP
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://$(K8S_NODE_IP):40318"
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: "http/protobuf"
```

* Note that listing the definition for `K8S_NODE_IP` _before_ `OTEL_EXPORTER_OTLP_ENDPOINT` is crucial.
* Adding `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf` is optional when the OpenTelemetry SDK in question uses that
  protocol as the default.
* For gRCP, use `OTEL_EXPORTER_OTLP_ENDPOINT=http://$(K8S_NODE_IP):40317` together with
  `OTEL_EXPORTER_OTLP_PROTOCOL=grpc` instead.

To use the service endpoint instead of the host port, you need to know
* the Helm release name of the Dash0 operator (for example `dash0-operator`), and
* the namespace where the Dash0 operator is installed (for example `dash0-system`).

With that information at hand, add the following to your workload:

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://${helm-release-name}-opentelemetry-collector-service.${namespace-of-the-dash0-operator}.svc.cluster.local:4318"
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: "http/protobuf"
```

or, for gRPC:

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://${helm-release-name}-opentelemetry-collector-service.${namespace-of-the-dash0-operator}.svc.cluster.local:4317"
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: "grpc"
```

In both cases, `${helm-release-name}` and `${namespace-of-the-dash0-operator}` needs to be replaced with the actual
values.

If the workload is in a namespace that is monitored by Dash0 and
[workload instrumentation](#monitoringresource.spec.instrumentWorkloads.mode) is enabled, Dash0 will automatically add
Kubernetes-related OpenTelemetry resource attributes to your telemetry, even if the runtime in question is not
yet supported by Dash0's auto-instrumentation.
There is currently one caveat: The resource attribute auto-detection relies on the process or runtime in question to
use dynamic linking at startup (that is, binding to a flavor of libc), which is true for almost all runtimes.
One notable exception are so called freestanding a.k.a. libc-free binaries, for example most binaries built with Go.

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

#### Note regarding TLS when using arbitrary OTLP-compatible backends

##### gRPC

- By default, a secure connection is assumed, unless explicitly setting `insecure: true`, or when the `insecure` field
is omitted and the endpoint URL starts with `http://`
- When using TLS, you can set `insecureSkipVerify: true` to disable the verification of the server's certificate chain,
which can be useful when using self-signed certificates.

Here's an example using `insecureSkipVerify`:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  export:
    grpc:
      endpoint: ...             # provide the secure OTLP gRPC endpoint of your observability backend here
      insecureSkipVerify: true  # disables the verification of the server's certificate chain
```

Please note that it is a validation error to set both `insecure` and `insecureSkipVerify` explicitly to true at the same time, since `insecureSkipVerify` is only applicable when using TLS.

##### HTTP

- For HTTP, the connection security is automatically detected based on whether the endpoint URL starts with `http://` or
`https://`
- When using TLS, you can set `insecureSkipVerify: true` to disable the verification of the server's certificate chain,
which can be useful when using self-signed certificates.

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
That means, the operator will send self-monitoring telemetry to the configured Dash0 backend.
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
helm upgrade --wait --namespace dash0-system dash0-operator dash0-operator/dash0-operator
```

## CRD Version Upgrades

Occasionally, the custom resource definitions (CRDs) used by the Dash0 operator (Dash0OperatorConfiguration,
Dash0Monitoring) will be updated to new versions.
Whenever possible, this will happen in a way that requires no manual intervention by users.
This section contains details about CRD version updates and version migrations.

### Operator Version 0.71.0: operator.dash0.com/v1alpha1/Dash0Monitoring -> operator.dash0.com/v1beta1/Dash0Monitoring

With operator version 0.71.0, the Dash0 operator's `Dash0Monitoring` custom resource definition (CRD) is upgraded
from version `v1alpha1` to `v1beta1`.
The operator handles both versions correctly, that is version `v1alpha1` to `v1beta1` are both fully supported.
Here is what you need to know about this version update for `Dash0Monitoring`:
- If you have existing `Dash0Monitoring` resources in version `v1alpha1` in your cluster, they will be automatically
  converted on the fly, for example when the Dash0 operator reads the `v1alpha1` resource version.
  At some point Kubernetes might also convert the resource permanently and store it in version `v1beta1`.
- After the upgrade to version 0.71.0, you can still deploy new `Dash0Monitoring` resource in version `v1alpha1`
  (for example via `kubectl apply`).
  They will be automatically converted and stored as `v1beta1` resources by Kubernetes.
- If you want to migrate a `Dash0Monitoring` _template_ (e.g. a yaml file) from version `v1alpha1` to `v1beta1`, follow
  these steps:
    - If the template specifies the workload instrumentation mode via `spec.instrumentWorkloads`, replace that with
      `spec.instrumentWorkloads.mode`.
      That is:
      ```yaml
        spec:
          instrumentWorkloads: created-and-updated
      ```
      becomes
      ```yaml
      spec:
        instrumentWorkloads:
          mode: created-and-updated
      ```
      If the template does not specify the workload instrumentation mode explicitly (that is, it relies on using the
      default instrumentation mode), no change is necessary here.
    - If the template contains the attribute `spec.prometheusScrapingEnabled`, replace that with
      `spec.prometheusScraping.enabled`.
      That is:
      ```yaml
        spec:
          prometheusScrapingEnabled: true
      ```
      becomes
      ```yaml
      spec:
        prometheusScraping:
          enabled: true
      ```
      The attribute `spec.prometheusScraping.enabled` is also already valid for `v1alpha1`, so this particular change
      can be applied independently of the CRD version change.
      If the template does not specify whether prometheusScraping is enabled or not (that is, it relies on using the
      default value), no change is necessary here.
- We recommend to update your templates from `v1alpha1` to `v1beta1` at some point.
  However, there are currently no plans to remove support for version `v1alpha1`.
- If you want to use the new [trace context propagators](#monitoringresource.spec.instrumentWorkloads.traceContext.propagators)
  option that has been added in version 0.71.0, you need to use version `v1beta1` of the `Dash0Monitoring` resource.
  This includes updating your Yaml templates to that version, as described above.
- After upgrading to operator version 0.71.0 or later, you can no longer easily downgrade to a version before 0.71.0.
  In particular, this downgrade would require to manually delete all `Dash0Monitoring` resources in the cluster.
  The reason is that the Dash0Monitoring resource are now stored as version `v1beta1` by Kubernetes and there is no
  automatic downward conversion from `v1beta1` back to `v1alpha1`.
- The only supported version for `Dash0OperatorConfiguration` is still `v1alpha1`, that is, trying to use
  `operator.dash0.com/v1beta1/Dash0OperatorConfiguration` will not work.

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

In namespaces that are [enabled for Dash0 monitoring](#enable-dash0-monitoring-for-a-namespace), all supported workload
types are automatically instrumented by the Dash0 operator, to achieve two goals:
* Enable tracing for [supported runtimes](#supported-runtimes) out of the box, and
* improve auto-detection of OpenTelemetry resource attributes.

This allows Dash0 users to avoid the hassle of manually adding the OpenTelemetry SDK to their applications, or to set
Kubernetes-related resource attributes manually.
Dash0 simply takes care of it automatically!

Automatic tracing only works for [supported runtimes](#supported-runtimes).
For other runtimes, you can add an OpenTelemetry SDK to your workloads
[by other means](#https://opentelemetry.io/docs/languages/).

Auto-detecting OpenTelemetry resource attributes works for _all_ runtimes, that is, for runtimes that are
supported by Dash0's auto-instrumentation as well as for workloads to which an OpenTelemetry SDK has been added
otherwise.
There is currently one caveat: The resource attribute auto-detection relies on the process or runtime in question to
use dynamic linking at startup (that is, binding to a flavor of libc), which is true for almost all runtimes.
One notable exception are so called freestanding a.k.a. libc-free binaries, for example most binaries built with Go.

The Dash0 operator will instrument the following workload types:

* [CronJobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)
* [DaemonSets](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/)
* [Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
* [Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
* [Pods](https://kubernetes.io/docs/concepts/workloads/pods/)
* [ReplicaSets](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/)
* [StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)

Note that Kubernetes jobs and Kubernetes pods are only instrumented at deploy time, _existing_ jobs and pods cannot be
instrumented since there is no way to restart them.
For all other workload types, the operator can instrument existing workloads as well as new workloads at deploy time
(depending on the setting of [`spec.instrumentWorkloads.mode`](#monitoringresource.spec.instrumentWorkloads.mode) in the
Dash0 monitoring resource).

The instrumentation process is performed by modifying the Pod spec template (for CronJobs, DaemonSets, Deployments,
Jobs, ReplicaSets, and StatefulSets) or the Pod spec itself (for standalone Pods).

The modifications that are performed for workloads are the following:
* add an [`emptyDir` volume](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) named
  `dash0-instrumentation` to the pod spec
* add a volume mount `dash0-instrumentation` to all containers of the pod
* add an [init container](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) named
  `dash0-instrumentation` that will copy the OpenTelemetry SDKs and distributions for supported runtimes to the
  `dash0-instrumentation` volume mount, so they are available in the target container's file system
* add or modifying environment variables (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `LD_PRELOAD`,
  and several Dash0-specific variables prefixed with `DASH0_`) to all containers of the pod
* add the Dash0 injector (see below for details) as a startup hook (via the `LD_PRELOAD` environment variable) to all
  containers of the pod
* add the following labels to the workload metadata:
    * `dash0.com/instrumented`: `true` or `false` depending on whether the workload has been successfully instrumented
      or not
    * `dash0.com/operator-image`: the fully qualified image name of the Dash0 operator image that has instrumented this
      workload
    * `dash0.com/init-container-image`: the fully qualified image name of the Dash0 instrumentation image that has been
      added as an init container to the pod spec
    * `dash0.com/instrumented-by`: either `controller` or `webhook`, depending on which component has instrumented this
      workload. The controller is responsible for instrumenting existing workloads while the webhook is responsible for
      instrumenting new workloads at deploy time.

**Notes:**

1. Automatic tracing will only happen for [supported runtimes](#supported-runtimes).
   Nonetheless, the modifications outlined above are performed for _every_ workload.
   One reason for that is that there is no way to tell which runtime a workload uses from the outside, e.g. on the
   Kubernetes level.
   The more important reason is that runtimes that are not (yet) supported for auto-instrumentation still benefit from
   the improved OpenTelemetry resource attribute detection.
2. The operator operates sets `OTEL_EXPORTER_OTLP_ENDPOINT=http://$(NODE_IP):40318`, that is, it tells the workload to
   send OTLP traffic to the HTTP port of the OpenTelemetry collector pod on the same host, which belongs to the
   OpenTelemetry collector DaemonSet managed by the operator.
   It also sets the protocol accordingly by setting `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`.
   The protocol `http/protobuf` is the recommended default according to the
   [OpenTelemetry specification](https://opentelemetry.io/docs/specs/otel/protocol/exporter/#specify-protocol), and it
   is widely supported.
   It does so under the assumption that workloads _which have an OpenTelemetry SDK_ use an SDK that respects
   `OTEL_EXPORTER_OTLP_PROTOCOL` and also has support for the `http/protobuf` protocol.
   For workloads that have an OpenTelemetry SDK that either does not respect `OTEL_EXPORTER_OTLP_PROTOCOL` (and defaults
   to `grpc`) or does not have support for `http/protobuf`, this will lead to the SDK trying to establish a gRPC
   connection to the collector's HTTP endpoint, that is, the SDK will not be able to emit telemetry.
   SDKs without support for `http/protobuf` are rather rare, but one prominent example is the Kubernetes
   [ingress-nginx](https://kubernetes.github.io/ingress-nginx/user-guide/third-party-addons/opentelemetry/).
   The recommended approach is to disable workload instrumentation by the Dash0 operator for these workloads; for
   example by opting out of auto-instrumentation via a workload label (i.e. `dash0.com/enable: "false"`, see
   [Disabling Auto-Instrumentation for Specific Workloads](#disabling-auto-instrumentation-for-specific-workloads)),
   or by not installing a Dash0 monitoring resource in the namespace where these workloads are located.
   The workloads can then be monitored by following the setup described in
   [Sending Data to the OpenTelemetry Collectors Managed by the Dash0 Operator](#sending-data-to-the-opentelemetry-collectors-managed-by-the-dash0-operator)
   to have the workload send telemetry to the collectors managed by the Dash0 operator, using gRPC.
   Note that this is not relevant for workloads that do not have an OpenTelemetry SDK at all, since they will ignore
   `OTEL_EXPORTER_OTLP_ENDPOINT`.
   In case the Dash0 operator Helm chart has been deployed with `operator.collectors.forceUseServiceUrl=true` or
   `operator.collectors.disableHostPorts=true`, `OTEL_EXPORTER_OTLP_ENDPOINT` is not set to `http://$(NODE_IP):40318`,
   but to the HTTP port of the DaemonSet collector's service URL
   `http://${helm-release-name}-opentelemetry-collector-service.${namespace-of-the-dash0-operator}.svc.cluster.local:4318`
   instead.

The remainder of this section provides a more detailed step-by-step description of how the Dash0 operator's workload
instrumentation for tracing works internally, intended for the technically curious reader.
You can safely skip this section if you are not interested in the technical details.

1. The Dash0 operator adds the `dash0-instrumentation` init container with the
   [Dash0 instrumentation image](https://github.com/dash0hq/dash0-operator/tree/main/images/instrumentation) to the pod
   spec template of workloads.
   The instrumentation image contains OpenTelemetry SDKs and distributions for all supported runtimes and the Dash0
   injector binary.
2. When the init container starts, it copies the OpenTelemetry distributions and distributions and the injector binary
   to a dedicated shared volume mount that has been added by the operator, so they are available in the target
   container's file system.
   When it has copied all files, the init container exits.
3. The operator also adds environment variables to the target container to ensure that the OpenTelemetry SDK has the
   correct configuration and will get activated at startup.
   The activation of the OpenTelemetry SDK happens via an `LD_PRELOAD` hook.
   For that purpose, the Dash0 operator adds the `LD_PRELOAD` environment variable to the pod spec template of the
   workload.
   `LD_PRELOAD` is an environment variable that is evaluated by the
   [dynamic linker/loader](https://man7.org/linux/man-pages/man8/ld.so.8.html) when a Linux executable starts.
   In general, it specifies a list of additional shared objects to be loaded before the actual code of the executable.
   In this specific case, the Dash0 injector binary is added to the `LD_PRELOAD` list.
4. At process startup, the Dash0 injector adds additional environment variables to the running process by hooking into
   the application startup, finding the `dlsym` symbol and `setenv` symbols, and then calling `setenv` to add or modify
   environment variables (like `OTEL_RESOURCES`, `NODE_OPTIONS`, `JAVA_TOOL_OPTIONS`).
   The reason for doing that at process startup and not when modifying the pod spec (where environment variables can
   also be added and modified) is that the original environment variables are not necessarily fully known at that time.
   Workloads will sometimes set environment variables in their Dockerfile or in an entrypoint script; those environment
   variables are only available at process runtime.
   For example, the Dash0 injector sets (or appends to) `NODE_OPTIONS` to activate the
   [Dash0 OpenTelemetry distribution for Node.js](https://github.com/dash0hq/opentelemetry-js-distribution) to collect
   tracing data from all Node.js workloads.
   For JVMs, the same is achieved by setting (or appending to) the `JAVA_TOOL_OPTIONS` environment variable, namely
   adding a `-javaagent`).
5. Additionally, the Dash0 injector automatically improves Kubernetes-related resource attributes as follows:
   The operator sets the environment variables `DASH0_NAMESPACE_NAME`, `DASH0_POD_NAME`, `DASH0_POD_UID` and
   `DASH0_CONTAINER_NAME` on workloads.
   The Dash0 injector binary picks these values up and uses them to populate the resource attributes
   `k8s.namespace.name`, `k8s.pod.name`, `k8s.pod.uid` and `k8s.container.name` via the `OTEL_RESOURCE_ATTRIBUTES`
   environment variable.
   If `OTEL_RESOURCE_ATTRIBUTES` is already set on the process, the key-value pairs for these attributes are appended to
   the existing value of `OTEL_RESOURCE_ATTRIBUTES`.
   If `OTEL_RESOURCE_ATTRIBUTES` was not set on the process, the Dash0 injector will add `OTEL_RESOURCE_ATTRIBUTES` as
   a new environment variable.

If you are curious, the source code for the injector is open source and can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/images/instrumentation/injector).

## Scraping Prometheus Endpoints

The Dash0 operator automatically scrapes Prometheus endpoints on pods annotated with the `prometheus.io/*` annotations
as defined by the
[Prometheus Helm chart](https://github.com/prometheus-community/helm-charts/tree/main/charts/prometheus#scraping-pod-metrics-via-annotations).

The supported annotations are:
* `prometheus.io/scrape`: Only scrape pods that have a value of `true`, except if `prometheus.io/scrape-slow` is set to
  `true` as well. Endpoints on pods annotated with this annotation are scraped every minute, i.e., scrape interval is 1
  minute, unless `prometheus.io/scrape-slow` is also set to `true`.
* `prometheus.io/scrape-slow`: If set to `true`, enables scraping for the pod with scrape interval of 5 minutes. If both
  `prometheus.io/scrape` and `prometheus.io/scrape-slow` are annotated on a pod with both values set to `true`, the pod
  will be scraped every 5 minutes.
* `prometheus.io/scheme`: If the metrics endpoint is secured then you will need to set this to `https`.
* `prometheus.io/path`: Override the metrics endpoint path if it is not the default `/metrics`.
* `prometheus.io/port`: Override the metrics endpoint port if it is not the default `9102`.

To be scraped, a pod annotated with the `prometheus.io/scrape` or `prometheus.io/scrape-slow` annotations must belong to
namespaces that are configured to be monitored by the Dash0 operator
(see [Enable Dash0 Monitoring For a Namespace](#enable-dash0-monitoring-for-a-namespace) section).

The scraping of a pod is executed from the same Kubernetes node the pod resides on.

This feature can be disabled for a namespace by explicitly setting
[`prometheusScraping.enabled: false`](#monitoringresource.spec.prometheusScraping.enabled) in the Dash0 monitoring
resource.

Note: To also have [Kube state metrics available](https://github.com/kubernetes/kube-state-metrics) (which are used
extensively in [Awesome Prometheus alerts](#https://samber.github.io/awesome-prometheus-alerts/)) scraped and delivered
to Dash0, you can annotate the kube-state-metrics pod with `prometheus.io/scrape: "true"` and add a Dash0 monitoring
resource to the namespace it is running in.

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
  has the setting [`synchronizePersesDashboards`](#monitoringresource.spec.synchronizePersesDashboards) set to `false`.
  (This setting is optional and defaults to `true` when omitted.)

Furthermore, the custom resource definition for Perses dashboards needs to be installed in the cluster. There are two
ways to achieve this:
* Install the Perses dashboard custom resource definition with the following command:
```console
kubectl apply --server-side -f https://raw.githubusercontent.com/perses/perses-operator/refs/tags/v0.2.0/config/crd/bases/perses.dev_persesdashboards.yaml
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

You can opt out of synchronization for individual Perses dashboard resources by adding the Kubernetes label
`dash0.com/enable: false` to the Perses dashboard resource.
If this label is added to a dashboard which has previously been synchronized to Dash0, the operator will delete the
corresponding dashboard in Dash0.
Note that the `spec.instrumentWorkloads.labelSelector` in the monitoring resource does not affect the synchronization of
Perses dashboards, the label to opt out of synchronization is always `dash0.com/enable: false`, even if a non-default
label selector has been set in `spec.instrumentWorkloads.labelSelector`.

When a Perses dashboard resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to the status of the Dash0 monitoring resource in the same namespace. This summary will also
show whether the dashboard had any validation issues or an error occurred during synchronization:
```
Kind: Dash0Monitoring
...
Status:
  Perses Dashboard Synchronization Results:
    my-namespace/perses-dashboard-test:
      Synchronization Status:     successful
      Synchronized At:            2024-10-25T12:02:12Z
```

Note: If you only want to manage dashboards, check rules, synthetic checks and views via the Dash0 operator, and you do
not want it to collect telemetry, you can set `telemetryCollection.enabled` to `false` in the Dash0 operator
configuration resource.
This will disable the telemetry collection by the operator, and it will also instruct the operator to not deploy the
OpenTelemetry collector in your cluster.

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
  has the setting [`synchronizePrometheusRules`](#monitoringresource.spec.synchronizePrometheusRules) set to `false`.
  (This setting is optional and defaults to `true` when omitted.)

Furthermore, the custom resource definition for Prometheus rules needs to be installed in the cluster. There are two
ways to achieve this:
* Install the Prometheus rules custom resource definition with the following command:
```console
kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.87.0/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml
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
* If `expr` contains the token `$__threshold`, and neither annotation `dash0-threshold-degraded` nor
  `dash0-threshold-critical` is present, the rule will be considered invalid and will not be synchronized to Dash0.
* If the rule has the annotation `dash0-enabled=false`, the check rule will be synchronized but disabled
  in Dash0.
  This Prometheus annotation is not to be confused with the Kubernetes label `dash0.com/enable: false`, which disables
  synchronization of the entire Prometheus rules resource (and all its check rules) to Dash0 (see below).
* The group attribute `limit` is not supported by Dash0 and will be ignored.
* The group attribute `partial_response_strategy` is not supported by Dash0 and will be ignored.
* All labels (except for the ones explicitly mentioned in the conversion table below) will be listed under
  "Additional labels".
* All annotations (except for the ones explicitly mentioned in the conversion table below) will be listed under
  "Annotations".

| Prometheus alert rule attribute        | Dash0 Check rule field                                                                                            | Notes                                                                                                                                                      |
|----------------------------------------|-------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `alert`                                | Name of check rule, prefixed by the group name                                                                    | must be a non-empty string                                                                                                                                 |
| `expr`                                 | "Expression"                                                                                                      | must be a non-empty string                                                                                                                                 |
| `interval` (from group)                | "Evaluate every"                                                                                                  |                                                                                                                                                            |
| `for`                                  | "Grace periods"/"For"                                                                                             | default: "0s"                                                                                                                                              |
| `keep_firing_for`                      | "Grace periods"/"Keep firing for"                                                                                 | default: "0s"                                                                                                                                              |
| `annotations/summary`                  | "Summary"                                                                                                         |                                                                                                                                                            |
| `annotations/description`              | "Description"                                                                                                     |                                                                                                                                                            |
| `annotations/dash0-enabled`            | Denotes whether the check rule is enabled and should be evaluated.                                                | default: "true"; must be either "true" or "false"                                                                                                          |
| `annotations/dash0-threshold-degraded` | Will be used in place of the token `$__threshold` in the expression, to determine whether the check is _degraded_ | If present, needs to a be string that can be parsed to a float value according to the syntax described in https://go.dev/ref/spec#Floating-point_literals. |
| `annotations/dash0-threshold-critical` | Will be used in place of the token `$__threshold` in the expression, to determine whether the check is _critical_ | If present, needs to a be string that can be parsed to a float value according to the syntax described in https://go.dev/ref/spec#Floating-point_literals. |
| `annotations/*`                        | "Annotations"                                                                                                     |
| `labels/*`                             | "Additional labels"                                                                                               |

You can opt out of synchronization for individual Prometheus rules resources by adding the Kubernetes label
`dash0.com/enable: false` to it.
If this label is added to a Prometheus rules resource which has previously been synchronized to Dash0, the operator will
delete all corresponding check rules in Dash0.
Note that this refers to a _Kubernetes_ label on the Kubernetes resource, and it will affect all check rules contained
in this Prometheus rules resource.
This mechanism is not to be confused with the Prometheus annotation `dash0-enabled`, which can be applied to
individual rules in a Prometheus rules resource, and controls whether the check rule is enabled or disabled in Dash0.
Please also note that the `spec.instrumentWorkloads.labelSelector` in the monitoring resource does not affect the
synchronization of Prometheus rule resources, the label to opt out of synchronization is always
`dash0.com/enable: false`, even if a non-default label selector has been set in
`spec.instrumentWorkloads.labelSelector`.

When a Prometheus rules resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to the status of the Dash0 monitoring resource in the same namespace.
This summary will also
show whether any of the rules had validation issues or errors occurred during synchronization:

```yaml
Kind: Dash0Monitoring
...
Status:
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

Note: If you only want to manage dashboards, check rules, synthetic checks and views via the Dash0 operator, and you do
not want it to collect telemetry, you can set `telemetryCollection.enabled` to `false` in the Dash0 operator
configuration resource.
This will disable the telemetry collection by the operator, and it will also instruct the operator to not deploy the
OpenTelemetry collector in your cluster.

## Managing Dash0 Synthetic Checks

You can manage your Dash0 synthetic checks via the Dash0 operator.

Pre-requisites for this feature:
* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have a Dash0 export configured with authorization
  (either `token` or `secret-ref`).
* The operator will only pick up synthetic check resources in namespaces that have a Dash0 monitoring resource
  deployed.

With the prerequisites in place, you can manage Dash0 synthetic checks via the operator.
The Dash0 operator will watch for synthetic check resources in all namespaces that have a Dash0 monitoring resource
deployed, and synchronize the synthetic check resources with the Dash0 backend:
* When a new synthetic check resource is created, the operator will create a corresponding synthetic check via Dash0's
  API.
* When a synthetic check resource is changed, the operator will update the corresponding synthetic check via Dash0's
  API.
* When a synthetic check resource is deleted, the operator will delete the corresponding synthetic check via Dash0's
  API.

The synthetic checks created by the operator will be in read-only mode in the Dash0 UI.

The custom resource definition for Dash0 synthetic checks can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/templates/operator/custom-resource-definition-synthetic-checks.yaml).
An easy way to get started is to create a synthetic check in the Dash0 UI and then download the YAML representation of
that check with the button in the upper right corner.
The downloaded YAML can then be deployed as the manifest of a synthethic check in your Kubernetes cluster.
Once the check is managed via the operator, you might want to delete the synthetic check that has been created in the
Dash0 UI directly in the first step -- otherwise it would show up as a duplicate in the Dash0 UI, i.e. two synthetic
checks with the same name but different internal IDs.

If the Dash0 operator configuration resource has the `dataset` property set, the operator will create the synthetic
checks in that dataset, otherwise they will be created in the `default` dataset.

You can opt out of synchronization for individual synthetic checks resources by adding the Kubernetes label
`dash0.com/enable: false` to the synthetic check resource.
If this label is added to a synthetic chec which has previously been synchronized to Dash0, the operator will delete the
corresponding synthetic chec in Dash0.
Note that the `spec.instrumentWorkloads.labelSelector` in the monitoring resource does not affect the synchronization of
synthetic checks, the label to opt out of synchronization is always `dash0.com/enable: false`, even if a non-default
label selector has been set in `spec.instrumentWorkloads.labelSelector`.

When a synthetic check resource has been synchronized to Dash0, the operator will write a summary of that
synchronization operation to its status.
Note that in contrast to synchronizing Prometheus rules or Perses dashboards (which are third-party custom resources
from the perspective of the operator, i.e. they are potentially owned and managed by another Kubernetes operator), the
result of the synchronization operation will not be written to the status of the Dash0 monitoring resource in the same
namespace, but to the synthetic check resource status directly.
The status will also show whether the synthetic check had any validation issues or an error occurred during
synchronization.

```yaml
Kind: Dash0SyntheticCheck
...
Status:
  Synchronization Status: successful
  Synchronized At:        2025-09-05T11:47:56Z
```

Note: If you only want to manage dashboards, check rules, synthetic checks and views via the Dash0 operator, and you do
not want it to collect telemetry, you can set `telemetryCollection.enabled` to `false` in the Dash0 operator
configuration resource.
This will disable the telemetry collection by the operator, and it will also instruct the operator to not deploy the
OpenTelemetry collector in your cluster.

## Managing Dash0 Views

You can manage your Dash0 views via the Dash0 operator.

Pre-requisites for this feature:
* A Dash0 operator configuration resource has to be installed in the cluster.
* The operator configuration resource must have the `apiEndpoint` property.
* The operator configuration resource must have a Dash0 export configured with authorization
  (either `token` or `secret-ref`).
* The operator will only pick up Dash0 view resources in namespaces that have a Dash0 monitoring resource
  deployed.

With the prerequisites in place, you can manage Dash0 views via the operator.
The Dash0 operator will watch for view resources in all namespaces that have a Dash0 monitoring resource deployed, and
synchronize the view resources with the Dash0 backend:
* When a new view resource is created, the operator will create a corresponding view via Dash0's API.
* When a view resource is changed, the operator will update the corresponding view via Dash0's API.
* When a view resource is deleted, the operator will delete the corresponding view via Dash0's API.

The views created by the operator will be in read-only mode in the Dash0 UI.

The custom resource definition for Dash0 views can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/templates/operator/custom-resource-definition-views.yaml).
An easy way to get started is to create a view in the Dash0 UI and then download the YAML representation of that view by
using the "Download -> YAML" action from the context menu of the view.
The downloaded YAML can then be deployed as the manifest of a view in your Kubernetes cluster.
Once the view is managed via the operator, you might want to delete the view that has been created in the Dash0 UI
directly in the first step -- otherwise it would show up as a duplicate in the Dash0 UI, i.e. two views with the same
name but different internal IDs.

If the Dash0 operator configuration resource has the `dataset` property set, the operator will create the view in that
dataset, otherwise they will be created in the `default` dataset.

You can opt out of synchronization for individual view resources by adding the Kubernetes label
`dash0.com/enable: false` to the view resource.
If this label is added to a synthetic chec which has previously been synchronized to Dash0, the operator will delete the
corresponding synthetic chec in Dash0.
Note that the `spec.instrumentWorkloads.labelSelector` in the monitoring resource does not affect the synchronization of
views, the label to opt out of synchronization is always `dash0.com/enable: false`, even if a non-default label selector
has been set in `spec.instrumentWorkloads.labelSelector`.

When a view resource has been synchronized to Dash0, the operator will write a summary of that synchronization operation
to its status.
Note that in contrast to synchronizing Prometheus rules or Perses dashboards (which are third-party custom resources
from the perspective of the operator, i.e. they are potentially owned and managed by another Kubernetes operator), the
result of the synchronization operation will not be written to the status of the Dash0 monitoring resource in the same
namespace, but to the view resource status directly.
The status will also show whether the view had any validation issues or an error occurred during synchronization.

```yaml
Kind: Dash0View
...
Status:
  Synchronization Status: successful
  Synchronized At:        2025-09-05T11:47:56Z
```

Note: If you only want to manage dashboards, check rules, synthetic checks and views via the Dash0 operator, and you do
not want it to collect telemetry, you can set `telemetryCollection.enabled` to `false` in the Dash0 operator
configuration resource.
This will disable the telemetry collection by the operator, and it will also instruct the operator to not deploy the
OpenTelemetry collector in your cluster.

## Notes on GKE Autopilot

When deploying the Dash0 operator to a GKE Autopilot cluster, provide the following additional setting when applying the
Helm chart:

```yaml
operator:
  gke:
    autopilot:
      enabled: true
```

GKE Autopilot [restricts](https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-security) what workloads
in an autopilot clusters can do.
With `operator.gke.autopilot.enabled` set to `true`, the Dash0 operator Helm chart deploys an
`auto.gke.io/AllowlistSynchronizer` resource into the target cluster, which in turn will add the required
`auto.gke.io/WorkloadAllowlist` resources for Dash0 workloads (the operator and the OpenTelemetry collectors it
manages).
This allows the Dash0 operator to work on GKE Autopilot clusters.

Not all restrictions can be lifted via workload allowlist, the following features are not available on GKE Autopilot
clusters:
- collecting utilization metrics with the `kubeletstats` receiver is disabled; collecting these requires access to the
  `/pod` endpoint of the kubelet API which is not available in GKE autopilot due to the lack of the `nodes/proxy`
  permission:
    - `k8s.pod.cpu_limit_utilization`,
    - `k8s.pod.cpu_request_utilization`,
    - `k8s.pod.memory_limit_utilization`, and
    - `k8s.pod.memory_request_utilization`
- collecting the extra metadata labels `container.id` and `k8s.volume.type` for the `kubeletstats` receiver metrics is
  disabled, collecting these requires access to the `/pod` endpoint of the kubelet API which is not available in GKE
  autopilot due to the lack of the `nodes/proxy` permission

**Note:** Using a volume for filelog offsets is currently not supported in GKE Autopilot clusters.

**Note:** The `AllowlistSynchronizer` resource is not removed automatically with `helm uninstall dash0-operator`.
If you decide to remove the Dash0 operator Helm release from the cluster, you might want to delete the
`AllowlistSynchronizer` manually afterward, for example by executing
`kubectl delete AllowlistSynchronizer dash0-allowlist-synchronizer`.
Deleting the `AllowlistSynchronizer` will also delete all associated `WorkloadAllowlist` resources.

Refer to <https://cloud.google.com/kubernetes-engine/docs/how-to/run-autopilot-partner-workloads> for more information
on `AllowlistSynchronizer`, `WorkloadAllowlist`, and related concepts.

### Managing the AllowlistSynchronizer Manually

As an alternative to letting the Helm chart install the `AllowlistSynchronizer`, you can also choose to manage this
manually, if you prefer:

```yaml
operator:
  gke:
    autopilot:
      enabled: true
      deployAllowlistSynchronizer: false
```

With these settings, the Dash0 operator Helm chart will not deploy the `AllowlistSynchronizer`.
Using these settings requires that you deploy the Dash0 `AllowlistSynchronizer` before installing the Dash0 operator.
To do that, create the following file `dash0-gke-autopilot-allowlist-synchronizer.yaml`:
```yaml
apiVersion: auto.gke.io/v1
kind: AllowlistSynchronizer
metadata:
  name: dash0-allowlist-synchronizer
spec:
  allowlistPaths:
    - Dash0/operator-manager/dash0-operator-manager-v1.0.0.yaml
    - Dash0/post-install/dash0-post-install-v1.0.0.yaml
    - Dash0/pre-delete/dash0-pre-delete-v1.0.0.yaml
    - Dash0/opentelemetry-collector-agent/dash0-opentelemetry-collector-agent-v1.0.0.yaml
    - Dash0/opentelemetry-cluster-metrics-collector/dash0-opentelemetry-cluster-metrics-collector-v1.0.0.yaml
```

Then deploy it as follows:
```
kubectl apply -f dash0-gke-autopilot-allowlist-synchronizer.yaml
```

When managing the `AllowlistSynchronizer` manually, you might need to update it from time to time for future Dash0
operator releases.

## Notes on Azure AKS

In [AKS](https://azure.microsoft.com/products/kubernetes-service) clusters that have the
[Azure Policy add-on](https://learn.microsoft.com/azure/aks/use-azure-policy) enabled, it is highly recommended to
[use a volume for filelog offsets](#providing-a-filelog-offset-volume)
instead of the default filelog offset config map.
Using the default config map filelog offset storage in AKS clusters with this add-on can lead to severe performance
issues.

## Notes on the Open Policy Agent

In clusters that have the [OPA gatekeeeper](https://github.com/open-policy-agent/gatekeeper) deployed it is highly
recommended to [use a volume for filelog offsets](#providing-a-filelog-offset-volume) instead of the default filelog
offset config map.
Using the default config map filelog offset storage in clusters with this component can lead to severe performance
issues.

## Notes on Kyverno Admission Controller

In clusters that have the
[Kyverno admission controller](https://kyverno.io/docs/introduction/how-kyverno-works/#kubernetes-admission-controls)
deployed, it is highly recommended to either [use a volume for filelog offsets](#providing-a-filelog-offset-volume)
instead of the default filelog offset config map, or to
[exclude](https://kyverno.io/docs/installation/customization/#resource-filters) ConfigMaps (or all resource types) in
the Dash0 operator's namespace from Kyverno's processing.
Leaving Kyverno processing in place and using the config map filelog offset storage can lead to severe performance
issues, since the default config map for filelog offsets is updated very frequently.
This can cause Kyverno to consume a lot of CPU and memory resources, potentially even leading to OOMKills of the Kyverno
admission controller.

## Notes on GitOps

When deploying workloads via GitOps tools like ArgoCD or Flux in a cluster where the Dash0 operator is installed, some
care needs to be exercised to not create conflicts between the workload definition in the GitOps repsitory and the
[workload modifications](#automatic-workload-instrumentation) that are applied automatically by the Dash0 operator.
Otherwise, workload settings might flip-flop between what the GitOps system wants to apply and what the Dash0 operator
does, or the GitOps system might overwrite the Dash0 operator's settings, thereby breaking telemetry collection for the
workload.

Environment variable definitions in pod spec templates are the most likely source of conflict.
To avoid conflicts, it is recommended to not define the following environment variables via GitOps:

* `OTEL_EXPORTER_OTLP_ENDPOINT`
* `OTEL_EXPORTER_OTLP_PROTOCOL`
* `OTEL_PROPAGATORS`
* `LD_PRELOAD`
* `DASH0_NODE_IP`
* `DASH0_OTEL_COLLECTOR_BASE_URL`
* `DASH0_NAMESPACE_NAME`
* `DASH0_POD_NAME`
* `DASH0_POD_UID`
* `DASH0_CONTAINER_NAME`
* `DASH0_SERVICE_NAME`
* `DASH0_SERVICE_NAMESPACE`
* `DASH0_SERVICE_VERSION`
* `DASH0_RESOURCE_ATTRIBUTES`

This recommendation does not apply to workloads that are
[excluded from workload instrumentation](#disabling-auto-instrumentation-for-specific-workloads) or workloads in
namespaces without a [Dash0 monitoring resource](#enable-dash0-monitoring-for-a-namespace) or a monitoring resource with
[`spec.instrumentWorkloads.mode`](#monitoringresource.spec.instrumentWorkloads.mode) set to `none`.

## Notes on ArgoCD

As many other Helm charts, the Dash0 operator Helm chart regenerates TLS certificates for in-cluster communication,
that is, for its services and webhooks.
The certificate will be regenerated every time the Dash0 operator Helm chart is applied.
For users deploying the Dash0 operator via ArgoCD, and in particular without using ArgoCD's auto-sync feature, the
certificates and derived data (`ca.crt`, `tls.crt`, `tls.key`, `caBundle`) will show up as a diff in the ArgoCD UI.
The certificate is also regenerated everytime the hard refresh option is used in ArgoCD, since this action will trigger
rendering the Helm chart templates again, even if nothing has changed in the git repository.

To avoid this, you can instruct ArgoCD to ignore these particular differences.
Here is an example for an `argoproj.io/v1alpha1.Application` resource with `ignoreDifferences`:

```yaml
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: dash0-operator
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:

  source:
    chart: dash0-operator
    repoURL: https://dash0hq.github.io/dash0-operator
    targetRevision: ...

  # ... your current spec for the dash0-operator ArgoCD application

  # Ignore certificates which are generated on the fly during Helm chart template rendering:
  ignoreDifferences:
    - kind: Secret
      name: dash0-operator-certificates
      jsonPointers:
        - /data/ca.crt
        - /data/tls.crt
        - /data/tls.key
    - group: admissionregistration.k8s.io
      kind: MutatingWebhookConfiguration
      name: dash0-operator-injector
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: admissionregistration.k8s.io
      kind: MutatingWebhookConfiguration
      name: dash0-operator-monitoring-mutating
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: admissionregistration.k8s.io
      kind: MutatingWebhookConfiguration
      name: dash0-operator-operator-configuration-mutating
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: admissionregistration.k8s.io
      kind: ValidatingWebhookConfiguration
      name: dash0-operator-monitoring-validator
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
    - group: admissionregistration.k8s.io
      kind: ValidatingWebhookConfiguration
      name: dash0-operator-operator-configuration-validator
      jsonPointers:
        - /webhooks/0/clientConfig/caBundle
```

## Notes on Running The Operator on Apple Silicon

When running the operator on an Apple Silicon host (M1, M3 etc.), for example via Docker Desktop, some attention needs
to be paid to the CPU architecture of images. The architecture of the Kubernetes node for this scenario will be `arm64`.
When running a single-architecture `amd64` image (as opposed to a single-architecture `arm64` image or a
[multi-platform build](https://docs.docker.com/build/building/multi-platform/) containing `amd64` as well as `arm64`)
the operator will prevent the container from starting.

The reason for this is the interaction between Rosetta emulation and how the operator works. The Dash0 instrumentation
image (which is added as an init container and contains the auto-tracing injector) is a multi-platform image, supporting
both `amd64` and `arm64`. When this image is pulled from an Apple Silicon machine, it automatically pulls the `arm64`
variant. That is, the injector binary that is added via the init container is compiled for `arm64`. Now, when the
application from your `amd64` application image is started, the injector and the application will be incompatible, as
they have been built for two different CPU architectures.

Under normal circumstances, an `amd64` image would not work on an `arm64` Kubernetes node anyway, but in the case of
Docker Desktop on MacOS, this combination is enabled due to Docker Desktop automatically running `amd64` images via
Rosetta2 emulation.

You can work around this issue by one of the following methods:
* using an `amd64` Kubernetes node,
* by building a multi-platform image for your application, or
* by building the application as an `arm64` image (e.g. by using `--platform=linux/arm64` when building the image).

## Notes on Running The Operator on Docker Desktop

The `hostmetrics` receiver will be disabled when using Docker as the container runtime.

## Notes on Running The Operator on Minikube

The `hostmetrics` receiver will be disabled when using Docker as the container runtime.

## Troubleshooting

### Create Heap Profiles

The instructions in this section are mainly meant to be used in a shared troubleshooting session with Dash0 support.

_To get a heap profile from the operator manager container:_
1. Deploy the operator manager with the additional Helm value `operator.pprofPort=1777`.
2. Take note of the namespace the operator is deployed in (default: `dash0-system`).
3. Run `kubectl get pod -n <operator-namespace> -l app.kubernetes.io/component=controller` to get the name of the
   operator manager pod (usually something like `dash0-operator-controller-xxxxxxxxx-xxxxx`, but the name depends on the
   Helm release name).
4. Using the information from the previous two steps, run
   `kubectl debug -n <operator-namespace> -it <operator-manager-pod-name> --image=quay.io/curl/curl --profile=general --custom=<(echo '{"securityContext":{"runAsNonRoot":false,"runAsUser":0}}') -- sh`
  to get an interactive shell in the operator manager container.
  `kubectl debug` will attach a debug container with the image `quay.io/curl/curl` to the pod, take note of the
   container name (something like `debugger-xxxxx`).
5. Run `curl http://localhost:1777/debug/pprof/heap > /tmp/heap.out` in this shell.
   Leave the container running for the next step, i.e. do not exit the shell just yet.
6. In a separate shell, while the `kubectl debug` shell from the previous two steps is still running, run
  `kubectl cp -n <operator-namespace> -c <debug-containr-name> <operator-manager-pod-name>:/tmp/heap.out dash0-operator-manager-heap.out`.
  (The command might print `tar: removing leading '/' from member names` to stdout, this can be ignored.)
7. Exit the `kubectl debug` shell.
8. Redeploy the operator without the Helm setting `operator.pprofPort=1777`.

_To get a heap profile from a OpenTelemetry collector daemonset container:_
1. Deploy the operator manager with the additional Helm value `operator.collectors.enablePprofExtension=true`.
2. Take note of the namespace the operator is deployed in (default: `dash0-system`).
3. Run `kubectl top pod -n <operator-namespace> -l app.kubernetes.io/component=agent-collector` to get the name of a
   collector pod that has high memory usage (usually something like
   `dash0-operator-opentelemetry-collector-agent-daemonset-xxxxx`, but the name depends on the Helm release name).
4. Using the information from the previous two steps, run
   `kubectl debug -n <operator-namespace> -it <collector-daemonset-pod-name> --image=quay.io/curl/curl --profile=general --custom=<(echo '{"securityContext":{"runAsNonRoot":false,"runAsUser":0}}') -- sh`
   to get an interactive shell in the collector container.
   `kubectl debug` will attach a debug container with the image `quay.io/curl/curl` to the pod, take note of the
   container name (something like `debugger-xxxxx`).
5. Run `curl http://localhost:1777/debug/pprof/heap > /tmp/heap.out` in this shell.
   Leave the container running for the next step, i.e. do not exit the shell just yet.
6. In a separate shell, while the `kubectl debug` shell from the previous two steps is still running, run
   `kubectl cp -n <operator-namespace> -c <debug-containr-name> <collector-daemonset-pod-name>:/tmp/heap.out dash0-daemonset-collector.out`
   (The command might print `tar: removing leading '/' from member names` to stdout, this can be ignored.)
7. Exit the `kubectl debug` shell.
8. Redeploy the operator without the Helm setting `operator.collectors.enablePprofExtension=true`.

_To get a heap profile from a OpenTelemetry collector deployment container:_
* Follow the same steps as for the collector daemonset, but use
  `-l app.kubernetes.io/component=cluster-metrics-collector` in step (3.).
