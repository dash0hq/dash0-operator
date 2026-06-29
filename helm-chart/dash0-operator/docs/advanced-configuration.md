# Advanced Configuration

This guide covers advanced configuration topics for the Dash0 operator, including exporting to other backends, certificate management, collector pod scheduling, and performance tuning.

## Table of Contents

- [Providing a Filelog Offset Volume](#providing-a-filelog-offset-volume)
- [Using cert-manager](#using-cert-manager)
- [Controlling On Which Nodes the Operator's Collector Pods Are Scheduled](#controlling-on-which-nodes-the-operators-collector-pods-are-scheduled)
  - [Allow Scheduling on Tainted Nodes](#allow-scheduling-on-tainted-nodes)
  - [Preventing Operator Scheduling on Specific Nodes](#preventing-operator-scheduling-on-specific-nodes)
  - [Custom Node Affinity](#custom-node-affinity)
  - [Adding Custom Labels and Annotations to the Collector Resources](#adding-custom-labels-and-annotations-to-the-collector-resources)
- [Configuring Pod-Level sysctls for the Collector Pods (TCP Keepalive)](#configuring-pod-level-sysctls-for-the-collector-pods-tcp-keepalive)
- [Disable Self-Monitoring](#disable-self-monitoring)
- [Exporting Data to Other Observability Backends](#exporting-data-to-other-observability-backends)
  - [Note regarding TLS when using arbitrary OTLP-compatible backends](#note-regarding-tls-when-using-arbitrary-otlp-compatible-backends)

## Providing a Filelog Offset Volume

The operator's collector uses the filelog receiver to read pod log files for monitored workloads.
When the collector is restarted (which can happen for various reasons, for example to apply configuration changes), it is important that the filelog receiver can continue reading the log files from where it left off.
If the filelog receiver started to read all log files from the beginning again after a restart, log records would be duplicated, that is, they would appear multiple times in Dash0.

For that purpose, the filelog receiver stores the log file offsets in persistent storage.
By default, the offsets are stored in a config map in the operator's namespace.
For small- to medium-sized clusters, this is usually sufficient, and it requires no additional configuration by users.
For larger clusters or clusters with many short-lived pods, we recommend providing a persistent volume for storing
offsets.

Any persistent volume that is accessible from the collector pods can be used for this purpose.

Here is an example with a `hostPath` volume (see also https://kubernetes.io/docs/concepts/storage/volumes/#hostpath for
considerations around using `hostPath` volumes):

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

Here is another example based on persistent volume claims.
(This assumes that a PersistentVolumeClaim named `offset-storage-claim` exists.)
See also https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims and
https://kubernetes.io/docs/concepts/storage/persistent-volumes/#reclaiming.

```yaml
operator:
  collectors:
    filelogOffsetSyncStorageVolume:
      name: filelogreceiver-offsets
      persistentVolumeClaim:
        claimName: offset-storage-claim
```

> **Important:** Since this volume is needed by a Daemonset run by the Dash0 operator, the PersistentVolumeClaim needs
> to be set with the `ReadWriteMany` access mode.

### When to Use a Volume for Filelog Offsets

Using a volume instead of the default config map approach is also helpful if you have webhooks in your cluster which process every config map update.

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

For more details on OPA, Kyverno, and other platform-specific considerations, see [Platform Specific](platform-specific.md).

## Using cert-manager

When installing the Helm chart, it generates TLS certificates on the fly for all components that need certificates (the
operator's webhook service and its metrics service).
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

The following annotated example is a minimal configuration that matches the Dash0 operator configuration snippet shown
above.
Both configuration snippets assume that the Dash0 operator is installed in the default `dash0-system` namespace, and the
Certificate and Issuer resource are also deployed into that namespace.

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

## Controlling On Which Nodes the Operator's Collector Pods Are Scheduled

### Allow Scheduling on Tainted Nodes

The operator uses a Kubernetes daemonset to deploy the OpenTelemetry collector on each node; to collect telemetry from
that node and workloads running on that node.
If you use [taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) on certain nodes,
Kubernetes will not schedule any pods there, preventing the daemonset collector pods to be present on these nodes.
You can allow the daemonset collector pods to be scheduled there by configuring tolerations matching your taints for the
collector pods.
Tolerations can be configured as follows:

```yaml
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

Changing Helm settings while the operator is already running requires a `helm upgrade`/`helm upgrade --reuse-values` or similar to take effect.

### Preventing Operator Scheduling on Specific Nodes

All the pods deployed by the operator have a default node anti-affinity for the `dash0.com/enable=false` node label.
That is, if you add the `dash0.com/enable=false` label to a node, none of the pods owned by the operator will be
scheduled on that node.

> **IMPORTANT:** This includes the daemonset that the operator will set up to receive telemetry from the pods, which
> might lead to situations in which instrumented pods cannot send telemetry because the local node does not have a
> daemonset collector pod.
> In other words, if you want to monitor workloads with the Dash0 operator and use the `dash0.com/enable=false` node
> anti-affinity, make sure that the workloads you want to monitor have the same anti-affinity:

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

### Custom Node Affinity

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

### Adding Custom Labels and Annotations to the Collector Resources

Additional labels and annotations can be added to the collector resources and the target-allocator resource managed by the operator, both to the workload objects (the daemonset and the deployments) and to their pods.
These are merged with the labels and annotations the operator sets itself; the operator-managed entries always take precedence, so they cannot be overridden.

```yaml
operator:
  collectors:
    # labels/annotations for the daemonset collector
    daemonSetLabels:
      my-label: my-value
    daemonSetAnnotations:
      my-annotation: my-value
    daemonSetPodLabels:
      my-pod-label: my-value
    daemonSetPodAnnotations:
      my-pod-annotation: my-value

    # labels/annotations for the cluster-metrics-collector deployment
    deploymentLabels:
      my-label: my-value
    deploymentAnnotations:
      my-annotation: my-value
    deploymentPodLabels:
      my-pod-label: my-value
    deploymentPodAnnotations:
      my-pod-annotation: my-value

  targetAllocator:
    # labels/annotations for the target-allocator deployment
    labels:
      my-label: my-value
    annotations:
      my-annotation: my-value
    podLabels:
      my-pod-label: my-value
    podAnnotations:
      my-pod-annotation: my-value
```

## Configuring Pod-Level sysctls for the Collector Pods (TCP Keepalive)

Pod-level [sysctls](https://kubernetes.io/docs/tasks/administer-cluster/sysctl-cluster/) can be applied to the
collector pods via `operator.collectors.daemonSetSysctls` (for the daemonset collector) and
`operator.collectors.deploymentSysctls` (for the cluster-metrics-collector deployment).
Both default to being unset, so no sysctls are applied unless you configure them.

The primary use case is forcing TCP keepalive on the collector's network namespace.
On some environments the connection-tracking layer reaps idle established TCP connections after a relatively short
timeout (for example, AWS Nitro v6 instance types such as `m8i`, `m8i-flex`, `r8i` and `c8i` reap them after 350
seconds).
The collector's self-monitoring connection is sparse and can idle longer than that; once the conntrack entry is reaped,
the next config reload blocks on the export deadline and the collector pod is restarted.
Lowering the TCP keepalive interval below the conntrack idle timeout keeps the connection alive and avoids these
restarts:

```yaml
operator:
  collectors:
    daemonSetSysctls:
      - name: net.ipv4.tcp_keepalive_time
        value: "240"
      - name: net.ipv4.tcp_keepalive_intvl
        value: "60"
      - name: net.ipv4.tcp_keepalive_probes
        value: "3"
    deploymentSysctls:
      - name: net.ipv4.tcp_keepalive_time
        value: "240"
      - name: net.ipv4.tcp_keepalive_intvl
        value: "60"
      - name: net.ipv4.tcp_keepalive_probes
        value: "3"
```

`net.ipv4.tcp_keepalive_time` is the idle time before the first keepalive probe is sent and must be lower than the
environment's conntrack idle timeout (for example, below 350 seconds on AWS Nitro v6); `tcp_keepalive_intvl` and
`tcp_keepalive_probes` only govern dead-peer detection.

This setting is opt-in for two reasons:

* The `net.ipv4.tcp_keepalive_*` sysctls are only considered _safe_ sysctls on Kubernetes 1.29 and later. On older clusters, or on clusters where the kubelet's `forbiddenSysctls` / a strict Pod Security Admission policy disallows them, the kubelet rejects these sysctls and the collector pods will fail to be scheduled.
* It changes the collector pod spec, which triggers a rollout of the collector pods.

Changing Helm settings while the operator is already running requires a `helm upgrade`/`helm upgrade --reuse-values` or similar to take effect.

## Disable Self-Monitoring

By default, self-monitoring is enabled for the Dash0 operator as soon as you deploy a Dash0 operator configuration resource with exports.
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
    - # ... see configuration.md for details on the exports settings
```

For more details on self-monitoring, see [Self-Monitoring](configuration.md#self-monitoring).

## Exporting Data to Other Observability Backends

Instead of `spec.exports[].dash0` in the Dash0 operator configuration resource, you can also provide `spec.exports[].http` or `spec.exports[].grpc` to export telemetry data to arbitrary OTLP-compatible backends, or to another local OpenTelemetry collector.

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

Export to multiple backends is also supported.
The supplied backends can be either of the same type or of different types.
In the following example the telemetry would be sent to two different datasets in Dash0 and in addition to a gRPC endpoint:

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

For more details on configuring exports, see [Configuring the Dash0 Backend Connection](configuration.md#configuring-the-dash0-backend-connection).

### Note regarding TLS when using arbitrary OTLP-compatible backends

#### gRPC

- By default, a secure connection is assumed, unless explicitly setting `insecure: true`, or when the `insecure` field is omitted and the endpoint URL starts with `http://`
- When using TLS, you can set `insecureSkipVerify: true` to disable the verification of the server's certificate chain, which can be useful when using self-signed certificates.

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

Please note that it is a validation error to set both `insecure` and `insecureSkipVerify` explicitly to true at the same time, since `insecureSkipVerify` is only applicable when using TLS.

#### HTTP

- For HTTP, the connection security is automatically detected based on whether the endpoint URL starts with `http://` or `https://`
- When using TLS, you can set `insecureSkipVerify: true` to disable the verification of the server's certificate chain, which can be useful when using self-signed certificates.

## Related Documentation

* [Configuration](configuration.md) - Backend connections and namespace monitoring
* [Platform Specific](platform-specific.md) - Platform-specific notes including OPA and Kyverno compatibility
* [Installation](installation.md) - Installation guide
