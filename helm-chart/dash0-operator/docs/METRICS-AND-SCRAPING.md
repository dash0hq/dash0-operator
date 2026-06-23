# Metrics Collection and Prometheus Scraping

This document covers metrics collection, Prometheus endpoint scraping, profiling, and exporting data to other observability backends.

## Table of Contents

- [Configure Metrics Collection](#configure-metrics-collection)
  - [Resource Attributes for Prometheus Scraping](#resource-attributes-for-prometheus-scraping)
- [Scraping Prometheus Endpoints](#scraping-prometheus-endpoints)
- [Profiling](#profiling)
  - [Collecting Profiles with the OpenTelemetry eBPF Profiler](#collecting-profiles-with-the-opentelemetry-ebpf-profiler)
- [Exporting Data to Other Observability Backends](#exporting-data-to-other-observability-backends)
  - [Note regarding TLS when using arbitrary OTLP-compatible backends](#note-regarding-tls-when-using-arbitrary-otlp-compatible-backends)
- [Disable Self-Monitoring](#disable-self-monitoring)

## Configure Metrics Collection

By default, the operator collects metrics as follows:

* The operator collects node, pod, container, and volume metrics from the API server via the
  [Kubelet Stats Receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/kubeletstatsreceiver/README.md),
  cluster-level metrics from the Kubernetes API server via the
  [Kubernetes Cluster Receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/k8sclusterreceiver/README.md),
  and system metrics from the underlying nodes via the
  [Host Metrics Receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/hostmetricsreceiver/README.md).
  Collecting these metrics can be disabled per cluster by setting
  `kubernetesInfrastructureMetricsCollection.enabled: false` in the Dash0 operator configuration resource (or setting
  the value `operator.kubernetesInfrastructureMetricsCollectionEnabled` to `false` when deploying the operator
  configuration resource via the Helm chart). See [CONFIGURATION.md](CONFIGURATION.md#kubernetes-infrastructure-metrics-collection)
  for details.

* Namespace-scoped metrics (e.g. metrics related to a workload running in a specific namespace) will only be collected
  if the namespace is monitored, that is, there is a Dash0 monitoring resource in that namespace. See
  [CONFIGURATION.md](CONFIGURATION.md#enable-dash0-monitoring-for-a-namespace) for how to enable monitoring for a
  namespace.

* The Dash0 operator scrapes Prometheus endpoints on pods annotated with the `prometheus.io/*` annotations in monitored
  namespaces, as described in the section [Scraping Prometheus Endpoints](#scraping-prometheus-endpoints).
  This can be disabled per namespace by explicitly setting `prometheusScraping.enabled: false` in the Dash0 monitoring
  resource. See [CONFIGURATION.md](CONFIGURATION.md#prometheus-scraping) for details.

* Metrics which are not namespace-scoped (for example node metrics like `k8s.node.*` or host metrics like
  `system.cpu.utilization`) will always be collected, unless metrics collection is disabled globally for the cluster
  (`kubernetesInfrastructureMetricsCollection.enabled: false`, see above).
  An operator configuration resource with exports settings has to be present in the cluster, otherwise no metrics
  collection takes place. See [CONFIGURATION.md](CONFIGURATION.md#configuring-the-dash0-backend-connection) for details
  on export configuration.

* Disabling or enabling individual metrics via configuration is not supported.
* Changing the frequency of metrics collection is not supported.

### Resource Attributes for Prometheus Scraping

When the operator scrapes Prometheus endpoints on pods, it does not have access to all the same metadata that is
available to the OpenTelemetry SDK in an instrumented application.
For that reason, resource attributes including the service name might be different.
The operator makes an effort to derive reasonable resource attributes.

The service name is derived as follows:

1. If the scraped service provides the `target_info` metric with a `service_name` attribute, that service name will be
   used.
   See
   https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/compatibility/prometheus_and_openmetrics.md#resource-attributes
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

> **Note:** In contrast to resource attributes for workloads via labels and annotations (see
> [AUTO-INSTRUMENTATION.md](AUTO-INSTRUMENTATION.md#specifying-additional-resource-attributes-via-labels-and-annotations)),
> Prometheus scraping can only see pod labels, not workload level (deployment, daemonset, ...) labels.

## Scraping Prometheus Endpoints

The Dash0 operator automatically scrapes Prometheus endpoints on pods annotated with the `prometheus.io/*` annotations
as defined by the
[Prometheus Helm chart](https://github.com/prometheus-community/helm-charts/tree/main/charts/prometheus#scraping-pod-metrics-via-annotations).

The supported annotations are:

* **`prometheus.io/scrape`**: Only scrape pods that have a value of `true`, except if `prometheus.io/scrape-slow` is set
  to `true` as well.
  Endpoints on pods annotated with this annotation are scraped every minute, i.e., scrape interval is 1 minute, unless
  `prometheus.io/scrape-slow` is also set to `true`.
* **`prometheus.io/scrape-slow`**: If set to `true`, enables scraping for the pod with scrape interval of 5 minutes.
  If both `prometheus.io/scrape` and `prometheus.io/scrape-slow` are annotated on a pod with both values set to `true`,
  the pod will be scraped every 5 minutes.
* **`prometheus.io/scheme`**: If the metrics endpoint is secured then you will need to set this to `https`.
* **`prometheus.io/path`**: Override the metrics endpoint path if it is not the default `/metrics`.
* **`prometheus.io/port`**: Override the metrics endpoint port if it is not the default `9102`.

To be scraped, a pod annotated with the `prometheus.io/scrape` or `prometheus.io/scrape-slow` annotations must belong to
namespaces that are configured to be monitored by the Dash0 operator (see
[CONFIGURATION.md](CONFIGURATION.md#enable-dash0-monitoring-for-a-namespace)).

The scraping of a pod is executed from the same Kubernetes node the pod resides on.

This feature can be disabled for a namespace by explicitly setting `prometheusScraping.enabled: false` in the Dash0
monitoring resource. See [CONFIGURATION.md](CONFIGURATION.md#prometheus-scraping) for details.

> **Note:** To also have [Kube state metrics](https://github.com/kubernetes/kube-state-metrics) (which are used
> extensively in [Awesome Prometheus alerts](https://samber.github.io/awesome-prometheus-alerts/)) scraped and
> delivered to Dash0, you can annotate the kube-state-metrics pod with `prometheus.io/scrape: "true"` and add a Dash0
> monitoring resource to the namespace it is running in.

## Profiling

The Dash0 operator can be configured to accept, process, and export
[profiling data](https://opentelemetry.io/docs/specs/otel/profiles/) via OTLP.

**Note:** The Dash0 Operator does not currently support collecting profiles, see the
[Collecting Profiles with the OpenTelemetry eBPF Profiler](#collecting-profiles-with-the-opentelemetry-ebpf-profiler)
section.

To enable profiling support, set the `operator.profilingEnabled` Helm value to `true`:

```console
helm install \
  --set operator.profilingEnabled=true \
  ... \
  dash0-operator \
  dash0-operator/dash0-operator
```

Alternatively, you can set `spec.profiling.enabled: true` directly on the `Dash0OperatorConfiguration` custom resource:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration-resource
spec:
  profiling:
    enabled: true
  # ... other settings
```

When profiling is enabled, you can use the same `spec.filter` and `spec.transform` settings on your `Dash0Monitoring`
resources to filter and transform profiling data, just like for traces, metrics, and logs.
See [CONFIGURATION.md](CONFIGURATION.md#filtering-and-transforming-telemetry) for details.

### Collecting Profiles with the OpenTelemetry eBPF Profiler

Since the standard OpenTelemetry auto-instrumentation agents do not yet emit OTLP profiles, a separate profiling agent
is needed to generate profiling data, like the
[OpenTelemetry eBPF profiler](https://github.com/open-telemetry/opentelemetry-ebpf-profiler).

The eBPF profiler is distributed as a specialized OpenTelemetry Collector (`otelcol-ebpf-profiler`) that includes a
`profiling` receiver.
It runs as a privileged DaemonSet with host PID access, since it relies on eBPF to collect stack traces from all
processes on the node.

Below is an example of deploying the eBPF profiler to send profiles to the Dash0 operator's collector:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ebpf-profiler-config
  namespace: dash0-system  # same namespace as the operator
data:
  config.yaml: |
    receivers:
      profiling:

    exporters:
      otlp/collector:
        endpoint: ${helm-release-name}-opentelemetry-collector-service.${namespace-of-the-dash0-operator}.svc.cluster.local:4317
        # The operator's OTLP receiver listens on plain-text gRPC (no TLS), so the exporter must
        # be configured accordingly. Without this, the gRPC exporter defaults to requiring TLS.
        tls:
          insecure: true

    service:
      telemetry:
        logs:
          level: info
      pipelines:
        profiles:
          receivers: [profiling]
          exporters: [otlp/collector]
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: ebpf-profiler
  namespace: dash0-system  # same namespace as the operator
  labels:
    app: ebpf-profiler
spec:
  selector:
    matchLabels:
      app: ebpf-profiler
  template:
    metadata:
      labels:
        app: ebpf-profiler
    spec:
      hostPID: true
      containers:
        - name: ebpf-profiler
          image: otel/opentelemetry-collector-ebpf-profiler:0.148.0
          args:
            - --config=file:/etc/otelcol/config.yaml
            - --feature-gates=service.profilesSupport
          securityContext:
            privileged: true
          volumeMounts:
            - name: config
              mountPath: /etc/otelcol
            - name: proc
              mountPath: /proc
              readOnly: true
            - name: sys
              mountPath: /sys
              readOnly: true
      volumes:
        - name: config
          configMap:
            name: ebpf-profiler-config
        - name: proc
          hostPath:
            path: /proc
        - name: sys
          hostPath:
            path: /sys
```

Replace `${helm-release-name}` and `${namespace-of-the-dash0-operator}` with the actual Helm release name and namespace.

The eBPF profiler requires:

* Linux kernel 5.4 or later.
* The container must run as privileged (or with `CAP_SYS_ADMIN`, `CAP_PERFMON`, and `CAP_BPF` capabilities).
* `hostPID: true` to observe processes running on the node.
* Access to `/proc` and `/sys` from the host.

Once both profiling support in the operator and the eBPF profiler DaemonSet are deployed, profiling data will flow from
the eBPF profiler through the operator's collector pipelines (including processors like `k8s_attributes` for Kubernetes
metadata enrichment) and on to the configured backend.

## Exporting Data to Other Observability Backends

Instead of `spec.exports[].dash0` in the Dash0 operator configuration resource, you can also provide
`spec.exports[].http` or `spec.exports[].grpc` to export telemetry data to arbitrary OTLP-compatible backends, or to
another local OpenTelemetry collector.

Here is an example for HTTP:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  exports:
    - http:
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
  exports:
    - grpc:
        endpoint: ... # provide the OTLP gRPC endpoint of your observability backend here
        headers: # you can optionally provide additional headers, for example for authorization
          - name: X-My-Header
            value: my-value
```

Export to multiple backends is also supported. The supplied backends can be either of the same type or of different
types. In the following example the telemetry would be sent to two different datasets in Dash0 and in addition to a
gRPC endpoint:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration
spec:
  exports:
    - dash0:
        dataset: dataset-one
        endpoint: ingress... # TODO needs to be replaced with the actual value
        authorization:
          token: auth_... # TODO needs to be replaced with the actual value
    - dash0:
        dataset: dataset-two
        endpoint: ingress... # TODO needs to be replaced with the actual value
        authorization:
          token: auth_... # TODO needs to be replaced with the actual value
    - grpc:
        endpoint: ... # provide the OTLP gRPC endpoint of your observability backend here
```

For more details on configuring exports, see [CONFIGURATION.md](CONFIGURATION.md#configuring-the-dash0-backend-connection).

### Note regarding TLS when using arbitrary OTLP-compatible backends

#### gRPC

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
  exports:
    - grpc:
        endpoint: ...             # provide the secure OTLP gRPC endpoint of your observability backend here
        insecureSkipVerify: true  # disables the verification of the server's certificate chain
```

Please note that it is a validation error to set both `insecure` and `insecureSkipVerify` explicitly to true at the same
time, since `insecureSkipVerify` is only applicable when using TLS.

#### HTTP

- For HTTP, the connection security is automatically detected based on whether the endpoint URL starts with `http://` or
  `https://`
- When using TLS, you can set `insecureSkipVerify: true` to disable the verification of the server's certificate chain,
  which can be useful when using self-signed certificates.

## Disable Self-Monitoring

By default, self-monitoring is enabled for the Dash0 operator as soon as you deploy a Dash0 operator configuration
resource with exports.
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
  exports:
    - # ... see CONFIGURATION.md for details on the exports settings
```

For more details on self-monitoring, see [CONFIGURATION.md](CONFIGURATION.md#self-monitoring).
