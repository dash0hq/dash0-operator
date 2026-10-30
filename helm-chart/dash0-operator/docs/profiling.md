# Profiling

This document covers profiling support in the Dash0 operator.

## Table of Contents

- [Enabling Profiling Support](#enabling-profiling-support)
- [Operator-managed eBPF profiler](#operator-managed-ebpf-profiler)
- [Collecting Profiles with the OpenTelemetry eBPF Profiler](#collecting-profiles-with-the-opentelemetry-ebpf-profiler)

## Enabling Profiling Support

The Dash0 operator can be configured to accept, process, and export [profiling data](https://opentelemetry.io/docs/specs/otel/profiles/) via OTLP.

To collect profiles, opt in to the [operator-managed eBPF profiler](#operator-managed-ebpf-profiler), or deploy the
[OpenTelemetry eBPF profiler](#collecting-profiles-with-the-opentelemetry-ebpf-profiler) separately.

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

When profiling is enabled, you can use the same `spec.filter` and `spec.transform` settings on your `Dash0Monitoring` resources to filter and transform profiling data, just like for traces, metrics, and logs.
See [Filtering and Transforming Telemetry](configuration.md#filtering-and-transforming-telemetry) for details.

**Note:** [Namespace-specific exports](configuration.md#namespace-specific-exports-and-api-sync-overrides) (`spec.export` or `spec.exports` on a `Dash0Monitoring` resource) do not apply to profiling data.
Profiles from all namespaces are sent to the default export of the operator configuration (for example via the `operator.dash0Export` or `operator.exports` Helm values), including profiles from namespaces that have a namespace-specific export.
The reason is that the operator sends telemetry to namespace-specific exports via the routing connector of the OpenTelemetry collector, which does not support profiles.

## Operator-managed eBPF profiler

Set `operator.ebpfProfilerEnabled: true` to let the operator deploy a Parca agent DaemonSet in the operator namespace.
Telemetry collection must remain enabled. The chart rejects `operator.telemetryCollectionEnabled: false` with this opt-in.
For a Helm-managed `Dash0OperatorConfiguration`, the opt-in also enables the profiling pipeline and sets
`spec.profiling.ebpfProfiler.enabled: true`. If you manage that resource yourself, set both
`spec.profiling.enabled: true` and `spec.profiling.ebpfProfiler.enabled: true` there.

### Agent image

The profiler defaults to `ghcr.io/parca-dev/parca-agent:v0.51.2`. This version is independent of the operator's
`appVersion`; enabling the profiler does not require an image override.

[Parca agent v0.51.2](https://github.com/parca-dev/parca-agent/releases/tag/v0.51.2) supports the separate
`--debuginfo-address` connection, multiple upload instructions through `supports_multiple_uploads`, and retries
indicated by `retry_required`. It also includes the startup and Go-label fixes from
[PR #3257](https://github.com/parca-dev/parca-agent/pull/3257): an unavailable debuginfo proxy does not prevent profile
sampling from starting, and Go labels remain available when Go symbolization is disabled. Version `v0.51.1` adds local
pod-UID discovery, retention of GPU/NVTX process identity until export, and external-label propagation across all OTLP
signals. Earlier versions do not provide the source marker needed by the collector's profiler routing. Version
`v0.51.2` also exposes PID namespace translation for environments with nested PID namespaces.

The image repository defaults to `ghcr.io/parca-dev/parca-agent`. The debuginfo proxy defaults to
`ghcr.io/dash0hq/debuginfo-proxy` with the chart's `appVersion`. Both `operator.ebpfProfilerImage` and
`operator.debuginfoProxyImage` accept `repository`, `tag`, `digest`, and `pullPolicy`; a digest takes precedence over a tag.

### Node-local delivery

The profiler sends all its OTLP signals to the shared gRPC receiver on port 4317 of the collector DaemonSet instance
on its own node. The collector's existing Service uses `internalTrafficPolicy: Local`. The operator sets
`--metadata-external-labels=parca.agent=true`, which adds the string resource attribute `parca.agent: "true"` to the
profiler's exports. Before Kubernetes enrichment, the collector routes marked metrics, logs, and traces to a
resource-only pod-association pipeline. It associates workload telemetry using attributes such as `container.id` and
`k8s.pod.uid`, never the profiler's connection IP. Profiles already use resource-only pod association. Host/GPU
telemetry without a pod identity remains host-level and uses the default exports. Unmarked application telemetry
retains its existing pod association. No additional receiver or port is required.

The `parca.agent` attribute is reserved for profiler source routing, not authentication. Do not set a conflicting
value on workload resources; the agent preserves existing resource attributes over external-label defaults.

When debuginfo uploads are enabled, the operator also deploys the proxy as a DaemonSet and creates a Service with the
same policy. There is no proxy replica count to tune as the cluster grows.

The Services do not fall back to other nodes when a local endpoint is unavailable. Keep collector, profiler, and proxy
scheduling settings aligned so every profiled node has both local receivers, including any required tolerations.

### Security and namespace ownership

The agent runs as root in a privileged container with `hostPID: true` and read-only host mounts of `/proc` and `/sys`.
Use a Linux kernel supported by the selected agent release. This gives the agent access to processes visible from the
node's PID namespace, not only to Kubernetes namespaces monitored by Dash0.

For nested PID namespaces, such as kind nodes inside Docker Desktop, set
`spec.profiling.ebpfProfiler.pidNamespaceTranslationMode: auto`. The operator passes this as
`--pid-namespace-translation-mode=auto`; an image upgrade alone does not enable translation. When the setting is omitted,
the operator leaves the flag unset and the agent retains its `none` default.

The allowed modes are `none`, `auto`, `exact`, and `recursive`. `exact` collects processes in the agent's PID namespace;
`recursive` also includes descendant PID namespaces and requires kernel BTF. `auto` chooses recursive when supported,
otherwise exact. Workloads in descendant namespaces may therefore be absent when auto falls back to exact.

Create the operator namespace yourself. If your cluster enforces Pod Security Admission, its policy must permit these
privileges. For example, a cluster administrator can apply this label to an existing namespace:

```console
kubectl label namespace dash0-system pod-security.kubernetes.io/enforce=privileged --overwrite
```

The namespace and its labels are user-owned. The chart neither creates a namespace nor changes its security labels,
and installs no namespace-relabeling hooks. Review this policy with your cluster administrator before enabling profiling.

On OpenShift, also set `operator.openShift.enabled: true`. The chart creates a separate profiler SCC and a namespaced
SCC-use binding only when both options are enabled. Only the `<release>-ebpf-profiler` service account receives that
binding. The collector SCC does not gain privileged-container or host-PID permissions.

### Debuginfo uploads

The debuginfo proxy is a trusted-network service, not a public endpoint. The operator manages a NetworkPolicy to restrict
access. Your cluster's CNI must enforce NetworkPolicy; creating the policy alone does not provide isolation on a CNI
that ignores it. Do not expose the proxy through an Ingress, LoadBalancer, or another untrusted network route.

Only Dash0 exports from the operator configuration receive debuginfo uploads. Other OTLP exports still receive profile
samples, but are not debuginfo upload destinations. With zero Dash0 exports, uploads and the proxy are disabled while
profile sampling continues. Set `spec.profiling.ebpfProfiler.debuginfo.uploadEnabled: false` globally on the
`Dash0OperatorConfiguration` to disable uploads and the proxy even when Dash0 exports exist.

The proxy DaemonSet rolls one node at a time with `maxSurge: 0` so old and new configurations do not serve requests on
the same node concurrently. Credential and mode changes invalidate upload routes. Updates can interrupt in-flight
uploads, which the agent can retry. Proxy updates are not zero-downtime updates on the affected node.

### Sampling and workload customization

The default CPU sampling frequency is 19 Hz. `offCPUThreshold` defaults to the fraction `0`,
`probabilisticSampling.interval` to `1m`, `probabilisticSampling.threshold` to the integer `100`, and
`enableErrorFrames` to `false`. Native tracing is always enabled. The default `tracers` list is
`python,hotspot,php,ruby,perl,v8,dotnet,beam,luajit,labels,cuda`, including CUDA GPU tracing. An explicit list replaces
these defaults; `all` is also allowed as an explicit choice. The agent always uses local metadata and OTLP profile
export, with Go symbolization disabled. Its service account does not need Kubernetes API permissions, and its pod
disables service account token automounting.

The operator leaves OTLP logging, GPU metrics, and GPU NVTX at the agent's enabled defaults. The default image is
`ghcr.io/parca-dev/parca-agent`, which includes GPU metrics support, not `parca-agent-static`. GPU collection requires
NVIDIA hardware and the corresponding driver/runtime support. To exclude CUDA tracing, configure a non-empty `tracers`
list without `cuda` or `all`. When `offCPUThreshold` is unset or zero, the operator omits `--off-cpu-threshold` entirely.

Configure these settings under `spec.profiling.ebpfProfiler` on the `Dash0OperatorConfiguration`. Customize resources
and scheduling separately via `spec.components.ebpfProfiler` and `spec.components.debuginfoProxy`, rather than changing
the operator manager's resources or scheduling settings.

### Configuration example

For persistent customization, manage the operator configuration yourself. Do not set `operator.exports`, and leave
`operator.dash0Export.enabled: false`. Helm-managed operator configurations are overwritten from Helm settings on
operator startup, so manual sampling changes to that resource would not persist.

Use these Helm values. The default agent image is `ghcr.io/parca-dev/parca-agent:v0.51.2`:

```yaml
operator:
  ebpfProfilerEnabled: true
  dash0Export:
    enabled: false
  exports: []
  # On OpenShift, also set openShift.enabled: true.
```

Create this operator configuration and provision `dash0-authorization` in the operator namespace. Replace the Dash0
endpoint and dataset with your own values. The generic gRPC export illustrates a destination that receives samples
but never debuginfo; omit it if you only export to Dash0.

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-configuration
spec:
  exports:
    - dash0:
        endpoint: ingress.YOUR_REGION.dash0.com:4317
        dataset: default
        authorization:
          secretRef:
            name: dash0-authorization
            key: token
    - grpc:
        endpoint: another-collector.example.com:4317
  profiling:
    enabled: true
    ebpfProfiler:
      enabled: true
      cpuSamplingFrequency: 19
      offCPUThreshold: 0.1
      tracers: [python, hotspot, php, ruby, perl, v8, dotnet, beam, luajit, labels, cuda]
      probabilisticSampling:
        interval: 1m
        threshold: 25
      enableErrorFrames: false
      mapScaleFactor: 0
      logLevel: info
      debuginfo:
        uploadEnabled: true
        maxConcurrentUploads: 25
        queueSize: 4096
  components:
    ebpfProfiler:
      containerResources:
        requests:
          cpu: 100m
          memory: 128Mi
        limits:
          memory: 512Mi
    debuginfoProxy:
      containerResources:
        requests:
          cpu: 10m
          memory: 32Mi
        limits:
          memory: 128Mi
```

Both components also accept the existing workload settings `labels`, `annotations`, `podLabels`, `podAnnotations`,
`tolerations`, `nodeAffinity`, and `priorityClassName`. The integer `threshold` accepts values from 1 to 100 and passes
directly to the agent's `--profiling-probabilistic-threshold` flag. At each interval, the profiler draws a random integer
from 0 to 99 and samples when it is less than `threshold`. Setting `threshold: 25` enables CPU sampling in 25% of
intervals on average; `threshold: 100` keeps it always active. This setting does not affect off-CPU sampling.
`offCPUThreshold` is a separate fraction from 0 to 1; set it to 0 to disable off-CPU sampling.

Disabling the Helm opt-in sets `DASH0_EBPF_PROFILER_ENABLED=false` explicitly so the operator can remove its managed
profiler resources. Namespace-scoped management permissions remain available for cleanup, including NetworkPolicies.

## Collecting Profiles with the OpenTelemetry eBPF Profiler

Since the standard OpenTelemetry auto-instrumentation agents do not yet emit OTLP profiles, a separate profiling agent is needed to generate profiling data, like the [OpenTelemetry eBPF profiler](https://github.com/open-telemetry/opentelemetry-ebpf-profiler).

The eBPF profiler is distributed as a specialized OpenTelemetry Collector (`otelcol-ebpf-profiler`) that includes a `profiling` receiver.
It runs as a privileged DaemonSet with host PID access, since it relies on eBPF to collect stack traces from all processes on the node.

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

Once both profiling support in the operator and the eBPF profiler DaemonSet are deployed, profiling data will flow from the eBPF profiler through the operator's collector pipelines (including processors like `k8s_attributes` for Kubernetes metadata enrichment) and on to the configured backend.

## Related Documentation

- [Configuration](configuration.md) - Configuration options including filtering and transforming telemetry
- [Installation](installation.md) - Installation guide
