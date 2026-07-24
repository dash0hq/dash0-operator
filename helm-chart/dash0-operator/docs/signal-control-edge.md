# Dash0 SignalControl Edge

SignalControl Edge (SCE) processes observability data inside your own Kubernetes cluster to cut egress cost. In order to
achieve that, the operator wires a set of custom Dash0 components into the OpenTelemetry collector pipelines:
- tail-sampling drops ordinary/repetitive traces while keeping the ones that matter e.g., errors, fixed percentage, or
based on custom criteria via OTTL
- RED (Rate, Errors, Duration) metrics are generated from spans before sampling so your dashboards stay accurate
- signal-to-metrics derives metrics from logs and traces - like RED metrics, this happens before sampling to keep
metrics accurate
- spam filters that are evaluated in-cluster to reduce egress costs
- the metric recorder emits operational counters from in-cluster components (currently the volume dropped by the spam
filter) so you keep visibility into filtered data without shipping the raw signals
- the operation processor derives useful attributes and normalizes high-cardinality attributes

Tail-sampling decisions that require cross-collector coordination are made by the Dash0 Decision Maker (SaaS-side).
Sampling rules, spam filters, and signal-to-metrics rules are configured as Kubernetes custom resources and synced to the
Dash0 backend.

## Note about availability

SignalControl Edge is not generally available and must be enabled for your organization on the Dash0 side. The operator verifies this entitlement
against the Dash0 API; if the organization is not entitled, the `Dash0SignalControl` resource is marked degraded, SignalControl Edge is
not applied and the standard collector is used, even when the Helm flag and the `Dash0SignalControl` resource are set.

## Quickstart

Enabling SignalControl Edge takes two steps: turning the feature flag on in the Helm chart, and creating a
`Dash0SignalControl` resource to configure it.

The Helm flag controls which collector image will be used (the one with SCE components or without them) and whether the
`Dash0SignalControl` and `Dash0SamplingRule` CRDs will be installed in the cluster. It is set to `false` by default to
ensure that without explicitly enabling it, there are zero changes to clusters of customers not using SCE.

The `Dash0SignalControl` resource configures the individual SCE components. Please refer to
[dash0signalcontrol_types.go](https://github.com/dash0hq/dash0-operator/blob/main/api/operator/v1alpha1/dash0signalcontrol_types.go)
for all available configuration
fields and their defaults.

SignalControl Edge builds on top of the standard operator setup, so if you are new to the Dash0 operator, please read the
[Helm chart README](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md) first.

SignalControl Edge only acts on telemetry destined for Dash0, so the operator must have a default Dash0 exporter
configured; without one, SCE has no effect. A few export constraints apply in the current beta: multi-organization
exporters are not supported, and tail-sampling is applied only to the first Dash0 exporter/dataset of each namespace —
configuring multiple Dash0 exporters/datasets within a single namespace is not supported for sampling.

Because tail-sampling makes a single keep-or-drop decision for an entire trace, we recommend to ensure all spans of a given
trace are routed to the same dataset. When the spans of a trace are split across multiple namespaces and sampling is only
applied to some of them, it might result in orphaned spans in Dash0.

To enable SignalControl Edge, the only additional required Helm setting is `operator.signalControl.enabled` which can be
either supplied in the values file or via `--set operator.signalControl.enabled=true`. Because the SCE components add
extra processing and buffering — tail-sampling in particular buffers traces in memory while decisions are pending —
plan to increase the collector's memory when enabling SignalControl Edge; at least 500Mi of additional memory is a
reasonable starting point.

Once the helm install or upgrade has completed, the next step is to create a `Dash0SignalControl` resource.
Note that this resource is a singleton: only one instance may exist per cluster.

Since every component defaults to enabled, a minimal resource with an empty spec turns on the full SCE pipeline,
including the Edge Proxy (recommended whenever you run more than one collector instance):

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SignalControl
metadata:
  name: dash0-signal-control
spec: {}
```

After creating this resource, you should now see a new component (edge proxy) in the operator namespace and if you inspect
the collector config, you will see the inserted SCE components.

As a test, you can define a cluster-scoped `Dash0SamplingRule` resource. The probabilistic rule below keeps 10% of all
traces and is evaluated locally in the collector, without involving the Decision Maker:

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SamplingRule
metadata:
  name: sample-10-percent
spec:
  enabled: true
  display:
    name: Sample 10% of all traces
  conditions:
    kind: probabilistic
    spec:
      rate: "0.1"
```

## How it works

SignalControl Edge does not replace the collector's normal processing — it runs after it. Telemetry first passes
through the standard collector processors (resource detection, Kubernetes attributes, batching, and so on), and only
then is handed to the SignalControl Edge components, right before it is exported to Dash0. The regular collection behavior
stays unchanged; the egress-reduction logic is layered on top.

The rules that drive these components (sampling rules, spam filters, signal-to-metrics rules) are authored as Kubernetes
custom resources. The operator reconciles each resource and syncs it to the Dash0 backend via the Dash0 API. Independently, the
collector pulls the effective, merged settings back down at runtime and feeds them to the SignalControl Edge components, so
rule changes take effect without redeploying the collector.

Most rules are evaluated locally in the collector. Tail-sampling decisions that require coordination across collector
instances are instead delegated to the Dash0 Decision Maker on the SaaS side; when the Edge Proxy is enabled, collectors
reach the Decision Maker through it rather than connecting individually.
