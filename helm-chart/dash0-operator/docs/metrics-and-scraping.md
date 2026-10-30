# Metrics Collection and Prometheus Scraping

This document covers metrics collection, Prometheus endpoint scraping, profiling, and exporting data to other observability backends.

## Table of Contents

- [Configure Metrics Collection](#configure-metrics-collection)
  - [Resource Attributes for Prometheus Scraping](#resource-attributes-for-prometheus-scraping)
- [Scraping Prometheus Endpoints](#scraping-prometheus-endpoints)
- [Support for Prometheus CRDs](#support-for-prometheus-crds)
  - [Authorization](#authorization)
    - [Certificate Renewal](#certificate-renewal)
    - [Allowing Insecure Auth Secrets](#allowing-insecure-auth-secrets)
  - [Restricting Access to Secrets](#restricting-access-to-secrets)
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

If the scraped endpoints require authorization, the credentials need to be transferred from the OpenTelemetry Target Allocator to the collectors in a secure manner.
By default, the Target Allocator only serves the credentials over mTLS; see [Allowing Insecure Auth Secrets](#allowing-insecure-auth-secrets) for an alternative if the transport security is already provided by other means.

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

#### Certificate Renewal

The Target Allocator reloads its certificate, key and CA certificate from the server certificate secret at runtime.
The collectors reload the client certificate and key from the client certificate secret at runtime, at most five minutes after they have changed.
They cannot reload the CA certificate at runtime; when the `ca.crt` entry of the client certificate secret changes, the operator restarts the collector pods.

The Target Allocator verifies the client certificate only against the `ca.crt` entry of the server certificate secret, and the collectors verify the server certificate only against the `ca.crt` entry of the client certificate secret.
If the CA is renewed with a new key, and the server and client certificates are not renewed at the same time, the two sides trust different CAs until both certificates have been renewed.
In the meantime, the collectors cannot fetch their scrape targets from the Target Allocator.
To prevent this, the certificates should be set up as follows:

* The CA has a much longer duration than the server and client certificates, so that it is not renewed together with them.
* The CA keeps its private key when it is renewed, so that certificates signed before and after the renewal are valid for both the old and the new CA certificate.
  With cert-manager, set `privateKey.rotationPolicy: Never` on the CA certificate; since cert-manager v1.18, the default is to generate a new key on every renewal.
* The server and client certificates are renewed before the old CA certificate in their secrets expires.
  With cert-manager, which renews certificates after two thirds of their duration by default, keep the duration of the server and client certificates well below one third of the CA's duration.

The [example template](https://github.com/dash0hq/dash0-operator/blob/main/test-resources/cert-manager/ta-issuers-and-cert.yaml.template) follows these rules.

If the two sides trust different CAs, the collectors log `x509: certificate signed by unknown authority` errors, and the Target Allocator logs `tls: bad certificate` errors.
Collector pods that start in this state fail their startup probe and are restarted repeatedly.
To recover, renew the server or client certificate that has been signed with the old CA key, for example with `cmctl renew <certificate-name> -n <operator-namespace>`.

If you have created your certificates with an earlier version of the example template, applying the updated template does not reissue them.
The CA certificate keeps its 90 day duration and is still renewed together with the server and client certificates.
To migrate:

1. Apply the updated template, with `$OPERATOR_NAMESPACE` replaced by the operator namespace.
2. Renew the CA certificate with `cmctl renew ta-mtls-ca -n <operator-namespace>`.
   Since the CA now keeps its private key, the existing server and client certificates stay valid.
3. Wait until `kubectl get certificate ta-mtls-ca -n <operator-namespace> -o jsonpath='{.status.notAfter}'` shows a date two years in the future.
4. Renew the server and client certificates with `cmctl renew ta-server-cert ta-client-cert -n <operator-namespace>`.
   This puts the renewed CA certificate into the `ca.crt` entries of their secrets.

#### Allowing Insecure Auth Secrets

If the transport security between the OpenTelemetry Target Allocator and the collectors is already guaranteed by other means, for example by a service mesh, you can skip the mTLS setup and let the Target Allocator serve the credentials over plain HTTP instead:

```yaml
operator:
  targetAllocator:
    allowInsecureAuthSecrets: true
```

Do not enable this setting unless transport-level security is guaranteed by other means, otherwise the credentials for the scraped endpoints are transferred unencrypted.
See the [upstream documentation](https://github.com/open-telemetry/opentelemetry-operator/blob/main/docs/target-allocator/README.md#alternative-allow-insecure-auth-secrets) for details.

### Restricting Access to Secrets

To resolve credentials referenced by Prometheus CRDs, the Target Allocator needs read access to secrets.
By default, the Target Allocator (and the operator manager, which grants this permission to the Target Allocator) can read secrets in all namespaces.
You can restrict this access to a list of namespaces via the Helm chart:

```yaml
operator:
  targetAllocator:
    allowSecretAccessInNamespaces:
      - namespace-a
      - namespace-b
```

If this list is set, the Target Allocator can only read secrets in the listed namespaces, and the operator manager only in the Dash0 operator namespace.
The list only restricts access to secrets.
It does not change which namespaces are watched.
The Target Allocator still watches Prometheus CRDs in all namespaces that have a Dash0 monitoring resource with [Prometheus scraping](configuration.md#monitoringresource.spec.prometheusScraping.enabled) enabled (which is the default), provided that [support for Prometheus CRDs](#support-for-prometheus-crds) is enabled.
In a namespace that is not on the list, Prometheus CRDs without a secret reference are scraped as usual.
Prometheus CRDs that reference a secret are rejected by the Target Allocator, and their targets are not scraped.
This means that authentication via secrets (for example basic auth, a bearer token or OAuth2) and TLS settings from secrets (for example a CA certificate for verifying the target's certificate, or a client certificate) do not work in such a namespace.
Listing a namespace does not enable scraping in it by itself.
Scraping pods via the `prometheus.io/*` annotations (see [Scraping Prometheus Endpoints](#scraping-prometheus-endpoints)) is not affected by this list.

All listed namespaces need to exist when the Helm chart is installed or upgraded, since a role binding is created in each of them.
If a listed namespace is deleted and recreated later, its role binding is only restored by the next Helm upgrade.

When the list is removed again via `helm upgrade`, the role bindings are deleted right away, while the operator grants cluster-wide access to secrets to the Target Allocator only once it has picked up the changed configuration.
Credentials that the Target Allocator has not resolved before can fail to resolve during that time.
They are resolved again within a few minutes.

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
