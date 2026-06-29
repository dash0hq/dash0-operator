# Configuration

This guide covers all configuration options for the Dash0 operator, including backend connections, namespace monitoring, secrets, and operator settings.

## Configuration

### Configuring the Dash0 Backend Connection

**You can skip this step if you provided `--set operator.dash0Export.enabled=true` together with the endpoint and either
a token or a secret reference when running `helm install`.**
In that case, proceed to the next section,
[Enable Dash0 Monitoring For a Namespace](#enable-dash0-monitoring-for-a-namespace).

Otherwise, configure the backend connection now by creating a file `dash0-operator-configuration.yaml` with the
following content:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  exports:
    - dash0:
        # Replace this value with the actual OTLP/gRPC endpoint of your Dash0 organization.
        endpoint: ingress... # TODO needs to be replaced with the actual value, see below

        authorization:
          # Provide the Dash0 authorization token as a string via the token property:
          token: auth_... # TODO needs to be replaced with the actual value, see below

        apiEndpoint: https://api.....dash0.com # TODO needs to be replaced with the actual value, see below

  clusterName: my-kubernetes-cluster # optional, see below
```

Here is a list of configuration options for this resource:

* <a href="#operatorconfigurationresource.spec.exports[]"><span id="operatorconfigurationresource.spec.exports[]">**`spec.exports[]`**</span></a>:
  One or more `export` configs defining endpoints and authorization (see below for details).
  If multiple exports are defined, the telemetry will be exported to all defined exports and CRs (views, synthetic
  checks, dashboards, check rules, notification channels, spam filters, signal-to-metrics rules) will be synced to all
  defined Dash0 exports.

* <a href="#operatorconfigurationresource.spec.exports[].dash0.endpoint"><span id="operatorconfigurationresource.spec.exports[].dash0.endpoint">**`spec.exports[].dash0.endpoint`**</span></a>:
  The URL of the Dash0 ingress endpoint to which telemetry data will be sent.
  This property is mandatory.
  Replace the value in the example above with the OTLP/gRPC endpoint of your Dash0 organization.
  The correct OTLP/gRPC endpoint can be copied from https://app.dash0.com → organization settings → "Endpoints" →
  "OTLP/gRPC".
  Note that the correct endpoint value will always start with `ingress.` and end in `dash0.com:4317`.
  Including a protocol prefix (e.g. `https://`) is optional.

* **`spec.exports[].dash0.authorization.token`** or **`spec.exports[].dash0.authorization.secretRef`**:
  Exactly one of these two properties needs to be provided.
  Providing both will cause a validation error when installing the Dash0Monitoring resource.

    * <a href="#operatorconfigurationresource.spec.export.dash0.authorization.token"><span id="operatorconfigurationresource.spec.export.dash0.authorization.token">**`spec.export.dash0.authorization.token`**</span></a>:
      Replace the value in the example above with the Dash0 authorization token of your organization.
      The authorization token for your Dash0 organization can be copied from https://app.dash0.com → organization
      settings → "Auth Tokens".
      The prefix `Bearer ` must not be included in the value.
      Note that the value will be rendered verbatim into a Kubernetes ConfigMap object.
      Anyone with API access to the Kubernetes cluster will be able to read the value.
      Use the `secretRef` property and a Kubernetes secret if you want to avoid that.

    * <a href="#operatorconfigurationresource.spec.export.dash0.authorization.secretRef"><span id="operatorconfigurationresource.spec.export.dash0.authorization.secretRef">**`spec.export.dash0.authorization.secretRef`**</span></a>:
      A reference to an existing Kubernetes secret in the Dash0 operator's namespace.
      See the section [Using a Kubernetes Secret for the Dash0 Authorization Token](#using-a-kubernetes-secret-for-the-dash0-authorization-token)
      for an example file that uses a `secretRef`.
      The secret needs to contain the Dash0 authorization token.
      See below for details on how exactly the secret should be created and configured.
      Note that by default, Kubernetes secrets are stored _unencrypted_, and anyone with API access to the Kubernetes
      cluster will be able to read the value.
      Additional steps are required to make sure secret values are encrypted.
      See https://kubernetes.io/docs/concepts/configuration/secret/ for more information on Kubernetes secrets.

* <a href="#operatorconfigurationresource.spec.exports[].dash0.apiEndpoint"><span id="operatorconfigurationresource.spec.exports[].dash0.apiEndpoint">**`spec.exports[].dash0.apiEndpoint`**</span></a>:
  The base URL of the Dash0 API to talk to.
  This is not where telemetry will be sent, but it is used for managing dashboards, check rules, synthetic checks,
  views, notification channels, spam filters and signal-to-metrics rules via the operator.
  This property is optional.
  The value needs to be the API endpoint of your Dash0 organization.
  The correct API endpoint can be copied from https://app.dash0.com → organization settings → "Endpoints" → "API".
  The correct endpoint value will always start with "https://api." and end in ".dash0.com".
  If this property is omitted, managing dashboards, check rules, synthetic checks, views, notification channels, spam
  filters and signal-to-metrics rules via the operator will not work.

* <a href="#operatorconfigurationresource.spec.selfMonitoring.enabled"><span id="operatorconfigurationresource.spec.selfMonitoring.enabled">**`spec.selfMonitoring.enabled`**</span></a>:
  An opt-out for self-monitoring for the operator.
  If enabled, the operator will collect self-monitoring telemetry and send it to the configured Dash0 backend.
  This setting is optional, it defaults to `true`.

* <a href="#operatorconfigurationresource.spec.kubernetesInfrastructureMetricsCollection.enabled"><span id="operatorconfigurationresource.spec.kubernetesInfrastructureMetricsCollection.enabled">**`spec.kubernetesInfrastructureMetricsCollection.enabled`**</span></a>:
  If enabled, the operator will collect Kubernetes infrastructure metrics.
  This setting is optional, it defaults to `true`; unless `telemetryCollection.enabled` is set to `false`, then
  `kubernetesInfrastructureMetricsCollection.enabled` defaults to `false` as well.
  It is a validation error to set `telemetryCollection.enabled=false` and
  `kubernetesInfrastructureMetricsCollection.enabled=true` at the same time.

* <a href="#operatorconfigurationresource.spec.collectPodLabelsAndAnnotations.enabled"><span id="operatorconfigurationresource.spec.collectPodLabelsAndAnnotations.enabled">**`spec.collectPodLabelsAndAnnotations.enabled`**</span></a>:
  If enabled, the operator will collect all Kubernetes pod labels and annotations and convert them to resource
  attributes for all spans, log records and metrics.
  The resulting resource attributes are prefixed with `k8s.pod.label.` or `k8s.pod.annotation.` respectively.
  This setting is optional, it defaults to `true`; unless `telemetryCollection.enabled` is set to `false`, then
  `collectPodLabelsAndAnnotations.enabled` defaults to `false` as well.
  It is a validation error to set `telemetryCollection.enabled=false` and `collectPodLabelsAndAnnotations.enabled=true`
  at the same time.

* <a href="#operatorconfigurationresource.spec.clusterName"><span id="operatorconfigurationresource.spec.clusterName">**`spec.clusterName`**</span></a>:
  If set, the value will be added as the resource attribute `k8s.cluster.name` to all telemetry.
  This setting is optional.
  By default, `k8s.cluster.name` will not be added to telemetry.

* <a href="#operatorconfigurationresource.spec.telemetryCollection.enabled"><span id="operatorconfigurationresource.spec.telemetryCollection.enabled">**`spec.telemetryCollection.enabled`**</span></a>:
  An opt-out switch for all telemetry collection, and to avoid having the operator deploy OpenTelemetry collectors in
  the cluster.
  This setting is optional, it defaults to `true` (that is, by default, OpenTelemetry collectors will be deployed and
  telemetry will be collected).
  If telemetry collection is disabled via this switch, the operator will not collect any telemetry, in particular it
  will not deploy any OpenTelemetry collectors in the cluster.
  This is useful if you want to use the operator for infrastructure-as-code (e.g. to synchronize dashboards & check
  rules), but do not want it to deploy the OpenTelemetry collector.
  Note that setting this to `false` does not disable the operator's self-monitoring telemetry, use the setting
  `spec.selfMonitoring.enabled` to disable self-monitoring if required (self-monitoring does not require an
  OpenTelemetry collector).
  Also note that this setting is not exposed via Helm, i.e. if you want to set this to `false` you need to deploy the
  operator configuration resource manually, i.e. omit the Helm value `operator.dash0Export.enabled` or set it to
  `false`, then deploy an operator configuration resource via `kubectl apply -f` or similar.

* <a href="#operatorconfigurationresource.spec.prometheusCrdSupport.enabled"><span id="operatorconfigurationresource.spec.prometheusCrdSupport.enabled">**`spec.prometheusCrdSupport.enabled`**</span></a>:
  A flag controlling whether support for Prometheus CRDs will be enabled.
  This setting is optional and the default is `false`.
  Setting it to `true` and having at least one namespace with `prometheusScraping` enabled, will deploy the
  OpenTelemetry target-allocator and update the `prometheusreceiver` in the OpenTelemetry collectors, so they query the
  allocator for targets to be scraped.

* <a href="#operatorconfigurationresource.spec.instrumentWorkloads.instrumentationDelivery"><span id="operatorconfigurationresource.spec.instrumentWorkloads.instrumentationDelivery">**`spec.instrumentWorkloads.instrumentationDelivery`**</span></a>:
  Whether to use an image volume or an init container plus an emptyDir volume to provide instrumentation files to
  workloads when applying auto-instrumentation.
  See [Using Image Volumes for Auto-Instrumentation Files](#using-image-volumes-for-auto-instrumentation-files).
  Allowed values:
  - `auto`: use image volumes if the Kubernetes version is 1.36 or later, otherwise use the init container
    approach.
  - `image-volume`: always use image volumes, also on Kubernetes versions older than 1.36. If the Kubernetes
    version is older than 1.31, the operator manager will log a warning and fall back to the init container
    approach, since image volumes are not supported in that version. Note that if you are using
    Kubernetes 1.34 or earlier, and you want to use this setting, you need to enable image volumes when configuring
    your cluster, since image volumes are disabled by default in versions older than 1.35.
  - `init-container`: always use the init container approach, regardless of the Kubernetes version.
    This is the default.

* <a href="#operatorconfigurationresource.spec.autoMonitorNamespaces.enabled"><span id="operatorconfigurationresource.spec.autoMonitorNamespaces.enabled">**`spec.autoMonitorNamespaces.enabled`**</span></a>: Controls whether monitoring is set up for namespaces
  automatically.
  By default, a Dash0Monitoring resource has to be added to each namespace that you want to monitor.
  With automatic namespace monitoring, you can let the Dash0 operator automate this.
  This is useful if you want to monitor all or almost all namespaces in your cluster.
  It is also useful if you create new namespaces frequently and want to have them monitored right away, without
  additional setup.
  It is best suited if almost all namespace should be monitored in the same fashion.
  If enabled, the operator will:
    * automatically add monitoring to all existing namespaces at startup, and
    * automatically add monitoring to new namespaces, as they are created.
  Even when enabled, individual namespaces can opt out of automatic monitoring via [label selectors](#operatorconfigurationresource.spec.autoMonitorNamespaces.labelSelector).
  Namespaces which are subject to automatic namespace monitoring will be monitored according to the settings of the
  [monitoringTemplate](#operatorconfigurationresource.spec.monitoringTemplate).

* <a href="#operatorconfigurationresource.spec.autoMonitorNamespaces.labelSelector"><span id="operatorconfigurationresource.spec.autoMonitorNamespaces.labelSelector">**`operatorconfigurationresource.spec.autoMonitorNamespaces.labelSelector`**</span></a>:
  An optional configurable label selector for controlling which namespaces are automatically monitored.
  Namespaces which match this label selector will be monitored automatically (if `autoMonitorNamespaces.enabled` is
  set to `true`).
  Namespaces which do not match this label selector will not be monitored, regardless of the value of
  `autoMonitorNamespaces.enabled`.
  By default, this label selector has the value `"dash0.com/enable!=false"` - that is, the following namespaces will
  be monitored:
    * namespaces which do not have the label dash0.com/enable at all, and
    * namespaces which have the label dash0.com/enable with a value other than "false".
  Namespaces which are subject to automatic namespace monitoring will be monitored according to the settings of the
  [monitoringTemplate](#operatorconfigurationresource.spec.monitoringTemplate).

* <a href="#operatorconfigurationresource.spec.monitoringTemplate"><span id="operatorconfigurationresource.spec.monitoringTemplate">**`operatorconfigurationresource.spec.monitoringTemplate`**</span></a>:
  Specification of the desired settings for automatically monitoring namespaces.

After providing the required values (at least `endpoint` and `authorization`), save the file and apply the resource to
the Kubernetes cluster you want to monitor:

```console
kubectl apply -f dash0-operator-configuration.yaml
```

The Dash0 operator configuration resource is cluster-scoped, so a specific namespace should not be provided when
applying it.

> **Note:** All configuration options available in the operator configuration resource can also be configured when
> letting the Helm chart auto-create this resource, as explained in the section [Installation](installation.md).
> You can consult the chart's
> [values.yaml](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/values.yaml) file for a
> complete list of available configuration settings.

### Notes on Creating the Operator Configuration Resource Via Helm

Providing the backend connection settings to the operator via Helm parameters is a convenience mechanism to get
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
`helm upgrade ... dash0-operator dash0-operator/dash0-operator`, or Kubernetes moving the operator manager pod to a
different node.

For this reason, when using this feature, it is recommended to treat the Helm values as the source of truth for the
operator configuration.
Any changes you want to be permanent should be applied via Helm and the `operator.dash0Export.*` settings.

If you would rather retain manual control over the operator configuration resource, you should omit any
`operator.dash0Export.*` Helm values and create and manage the operator configuration resource manually (that is, via
kubectl, ArgoCD etc.).

### Enable Dash0 Monitoring For a Namespace

> **Note:** As an alternative to enabling monitoring per namespace, you can also
> [monitor all namespaces automatically](#automatic-namespace-monitoring).

> **Note:** By default, when enabling Dash0 monitoring for a namespace, all workloads in this namespace will be
> restarted to apply the Dash0 instrumentation.
> If you want to avoid this, set the `instrumentWorkloads` property in the monitoring resource spec to
> `created-and-updated`.
> See below for more information on the `instrumentWorkloads` modes.

For each namespace that you want to monitor with Dash0, enable monitoring by installing a Dash0 monitoring resource into
that namespace:

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

> **Note:** Even when no monitoring resources has been installed and no namespace is being monitored by Dash0, the Dash0
> operator's collector will collect Kubernetes infrastructure metrics that are not namespace scoped, like node-related
> metrics.
> The only prerequisite for this is an [operator configuration](#configuring-the-dash0-backend-connection) with
> exports settings.

### Disable Dash0 Monitoring For a Namespace

If you want to stop monitoring a namespace with Dash0, remove the Dash0 monitoring resource from that namespace.
For example, if you want to stop monitoring workloads in the namespace `my-nodejs-applications`, use the following command:

```console
kubectl delete --namespace my-nodejs-applications Dash0Monitoring dash0-monitoring-resource
```

or, alternatively, by using the `dash0-monitoring.yaml` file created earlier:

```console
kubectl delete --namespace my-nodejs-applications -f dash0-monitoring.yaml
```

### Additional Configuration Per Namespace

The Dash0 monitoring resource supports additional configuration settings:

* <a href="#monitoringresource.spec.instrumentWorkloads.mode"><span id="monitoringresource.spec.instrumentWorkloads.mode"><span id="monitoringresource.spec.instrumentWorkloads">**`spec.instrumentWorkloads.mode`**</span></span></a>:
  A namespace-wide configuration for the workload instrumentation strategy for the target namespace.
  There are three possible settings: `all`, `created-and-updated` and `none`.
  By default, the setting `all` is assumed; unless there is an operator configuration resource with
  `telemetryCollection.enabled=false`, then the setting `none` is assumed by default.
  Note that `spec.instrumentWorkloads.mode` is the path for this setting starting with version `v1beta1` of the Dash0
  monitoring resource; when using `v1alpha1`, the path is `spec.instrumentWorkloads`.

    * **`all`**: If set to `all`, the operator will:
        * instrument existing workloads in the target namespace (i.e. workloads already running in the namespace) when
          the Dash0 monitoring resource is deployed,
        * instrument existing workloads or update the instrumentation of already instrumented workloads in the target
          namespace when the Dash0 operator is first started or updated to a newer version,
        * instrument new workloads in the target namespace when they are deployed, and
        * instrument changed workloads in the target namespace when changes are applied to them.
        * **Note that the first two actions (instrumenting existing workloads) will result in restarting the pods of the
          affected workloads.**
          Use `created-and-updated` if you want to avoid pod restarts.

    * **`created-and-updated`**: If set to `created-and-updated`, the operator will not instrument existing workloads in
      the target namespace.
      Instead, it will only:
        * instrument new workloads in the target namespace when they are deployed, and
        * instrument changed workloads in the target namespace when changes are applied to them.
      This setting is useful if you want to avoid pod restarts as a side effect of deploying the Dash0 monitoring
      resource or restarting the Dash0 operator.

    * **`none`**: You can opt out of instrumenting workloads entirely by setting this option to `none`.
      With `spec.instrumentWorkloads: none`, workloads in the target namespace will never be instrumented to emit
      telemetry.

  If this setting is omitted, the value `all` is assumed and new/updated as well as existing Kubernetes workloads will
  be instrumented by the operator to emit telemetry, as described above.
  There is one exception to this rule: If there is an operator configuration resource with
  `telemetryCollection.enabled=false`, then the default setting is `none` instead of `all`, and no workloads will be
  instrumented by the Dash0 operator.

  More fine-grained per-workload control over instrumentation is available by setting the label
  `dash0.com/enable=false` on individual workloads, see
  [Disabling Auto-Instrumentation for Specific Workloads](auto-instrumentation.md#disabling-auto-instrumentation-for-specific-workloads).

  The behavior when changing this setting for an existing Dash0 monitoring resource is as follows:
    * When this setting is updated to `spec.instrumentWorkloads=all` (and it had a different value before):
      All existing uninstrumented workloads will be instrumented.
      Their pods will be restarted to apply the instrumentation.
    * When this setting is updated to `spec.instrumentWorkloads=none` (and it had a different value before):
      The instrumentation will be removed from all instrumented workloads.
      Their pods will be restarted to remove the instrumentation.
      (After this change, the operator will no longer instrument any workloads nor will it restart any pods.)
    * Updating this value to `spec.instrumentWorkloads=created-and-updated` has no immediate effect; existing
      uninstrumented workloads will not be instrumented, existing instrumented workloads will not be uninstrumented.
      Newly deployed or updated workloads will be instrumented from the point of the configuration change onwards as
      described above.

  Automatic workload instrumentation will automatically add tracing to your workloads.
  You can read more about what exactly this feature entails in the section
  [Automatic Workload Instrumentation](auto-instrumentation.md).

* <a href="#monitoringresource.spec.instrumentWorkloads.labelSelector"><span id="monitoringresource.spec.instrumentWorkloads.labelSelector">**`spec.instrumentWorkloads.labelSelector`**</span></a>:
  A custom Kubernetes [label selector](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors)
  for controlling the workload instrumentation on the level of individual workloads, see
  [Using a Custom Label Selector to Control Auto-Instrumentation](auto-instrumentation.md#using-a-custom-label-selector-to-control-auto-instrumentation).

* <a href="#monitoringresource.spec.instrumentWorkloads.traceContext.propagators"><span id="monitoringresource.spec.instrumentWorkloads.traceContext.propagators">**`spec.instrumentWorkloads.traceContext.propagators`**</span></a>:
  When set, the operator will add the environment variable `OTEL_PROPAGATORS` to all instrumented workloads in the
  target namespace.
  This environment variable determines which trace context propagation headers an OTel SDK uses.
  Setting this can be useful if the workloads in this namespace interact with services that do not use the W3C trace
  context standard header `traceparent` for trace context propagation, but for example AWS X-Ray (`X-Amzn-Trace-Id`).
  The value of the setting will be validated, it needs to be a comma-separated list of valid propagators.
  See <https://opentelemetry.io/docs/languages/sdk-configuration/general/#otel_propagators> for more information.

  When the option is not set, the operator will not set the environment variable `OTEL_PROPAGATORS`.
  If the option is set in the monitoring resource at some point and then later removed again, the operator will remove
  the environment variable from instrumented workloads if and only if the value of the environment variable matches the
  previously used setting in the monitoring resource.
  This is done to prevent accidentally removing an `OTEL_PROPAGATORS` environment variable that has been set manually
  and not by the operator.
  (For that purpose, the previous setting is stored in the monitoring resource's status.)

* <a href="#monitoringresource.spec.instrumentWorkloads.captureSqlQueryParameters"><span id="monitoringresource.spec.instrumentWorkloads.captureSqlQueryParameters">**`spec.instrumentWorkloads.captureSqlQueryParameters`**</span></a>:
  When set to `true`, the operator enables SQL query parameter capture in the language agents that support it.
  Currently this covers the OpenTelemetry Java agent's JDBC instrumentation and the OpenTelemetry .NET
  auto-instrumentation for Microsoft.Data.SqlClient and Entity Framework Core, by adding the following environment
  variables (all set to `true`) to instrumented containers in the target namespace:
  - `OTEL_INSTRUMENTATION_JDBC_EXPERIMENTAL_CAPTURE_QUERY_PARAMETERS`
  - `OTEL_DOTNET_EXPERIMENTAL_SQLCLIENT_ENABLE_TRACE_DB_QUERY_PARAMETERS`
  - `OTEL_DOTNET_EXPERIMENTAL_EFCORE_ENABLE_TRACE_DB_QUERY_PARAMETERS`

  Recorded query parameter values are added to the resulting database span attributes.
  Other language agents ignore these variables.

  Note that these are experimental upstream OpenTelemetry agent flags and may be renamed or removed in future agent
  releases.
  Recorded query parameter values may include sensitive data such as personally identifiable information (PII), so
  enable this only for namespaces where capturing query parameter values is acceptable.

  When the option is not set or set to `false`, the operator will not set the environment variables.
  If the option is set to `true` at some point and then later set to `false` or removed, the operator will remove each
  environment variable from instrumented workloads if and only if its current value still matches what the operator
  would have set (the literal `true`); a user-set value that differs is preserved.
  (For that purpose, the previous setting is stored in the monitoring resource's status.)

* <a href="#monitoringresource.spec.logCollection.enabled"><span id="monitoringresource.spec.logCollection.enabled">**`spec.logCollection.enabled`**</span></a>:
  A namespace-wide opt-out for collecting pod logs via the `filelog` receiver.
  If enabled, the operator will configure its OpenTelemetry collector to watch the log output of all pods in the
  namespace and send the resulting log records to Dash0.
  This setting is optional, it defaults to `true`; unless there is an operator configuration resource with
  `telemetryCollection.enabled=false`, then log collection is off by default.
  It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and
  `logCollection.enabled=true` in any monitoring resource at the same time.

* <a href="#monitoringresource.spec.prometheusScraping.enabled"><span id="monitoringresource.spec.prometheusScraping.enabled">**`spec.prometheusScraping.enabled`**</span></a>:
  A namespace-wide opt-out for Prometheus scraping for the target namespace.
  If enabled, the operator will configure its OpenTelemetry collector to scrape metrics from pods in the namespace
  of this Dash0Monitoring resource according to their `prometheus.io/scrape` annotations via the OpenTelemetry
  Prometheus receiver.
  In addition, if the operator configuration resource has `prometheusCrdSupport.enabled=true`, the collectors will scrape metrics
  from endpoints defined in Prometheus CRs (`ServiceMonitor`, `PodMonitor`, `ScrapeConfig`) present in this namespace.
  This setting is optional, it defaults to `true`; unless there is an operator configuration resource with
  `telemetryCollection.enabled=false`, then Prometheus scraping is off by default.
  It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and
  `prometheusScraping.enabled=true` in any monitoring resource at the same time.
  Note that the collection of OpenTelemetry-native metrics is not affected by setting `prometheusScraping.enabled=false`
  for a namespace.

* <a href="#monitoringresource.spec.filter"><span id="monitoringresource.spec.filter">**`spec.filter`**</span></a>:
  An optional custom filter configuration to drop some of the collected telemetry before sending it to the configured
  telemetry backend.
  Filters for a specific telemetry object type (e.g. spans) are lists of
  [OTTL](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/pkg/ottl/README.md) expressions.
  If at least one of the conditions of a list evaluates to true, the object will be dropped.
  (That is, conditions are implicitly connected by a logical OR.)
  The configuration structure is identical to the configuration of the OpenTelemetry collector's
  [filter processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/filterprocessor/README.md).
  One difference to the filter processor is that the filter rules configured in a Dash0 monitoring resource will only be
  applied to the telemetry collected in the namespace the monitoring resource is installed in.
  Telemetry from other namespaces is not affected.
  Existing configurations for the filter processor can be copied and pasted without syntactical changes.
    * **`spec.filter.traces.span`**:
      A list of OTTL conditions for filtering spans.
      All spans where at least one condition evaluates to true will be dropped.
      (That is, conditions are implicitly connected by a logical OR.)
    * **`spec.filter.traces.spanevent`**:
      A list of OTTL conditions for filtering span events.
      All span events where at least one condition evaluates to true will be dropped.
      If all span events for a span are dropped, the span will be left intact.
    * **`spec.filter.metrics.metric`**:
      A list of OTTL conditions for filtering metrics.
      All metrics where at least one condition evaluates to true will be dropped.
    * **`spec.filter.metrics.datapoint`**:
      A list of OTTL conditions for filtering individual data points of metrics.
      All data points where at least one condition evaluates to true will be dropped.
      If all datapoints for a metric are dropped, the metric will also be dropped.
    * **`spec.filter.logs.log_records`**:
      A list of OTTL conditions for filtering log records.
      All log records where at least one condition evaluates to true will be dropped.
    * **`spec.filter.profiles.profile`**:
      A list of OTTL conditions for filtering profiles.
      All profiles where at least one condition evaluates to true will be dropped.
  This setting is optional, by default, no filters are applied.
  It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and set
  filters in any monitoring resource at the same time.

  Note that although `error_mode` can be specified per namespace, the filter conditions will be aggregated into one
  single filter processor in the resulting OpenTelemetry collector configuration; if different error modes are
  specified in different namespaces, the "most severe" error mode will be used (propagate > ignore > silent).

* <a href="#monitoringresource.spec.transform"><span id="monitoringresource.spec.transform">**`spec.transform`**</span></a>:
  An optional custom transformation configuration that will be applied to the collected telemetry before sending it to
  the configured telemetry backend.
  Transformations for a specific telemetry signal (e.g. traces, metrics, logs, profiles) are lists of
  [OTTL](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/pkg/ottl/README.md) statements.
  All telemetry for the respective signal will be routed through all transformation statements.
  The statements are executed in the order they are listed.
  The configuration structure is identical to the configuration of the OpenTelemetry collector's
  [transform processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/transformprocessor/README.md).
  Both the
  [basic configuration style](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/transformprocessor/README.md#basic-config),
  and the
  [advanced configuration style](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/transformprocessor/README.md#advanced-config)
  of the transform processor are supported.
  One difference to the transform processor is that the transform rules configured in a Dash0 monitoring resource will
  only be applied to the telemetry collected in the namespace the monitoring resource is installed in.
  Telemetry from other namespaces is not affected.
  If both `spec.filter` and `spec.transform` are configured, the filtering for a given signal (traces, metrics, logs, profiles)
  will be executed before the transform processor.
  (That is, you cannot assume that transformations have already been applied when writing filter rules.)
  Existing configurations for the transform processor can be copied and pasted without syntactical changes.
    * **`spec.transform.trace_statements`**:
      A list of OTTL statements (or a list of groups in the advanced config style) for transforming trace telemetry.
    * **`spec.transform.metric_statements`**:
      A list of OTTL statements (or a list of groups in the advanced config style) for transforming metric telemetry.
    * **`spec.transform.log_statements`**:
      A list of OTTL statements (or a list of groups in the advanced config style) for transforming log telemetry.
    * **`spec.transform.profile_statements`**:
      A list of OTTL statements (or a list of groups in the advanced config style) for transforming profile telemetry.
  This setting is optional, by default, no transformations are applied.
  It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and set
  transforms in any monitoring resource at the same time.

  Note that although `error_mode` can be specified per namespace, the transform statements will be aggregated into one
  single transform processor in the resulting OpenTelemetry collector configuration; if different error modes are
  specified in different namespaces, the "most severe" error mode will be used (propagate > ignore > silent).

* <a href="#monitoringresource.spec.synchronizePersesDashboards"><span id="monitoringresource.spec.synchronizePersesDashboards">**`spec.synchronizePersesDashboards`**</span></a>:
  A namespace-wide opt-out for synchronizing Perses dashboard resources found in the target namespace.
  If enabled, the operator will watch Perses dashboard resources in this namespace and create corresponding dashboards
  in Dash0 via the Dash0 API.
  More fine-grained per-resource control over synchronization is available by setting the label
  `dash0.com/enable=false` on individual Perses dashboard resources.
  See [Managing Dash0 Dashboards](managing-dash0-resources.md#managing-dash0-dashboards) for details.
  This setting is optional, it defaults to `true`.

* <a href="#monitoringresource.spec.synchronizePrometheusRules"><span id="monitoringresource.spec.synchronizePrometheusRules">**`spec.synchronizePrometheusRules`**</span></a>:
  A namespace-wide opt-out for synchronizing Prometheus rule resources found in the target namespace.
  If enabled, the operator will watch Prometheus rule resources in this namespace and create corresponding check rules
  in Dash0 via the Dash0 API.
  More fine-grained per-resource control over synchronization is available by setting the label
  `dash0.com/enable=false` on individual Prometheus rule resources.
  See [Managing Dash0 Check Rules](managing-dash0-resources.md#managing-dash0-check-rules) for details.
  This setting is optional, it defaults to `true`.

#### Example

Here is a comprehensive example for a monitoring resource which:

* sets the instrumentation mode to `created-and-updated`,
* disables Prometheus scraping,
* sets a couple of filters for all six telemetry object types,
* applies transformations to limit the length of span attributes, datapoint attributes, log attributes, and profile attributes
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
    profiles:
      profile:
      - 'resource.attributes["k8s.pod.name"] == "debug-pod"'

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
    profile_statements:
    - 'truncate_all(profile.attributes, 1024)'

  synchronizePersesDashboards: false

  synchronizePrometheusRules: false
```

### Namespace-specific Exports and API-sync Overrides

It is possible to override the global/default exports config provided via the Dash0 operator configuration on a
per-namespace basis by providing the corresponding exports config via the Dash0 monitoring resource. Please note that
the namespace-specific overrides replace the default list of exports from the Dash0 operator configuration.

The override supports both the export of telemetry and the sync of resources, like dashboards or views, via the Dash0 API.

Note: The operator always expects default export and API-sync settings via the Dash0 operator configuration, even when
namespace-specific overrides are used.

### Automatic Namespace Monitoring

By default, a Dash0Monitoring resource has to be added to each namespace that you want to monitor (see
[Enable Dash0 Monitoring For a Namespace](#enable-dash0-monitoring-for-a-namespace)).
With automatic namespace monitoring, you can let the Dash0 operator automate this.
This is useful if you want to monitor all or almost all namespaces in your cluster.
It is also useful if you create new namespaces frequently and want to have them monitored right away, without additional
setup.
It is best suited if almost all namespace should be monitored in the same fashion.

Use the following Helm values to enable automatic namespace monitoring:

```
operator:

  dash0Export:
    # operator.dash0Export.enabled must be true to facilitate automatic namespace monitoring.
    # Refer to the section "Installation" for details.
    enabled: true
    ...

  autoMonitorNamespaces:
    # Setting operator.autoMonitorNamespaces.enabled=true activates automatic namespace monitoring.
    enabled: true
```

If automatic namespace monitoring is enabled, the operator will:
* automatically add monitoring to all existing namespaces at startup, and
* automatically add monitoring to new namespaces, as they are created.

Even when this feature is enabled, individual namespaces can opt out of automatic monitoring via label selectors.
Set `dash0.com/enable: "false"` on a namespace to exclude it from automatic namespace monitoring.

The namespace label selector is configurable.
For example, to use an opt-in approach instead of opt-out, something like this can be used:

```
operator:

  dash0Export:
    enabled: true
    ...

  autoMonitorNamespaces:
    enabled: true
    labelSelector: monitor-namespace-with-dash0==true
```

With this configuration, only namespaces that have the label `monitor-namespace-with-dash0: "true"` will be monitored.

The following namespaces will not be monitored by automatic namespace monitoring, regardless of the label selector:
* `kube-system`
* `kube-node-lease`
* `kube-public`
* the namespace of the Dash0 operator

You can deploy a monitoring resource to these namespaces manually though.

Automatically monitoring namespaces will be monitored with the following default settings:

* `instrumentWorkloads.mode`: `created-and-updated`
* Log Collection: Enabled
* Event Collection: Enabled
* Prometheues Scraping: Enabled
* Synchronize Perses Dashboards: Enabled
* Synchronize Prometheus Rules: Enabled

The defaults can be customized, as follows:

```
operator:
  dash0Export:
    enabled: true
    ...

  autoMonitorNamespaces:
    enabled: true

  monitoringTemplate:
    spec:
      instrumentWorkloads:
        mode: none
      logCollection:
        enabled: true
      eventCollection:
        enabled: true
      prometheusScraping:
        enabled: false
      synchronizePersesDashboards: false
      synchronizePrometheusRules: false
```

With the previous example, only logs and Kubernetes events would be collected in automatically monitored namespaces,
no workload auto-instrumentation would happen, and Prometheus metrics would not be scraped.

All settings mentioned in [Additional Configuration Per Namespace](#additional-configuration-per-namespace)) can be
set with the monitoring template, with the exception of per-namespace exports.
(Use the `operator.dash0Export` to configure the export instead.)

The following snippet shows all possible settings:

```
operator:
  monitoringTemplate:
    spec:
      instrumentWorkloads:
        mode: none
      logCollection:
        enabled: true
      eventCollection:
        enabled: true
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

If there are some namespaces which require different monitoring settings, exclude them from automatic namespace
monitoring via the label selector (e.g. `dash0.com/enable: "false"`) and deploy a Dash0Monitoring resource there
manually.

If the cluster has a lot of namespaces (e.g. close to or more than 1,000), it is recommended to set
`operator.collectors.compressConfigMaps` to `true`.
This will enable gzip compression for the ConfigMaps for the OpenTelemetry collectors.

Example:

```
operator:
  dash0Export:
    enabled: true
    ...

  collectors:
    compressConfigMaps: true

  autoMonitorNamespaces:
    enabled: true
```

### Using Image Volumes for Auto-Instrumentation Files

When using auto-instrumentation of workloads, by default the operator adds an
[`emptyDir` volume](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) and an
[init container](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) to provide the instrumentation
files to the workload.

Image volumes are a new Kubernetes feature that provides a better way to do this.
They provide the following advantages:
* No additional ephemeral storage usage.
* Faster workload startup (because nothing needs to be copied over from the init container to the empty dir volume)

They were introduced in Kubernetes 1.31 as an alpha feature behind a feature gate.
In version 1.33 they graduated to beta, but were still disabled by default.
Starting with version 1.35, the image volume feature gate is enabled by default, but they are still considered beta.
Image volumes finally became a stable feature in version 1.36

Set the [`instrumentationDelivery` setting](#operatorconfigurationresource.spec.instrumentWorkloads.instrumentationDelivery)
in the operator configuration resource to determine under which circumstances image volumes will be used instead of the
init container approach.
When using the operator manager to create and manage the operator configuration resource (i.e. with
`operator.dash0Export.enabled=true`), set `operator.instrumentation.delivery` in Helm to configure image volumes.

Allowed values for the instrumentation delivery setting:
- `auto`: use image volumes if the Kubernetes version is 1.36 or later, otherwise use the init container
  approach.
- `image-volume`: always use image volumes, also on Kubernetes versions older than 1.36. If the Kubernetes
  version is older than 1.31, the operator manager will log a warning and fall back to the init container
  approach, since image volumes are not supported in that version. Note that if you are using
  Kubernetes 1.34 or earlier, and you want to use this setting, you need to enable image volumes when configuring
  your cluster, since image volumes are disabled by default in versions older than 1.35.
- `init-container`: always use the init container approach, regardless of the Kubernetes version.
  This is the default.

Note: Changing the instrumentation delivery setting for an existing operator installation will not trigger a bulk
re-instrumentation of all existing workloads, even for namespaces that are set to `instrumentWorkloadsMode=all`.
Once a workload has been successfully instrumented, there is no benefit in re-instrumenting it with a different delivery
mechanism.
The new setting will be applied when instrumenting newly deployed workloads, or when a workload is updated/re-deployed.

### Python Auto-Instrumentation

To enable auto-instrumentation for Python workloads, set `operator.instrumentation.enablePythonAutoInstrumentation=true`
via Helm.
If this setting is enabled for an existing operator installation, Python auto-instrumentation will be enabled
immediately for workloads in namespaces that have a Dash0Monitoring resource with
[`instrumentWorkloads.mode`](#monitoringresource.spec.instrumentWorkloads.mode) `all`.
This will cause all pods in these namespaces to be restarted.
For workloads in namespaces that use `instrumentWorkloads.mode=created-and-updated`, it will become active with the next
re-deployment of the workload.
The setting has no effect on workloads in namespaces that use `instrumentWorkloads.mode=none` or do not have a
Dash0Monitoring resource.

Python auto-instrumentation is only supported for Python 3.9 or later.
If the Dash0 Python auto-instrumentation detects an incompatible Python version (i.e. version 3.8 or older), it will
automatically deactivate itself safely and print a warning to `stderr`:
```
[dash0] warning: cannot auto-instrument Python process: unsupported Python version: 3.8.0
```
This warning is also visible in the Dash0 UI's log view, unless log collection has been disabled for the namespace.
Update the Python version to enable automatic Python instrumentation by Dash0 for this workload.

Python auto-instrumentation only works if the configured OTLP export protocol is `http/protobuf`.
If the operator is managing the container's `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL` variables,
this will be set correctly automatically.
If the Dash0 Python auto-instrumentation detects an incompatible `OTEL_EXPORTER_OTLP_PROTOCOL` setting, it will
automatically deactivate itself safely and print a warning to `stderr`:
```
[dash0] warning: cannot auto-instrument Python process: OTEL_EXPORTER_OTLP_PROTOCOL=grpc is not supported
```
This can only happen if the container is setting its own `OTEL_EXPORTER_OTLP_ENDPOINT` and/or
`OTEL_EXPORTER_OTLP_PROTOCOL`.
Remove these environment variables from the pod spec template to enable automatic Python instrumentation by Dash0 for
this workload.

Dash0's Python auto-instrumentation is not compatible with workloads that are already instrumented, either
[manually](https://opentelemetry.io/docs/languages/python/instrumentation/) or using the
[zero-code instrumentation](https://opentelemetry.io/docs/zero-code/python/), e.g. the `opentelemetry-instrument`
wrapper.
If existing instrumentation is detected, the Dash0 Python auto-instrumentation will automatically deactivate itself
safely and print a warning to `stderr`:
```
[dash0] warning: cannot auto-instrument Python process: The application has OpenTelemetry dependencies which indicate
that it is already instrumented. The following problematic dependencies have been found: ...
Skipping the Dash0 Python auto-instrumentation to avoid double instrumentation.
```
This warning is also visible in the Dash0 UI's log view, unless log collection has been disabled for the namespace.
Remove the existing instrumentation from the workload to enable automatic Python instrumentation by Dash0 for
this, or leave the existing instrumentation in place, in which case Dash0 will refrain from instrumenting it.

Last but not least, due to the nature of Python's dependency management, Python auto-instrumentation has the potential
to introduce dependency conflicts.
The Dash0 Python auto-instrumentation checks for potential dependency conflicts before actually instrumenting a process.
If a dependency conflict is detected, the Dash0 Python auto-instrumentation will automatically deactivate itself safely
and print a warning to `stderr`:
```
[dash0] warning: cannot auto-instrument Python process: dependency conflicts: {'package-name': {'version_required': '>=20.0', 'version_found': '19.0'}}
```
This warning is also visible in the Dash0 UI's log view, unless log collection has been disabled for the namespace.
Resolve the version conflicts to enable automatic Python instrumentation by Dash0 for this workload, for example
by updating the dependency versions used by the workload.
If the conflicting dependencies cannot be resolved, you might need to instrument this workload individually, for
example by using the OpenTelemetry Python [zero-code instrumentation](https://opentelemetry.io/docs/zero-code/python/).

### Using a Kubernetes Secret for the Dash0 Authorization Token

If you want to provide the Dash0 authorization token via a Kubernetes secret instead of providing the token as a string,
create the secret in the namespace where the Dash0 operator is installed.
This also applies when providing a per-namespace export and API-sync config via a monitoring resource, i.e. the operator
will always try to look up the auth token secret in the operator namespace.
If you followed the guide above, the name of that namespace is `dash0-system`.
The authorization token for your Dash0 organization can be copied from https://app.dash0.com → organization settings →
"Auth Tokens".
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

If you do not want to install the operator configuration resource via `helm install` but instead deploy it manually, and
use a secret reference for the auth token, the following example YAML file would work with the secret created above:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  exports:
    - dash0:
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
  exports:
    - dash0:
        endpoint: ingress... # TODO needs to be replaced with the actual value, see above

        authorization:
          secretRef: {}

        apiEndpoint: https://api... # optional, see above
```

> **Note:** There are no defaults when using `--set operator.dash0Export.secretRef.name` and
> `--set operator.dash0Export.secretRef.key` with `helm install`, so for that approach the values must always be
> provided explicitly.

Note that by default, Kubernetes secrets are stored unencrypted, and anyone with API access to the Kubernetes cluster
will be able to read the value.
Additional steps are required to make sure secret values are encrypted, if that is desired.
See https://kubernetes.io/docs/concepts/configuration/secret/ for more information on Kubernetes secrets.

### Dash0 Dataset Configuration

Use the `spec.exports[].dash0.dataset` property to configure the dataset that should be used for the telemetry data.
By default, data will be sent to the dataset `default`.
Here is an example for a configuration that uses a different Dash0 dataset:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  exports:
    - dash0:
        endpoint: ingress... # see above

        dataset: my-custom-dataset # This optional setting determines the Dash0 dataset to which telemetry will be sent.

        authorization: # see above
          ...

        apiEndpoint: https://api... # optional, see above
```

