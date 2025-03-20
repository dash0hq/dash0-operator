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

* Java 8+
* Node.js 18+

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
  dashboards and check rules via the operator. This property is optional.
  The value needs to be the API endpoint of your Dash0 organization.
  The correct API endpoint can be copied fom https://app.dash0.com -> organization settings -> "Endpoints" -> "API".
  The correct endpoint value will always start with "https://api." and end in ".dash0.com".
  If this property is omitted, managing dashboards and check rules via the operator will not work.
* <a href="#operatorconfigurationresource.spec.selfMonitoring.enabled"><span id="operatorconfigurationresource.spec.selfMonitoring.enabled">`spec.selfMonitoring.enabled`</span></a>:
  An opt-out for self-monitoring for the operator.
  If enabled, the operator will collect self-monitoring telemetry and send it to the configured Dash0 backend.
  This setting is optional, it defaults to true.
* <a href="#operatorconfigurationresource.spec.kubernetesInfrastructureMetricsCollectionEnabled"><span id="operatorconfigurationresource.spec.kubernetesInfrastructureMetricsCollectionEnabled">`spec.kubernetesInfrastructureMetricsCollectionEnabled`</span></a>:
  If enabled, the operator will collect Kubernetes infrastructure metrics.
  This setting is optional, it defaults to true.
* <a href="#operatorconfigurationresource.spec.clusterName"><span id="operatorconfigurationresource.spec.clusterName">`spec.clusterName`</span></a>:
  If set, the value will be added as the resource attribute `k8s.cluster.name` to all telemetry.
  This setting is optional. By default, `k8s.cluster.name` will not be added to telemetry.

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

