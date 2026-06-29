# Profiling

This document covers profiling support in the Dash0 operator.

## Table of Contents

- [Enabling Profiling Support](#enabling-profiling-support)
- [Collecting Profiles with the OpenTelemetry eBPF Profiler](#collecting-profiles-with-the-opentelemetry-ebpf-profiler)

## Enabling Profiling Support

The Dash0 operator can be configured to accept, process, and export [profiling data](https://opentelemetry.io/docs/specs/otel/profiles/) via OTLP.

**Note:** The Dash0 Operator does not currently support collecting profiles, see the [Collecting Profiles with the OpenTelemetry eBPF Profiler](#collecting-profiles-with-the-opentelemetry-ebpf-profiler) section.

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
