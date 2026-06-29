# Metrics Collection and Prometheus Scraping

This document covers metrics collection, Prometheus endpoint scraping, profiling, and exporting data to other observability backends.

## Table of Contents

- [Configure Metrics Collection](#configure-metrics-collection)
  - [Resource Attributes for Prometheus Scraping](#resource-attributes-for-prometheus-scraping)
- [Scraping Prometheus Endpoints](#scraping-prometheus-endpoints)
- [Support for Prometheus CRDs](#support-for-prometheus-crds)
  - [Authorization](#authorization)
  - [Configuring Resource Requests/Limits for the Target-Allocator](#configuring-resource-requestslimits-for-the-target-allocator)

## Configure Metrics Collection

By default, the operator collects metrics as follows:

* The operator collects node, pod, container, and volume metrics from the API server via the
  [Kubelet Stats Receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/kubeletstatsreceiver/README.md),
  cluster-level metrics from the Kubernetes API server via the
  [Kubernetes Cluster Receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/k8sclusterreceiver/README.md),
  and system metrics from the underlying nodes via the
  [Host Metrics Receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/hostmetricsreceiver/README.md).
  Collecting these metrics can be disabled per cluster by setting
  [`kubernetesInfrastructureMetricsCollection.enabled: false`](configuration.md#operatorconfigurationresource.spec.kubernetesInfrastructureMetricsCollection.enabled) in the Dash0 operator configuration resource (or setting
  the value `operator.kubernetesInfrastructureMetricsCollectionEnabled` to `false` when deploying the operator
  configuration resource via the Helm chart).

* Namespace-scoped metrics (e.g. metrics related to a workload running in a specific namespace) will only be collected
  if the namespace is monitored, that is, there is a Dash0 monitoring resource in that namespace. See
  [Enable Dash0 Monitoring for a Namespace](configuration.md#enable-dash0-monitoring-for-a-namespace) for how to enable monitoring for a
  namespace.

* The Dash0 operator scrapes Prometheus endpoints on pods annotated with the `prometheus.io/*` annotations in monitored
  namespaces, as described in the section [Scraping Prometheus Endpoints](#scraping-prometheus-endpoints).
  This can be disabled per namespace by explicitly setting
  [`prometheusScraping.enabled: false`](configuration.md#monitoringresource.spec.prometheusScraping.enabled) in the Dash0 monitoring
  resource.

* Metrics which are not namespace-scoped (for example node metrics like `k8s.node.*` or host metrics like
  `system.cpu.utilization`) will always be collected, unless metrics collection is disabled globally for the cluster
  (`kubernetesInfrastructureMetricsCollection.enabled: false`, see above).
  An operator configuration resource with exports settings has to be present in the cluster, otherwise no metrics
  collection takes place. See [Configuring the Dash0 Backend Connection](configuration.md#configuring-the-dash0-backend-connection) for details
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
> [Specifying Additional Resource Attributes via Labels and Annotations](auto-instrumentation.md#specifying-additional-resource-attributes-via-labels-and-annotations)),
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
[Enable Dash0 Monitoring for a Namespace](configuration.md#enable-dash0-monitoring-for-a-namespace)).

The scraping of a pod is executed from the same Kubernetes node the pod resides on.

This feature can be disabled for a namespace by explicitly setting
[`prometheusScraping.enabled: false`](configuration.md#monitoringresource.spec.prometheusScraping.enabled) in the Dash0 monitoring
resource.

> **Note:** To also have [Kube state metrics](https://github.com/kubernetes/kube-state-metrics) (which are used
> extensively in [Awesome Prometheus alerts](https://samber.github.io/awesome-prometheus-alerts/)) scraped and
> delivered to Dash0, you can annotate the kube-state-metrics pod with `prometheus.io/scrape: "true"` and add a Dash0
> monitoring resource to the namespace it is running in.

## Support for Prometheus CRDs

If you would like to enable support for Prometheus CRDs:
1. Ensure the CRDs (`ServiceMonitor`, `PodMonitor`, `ScrapeConfig`) are installed in the cluster
2. Include `--set operator.prometheusCrdSupportEnabled=true` when running `helm install`

Alternatively, if you are creating the operator configuration resource manually, set `spec.prometheusCrdSupport.enabled: true` in the operator configuration resource.
Refer to [Configuration](configuration.md) for details.

The operator supports the following CRDs:
- `ServiceMonitor`
- `PodMonitor`
- `ScrapeConfig` with `kubernetesSDConfigs`

The Dash0 Operator uses the [OpenTelemetry Target Allocator](https://github.com/open-telemetry/opentelemetry-operator/tree/main/cmd/otel-allocator) to watch Prometheus CRDs and assign targets to the collector running on the same node as the monitored workload.

### Authorization

If the scraped endpoints require authorization, it is mandatory to configure mTLS for the communication between the OpenTelemetry Target Allocator and the collectors, so the credentials can be transfered in a secure manner.

We recommend to use [cert-manager](https://cert-manager.io/) for the creation of the certificates/secrets, but any secrets following the [kubernetes.io/tls](https://kubernetes.io/docs/concepts/configuration/secret/#tls-secrets) secret type and providing `ca.crt`, `tls.crt`, and `tls.key` should be compatible.
Note that the server and client certificates need to be signed by the same CA to be trusted (i.e. two random self-signed certificates won't work).

You can find an example of minimal issuers and certificates for cert-manager in [this template](https://github.com/dash0hq/dash0-operator/blob/main/test-resources/cert-manager/ta-issuers-and-cert.yaml.template).

The secrets must be created in the Dash0 operator namespace.

Once you have created the required secrets holding the certificates, you can enable mTLS and set the secret names via the Helm chart:

```yaml
operator:
  targetAllocator:
    mTls:
      enabled: true
      serverCertSecretName: "ta-mtls-server-cert-secret"
      clientCertSecretName: "ta-mtls-client-cert-secret"
```

### Configuring Resource Requests/Limits for the Target-Allocator

Depending on individual requirements (like the number of watched resources), it might be necessary to increase the resource requests/limits of the target-allocator.
This can be achieved by setting the respective fields via Helm:

```yaml
operator:
  targetAllocator:
    containerResources:
      limits:
        cpu: 200m
        memory: 500Mi
      gomemlimit: 400MiB
      requests:
        cpu: 200m
        memory: 128Mi
```

## Related Documentation

- [Profiling](profiling.md) - Profiling support and OpenTelemetry eBPF profiler setup
- [Configuration](configuration.md) - Configuration options
- [Advanced Configuration](advanced-configuration.md) - Advanced topics including exporting to other backends