Note: Even when no monitoring resources has been installed and no namespace is being monitored by Dash0, the Dash0
operator's collector will collect Kubernetes infrastructure metrics that are not namespace scoped, like node-related
metrics. The only prerequisite for this is an [operator configuration](#configuring-the-dash0-backend-connection) with
export settings.

### Additional Configuration Per Namespace

The Dash0 monitoring resource supports additional configuration settings:

* <a href="#monitoringresource.spec.instrumentWorkloads"><span id="monitoringresource.spec.instrumentWorkloads">`spec.instrumentWorkloads`</span></a>:
  A namespace-wide configuration for the workload instrumentation strategy for the target namespace.
  There are three possible settings: `all`, `created-and-updated` and `none`.
  By default, the setting `all` is assumed.

  * `all`: If set to `all` (or omitted), the operator will:
      * instrument existing workloads in the target namespace (i.e. workloads already running in the namespace) when
        the Dash0 monitoring resource is deployed,
      * instrument existing workloads or update the instrumentation of already instrumented workloads in the target
        namespace when the Dash0 operator is first started or updated to a newer version,,
      * instrument new workloads in the target namespace when they are deployed, and
      * instrument changed workloads in the target namespace when changes are applied to them.
    Note that the first two actions (instrumenting existing workloads) will result in restarting the pods of the
    affected workloads.
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

  More fine-grained per-workload control over instrumentation is available by setting the label
  `dash0.com/enable=false` on individual workloads, see
  [Disabling Auto-Instrumentation for Specific Workloads](#disabling-auto-instrumentation-for-specific-workloads).

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

* <a href="#monitoringresource.spec.synchronizePersesDashboards"><span id="monitoringresource.spec.synchronizePersesDashboards">`spec.synchronizePersesDashboards`</span></a>:
  A namespace-wide opt-out for synchronizing Perses dashboard resources found in the target namespace.
  If enabled, the operator will watch Perses dashboard resources in this namespace and create corresponding dashboards
  in Dash0 via the Dash0 API.
  See [Managing Dash0 Dashboards](#managing-dash0-dashboards) for details. This setting is optional, it defaults to
  true.

* <a href="#monitoringresource.spec.synchronizePrometheusRules"><span id="monitoringresource.spec.synchronizePrometheusRules">`spec.synchronizePrometheusRules`</span></a>:
  A namespace-wide opt-out for synchronizing Prometheus rule resources found in the target namespace.
  If enabled, the operator will watch Prometheus rule resources in this namespace and create corresponding check rules
  in Dash0 via the Dash0 API.
  See [Managing Dash0 Check Rules](#managing-dash0-check-rules) for details.
  This setting is optional, it defaults to true.

* <a href="#monitoringresource.spec.prometheusScrapingEnabled"><span id="monitoringresource.spec.prometheusScrapingEnabled">`spec.prometheusScrapingEnabled`</span></a>:
  A namespace-wide opt-out for Prometheus scraping for the target namespace.
  If enabled, the operator will configure its OpenTelemetry collector to scrape metrics from pods in the namespace
  of this Dash0Monitoring resource according to their prometheus.io/scrape annotations via the OpenTelemetry Prometheus
  receiver.
  This setting is optional, it defaults to true. Note that the collection of OpenTelemetry-native metrics is not
  affected by setting `prometheusScrapingEnabled` to `false` for a namespace.

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


Here is comprehensive example for a monitoring resource which
* sets the instrumenation mode to `created-and-updated`,
* disables Perses dashboard synchronization,
* disable Prometheus rule synchronization,
* disables Prometheus scraping,
* sets a couple of filters for all five telemetry object types, and
* applies transformations to limit the length of span attributes, datapoint attributes, and log attributes
  (with the metric transform using the advanced transform config style).

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
    - 'truncate_all(span.attributes, 4096)'
    metric_statements:
      - conditions:
        - 'metric.type == METRIC_DATA_TYPE_SUM'
        statements:
        - 'truncate_all(datapoint.attributes, 4096)'
    log_statements:
    - 'truncate_all(log.attributes, 4096)'
```

The Dash0 operator will instrument the following workload types:

* [CronJobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)
* [DaemonSets](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/)
* [Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
* [Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
* [Pods](https://kubernetes.io/docs/concepts/workloads/pods/)
* [ReplicaSets](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/)
* [StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)

Note that Kubernetes jobs and Kubernetes pods are only instrumented at deploy time, _existing_ jobs and pods cannot be
instrumented since there is no way to restart them. For all other workload types, the operator can instrument existing
workloads as well as new workloads at deploy time (depending on the setting of
[`instrumentWorkloads`](#monitoringresource.spec.instrumentWorkloads) in the Dash0 monitoring resource).

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
  [`kubernetesInfrastructureMetricsCollectionEnabled: false`](#operatorconfigurationresource.spec.kubernetesInfrastructureMetricsCollectionEnabled)
  in the Dash0 operator configuration resource (or setting the value
  `operator.kubernetesInfrastructureMetricsCollectionEnabled` to `false` when deploying the operator configuration
  resource via the Helm chart).
* Namespace-scoped metrics (e.g. metrics related to a workload running in a specific namespace) will only be collected
  if the namespace is monitored, that is, there is a Dash0 monitoring resource in that namespace.
* The Dash0 operator scrapes Prometheus endpoints on pods annotated with the `prometheus.io/*` annotations in monitored
  namespaces, as described in the section [Scraping Prometheus endpoints](#scraping-prometheus-endpoints).
  This can be disabled per namespace by explicitly setting
  [`prometheusScrapingEnabled: false`](#monitoringresource.spec.prometheusScrapingEnabled) in the Dash0 monitoring
  resource.
* Metrics which are not namespace-scoped (for example node metrics like `k8s.node.*` or host metrics like
  `system.cpu.utilization`) will always be collected, unless metrics collection is disabled globally for the cluster
  (`kubernetesInfrastructureMetricsCollectionEnabled: false`, see above).
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

Changing this setting while the operator is already running requires a `helm upgrade`/`helm upgrade --reuse-values` or
similar to take effect.

Note: The tolerations will be added to the daemonset collector pods, but not to the deployment collector pod.

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

### Disabling Auto-Instrumentation for Specific Workloads

In namespaces that are Dash0-monitoring enabled, all supported workload types are automatically instrumented for
tracing. This process will modify the workload specification, e.g. by adding environment variables, Kubernetes labels
and an init container. Although this will only result in automatic tracing for supported runtimes, the modifications are
performed for every workload (as there is no way to tell which runtime a workload uses from the outside).

You can disable these workload modifications for specific workloads by setting the label `dash0.com/enable: "false"` in
the top level metadata section of the workload specification.

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

<span id="specifying-additional-resource-attributes-for-Workloads-via-labels-and-annotations"><!-- local anchor redirect after renaming the topic --></span>
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
to enable tracing for [supported runtimes](#supported-runtimes) out of the box, and to improve Kubernetes-related
resource attribute auto-detection.
This allows Dash0 users to avoid the hassle of manually adding the OpenTelemetry SDK to their applications.
Dash0 takes care of it automatically.

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

Additionally, the Dash0 injector automatically improves Kubernetes-related resource attributes as follows: The operator
sets the environment variables `DASH0_NAMESPACE_NAME`, `DASH0_POD_NAME`, `DASH0_POD_UID` and `DASH0_CONTAINER_NAME` on
workloads.
The Dash0 injector binary picks these values up and uses them to populate the resource attributes `k8s.namespace.name`,
`k8s.pod.name`, `k8s.pod.uid` and `k8s.container.name` via the `OTEL_RESOURCE_ATTRIBUTES` environment variable.
If `OTEL_RESOURCE_ATTRIBUTES` is already set on the process, the key-value pairs for these attributes are appended to
the existing value of `OTEL_RESOURCE_ATTRIBUTES`.
If `OTEL_RESOURCE_ATTRIBUTES` was not set on the process, the Dash0 injector will add `OTEL_RESOURCE_ATTRIBUTES` as
a new environment variable.

If you are curious, the source code for the injector is open source and can be found
[here](https://github.com/dash0hq/dash0-operator/blob/main/images/instrumentation/injector/src/dash0_injector.c).

## Scraping Prometheus Endpoints

The Dash0 operator automatically scrapes Prometheus endpoints on pods labelled with the `prometheus.io/*` annotations as
defined by the
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
[`prometheusScrapingEnabled: false`](#monitoringresource.spec.prometheusScrapingEnabled) in the Dash0 monitoring
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
  has the setting [`synchronizePrometheusRules`](#monitoringresource.spec.synchronizePrometheusRules) set to `false`.
  (This setting is optional and defaults to `true` when omitted.)

Furthermore, the custom resource definition for Prometheus rules needs to be installed in the cluster. There are two
ways to achieve this:
* Install the Prometheus rules custom resource definition with the following command:
```console
kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.81.0/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml
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
* The group attribute `limit` is not supported by Dash0 and will be ignored.
* The group attribute `partial_response_strategy` is not supported by Dash0 and will be ignored.
* All labels (except for the ones explicitly mentioned in the conversion table below) will be listed under
  "Additional labels".
* All annotations (except for the ones explicitly mentioned in the conversion table below) will be listed under
  "Annotations".

| Prometheus alert rule attribute        | Dash0 Check rule field                                                                                                         | Notes                                                                                                                                                      |
|----------------------------------------|--------------------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `alert`                                | Name of check rule, prefixed by the group name                                                                                 | must be a non-empty string                                                                                                                                 |
| `expr`                                 | "Expression"                                                                                                                   | must be a non-empty string                                                                                                                                 |
| `interval` (from group)                | "Evaluate every"                                                                                                               |                                                                                                                                                            |
| `for`                                  | "Grace periods"/"For"                                                                                                          | default: "0s"                                                                                                                                              |
| `keep_firing_for`                      | "Grace periods"/"Keep firing for"                                                                                              | default: "0s"                                                                                                                                              |
| `annotations/summary`                  | "Summary"                                                                                                                      |                                                                                                                                                            |
| `annotations/description`              | "Description"                                                                                                                  |                                                                                                                                                            |
| `annotations/dash0-threshold-degraded` | Will be used in place of the token `$__threshold` in the expression, to determine whether the check is _degraded_              | If present, needs to a be string that can be parsed to a float value according to the syntax described in https://go.dev/ref/spec#Floating-point_literals. |
| `annotations/dash0-threshold-critical` | Will be used in place of the token `$__threshold` in the expression, to determine whether the check is _critical_              | If present, needs to a be string that can be parsed to a float value according to the syntax described in https://go.dev/ref/spec#Floating-point_literals. |
| `annotations/*`                        | "Annotations"                                                                                                                  |
| `labels/*`                             | "Additional labels"                                                                                                            |

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
