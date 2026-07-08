// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0operator "github.com/dash0hq/dash0-operator/api/operator"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0SignalControl is the schema for the Dash0SignalControl API. It configures the Signal Control
// feature cluster-wide. This is a singleton resource — only one instance may exist per cluster.
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Enabled",type="boolean",JSONPath=".spec.enabled"
// +kubebuilder:printcolumn:name="EdgeProxy",type="boolean",JSONPath=".spec.edgeProxy.enabled"
// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=`.status.conditions[?(@.type == "Available")].status`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Dash0SignalControl struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0SignalControlSpec   `json:"spec,omitempty"`
	Status Dash0SignalControlStatus `json:"status,omitempty"`
}

// Dash0SignalControlSpec describes the cluster-wide configuration for the Signal Control tail-sampling feature.
// When enabled, the operator modifies the collector pipeline to include tail-sampling components (dash0resource,
// dash0operation, dash0filter, dash0sampling processors, dash0redmetrics and dash0signaltometrics connectors)
// and optionally deploys the Edge Proxy.
type Dash0SignalControlSpec struct {
	// Whether Signal Control is enabled. When disabled, no Signal Control components are added to the collector pipeline
	// and no Signal Control resources (e.g. Edge Proxy) are deployed, regardless of other settings in this resource. This
	// setting is optional, it defaults to true.
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`

	// Configuration for the Edge Proxy. When enabled, the operator deploys the Edge Proxy as a Deployment and
	// Service in the operator namespace. The Edge Proxy consolidates collector-to-Decision-Maker connections,
	// reducing egress and improving reliability. Recommended for clusters with multiple collector instances.
	//
	// +kubebuilder:validation:Optional
	EdgeProxy EdgeProxyConfig `json:"edgeProxy,omitempty"`

	// Configuration for the dash0sampling processor in the collector pipeline.
	//
	// +kubebuilder:validation:Optional
	Sampling SamplingConfig `json:"sampling,omitempty"`

	// Configuration for the dash0redmetrics connector. This connector generates RED (Rate, Errors, Duration)
	// metrics from 100% of spans before sampling, ensuring metric accuracy regardless of the sample rate.
	//
	// +kubebuilder:validation:Optional
	RedMetrics RedMetricsConfig `json:"redMetrics,omitempty"`

	// Configuration for the dash0signaltometrics connector in the collector pipeline. The connector derives
	// custom metrics from spans and log records at the edge based on Dash0SignalToMetrics rules synced to
	// the Dash0 control plane.
	//
	// +kubebuilder:validation:Optional
	SignalToMetrics SignalToMetricsConfig `json:"signalToMetrics,omitempty"`

	// Configuration for the dash0filter processor in the collector pipeline. The processor drops spans,
	// metric data points, and log records that match Dash0SpamFilter rules synced to the Dash0 control
	// plane, allowing high-volume or low-value telemetry to be filtered out at the edge before it leaves
	// the cluster.
	//
	// +kubebuilder:validation:Optional
	SpamFilter SpamFilterConfig `json:"spamFilter,omitempty"`

	// Configuration for the dash0operation processor. This processor derives dash0.operation.* and dash0.span.*
	// attributes from OpenTelemetry semantic conventions.
	//
	// +kubebuilder:validation:Optional
	OperationProcessor OperationProcessorConfig `json:"operationProcessor,omitempty"`

	// The control plane API endpoint used by the dash0settingsonedge extension to fetch settings and rules.
	// This setting is optional. When not set, the endpoint is derived from the Dash0 API endpoint configured
	// in the operator configuration resource by stripping regional segments and replacing "api." with
	// "control-plane-api." (e.g., https://api.eu-central-1.aws.dash0.com -> https://control-plane-api.dash0.com).
	//
	// +kubebuilder:validation:Optional
	ControlPlaneApiEndpoint string `json:"controlPlaneApiEndpoint,omitempty"`
}

// EdgeProxyConfig configures the Edge Proxy (Decision Maker proxy).
type EdgeProxyConfig struct {
	// Whether to deploy the Edge Proxy. When enabled, collectors connect to the Edge Proxy instead of directly
	// to the cloud Decision Maker. This consolidates N collector connections into 1-2 upstream connections.
	// This setting is optional, it defaults to true.
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`

	// The log level for the Edge Proxy. This setting is optional, it defaults to "info".
	//
	// +kubebuilder:default=info
	// +kubebuilder:validation:Enum=trace;debug;info;warn;error
	LogLevel EdgeProxyLogLevel `json:"logLevel,omitempty"`

	// Enable verbose network logging in the Edge Proxy. This is separate from the log level and produces
	// detailed output about gRPC connections and message flow. Warning: high log volume. This setting
	// is optional, it defaults to false.
	//
	// +kubebuilder:validation:Optional
	Debug *bool `json:"debug,omitempty"`

	// Disables TLS for the Edge Proxy's upstream gRPC connection to the Decision Maker. Intended for local
	// development and end-to-end testing against in-cluster mock services. Production deployments
	// should leave this unset so that the Edge Proxy requires TLS upstream. This setting is optional, it
	// defaults to false.
	//
	// +kubebuilder:validation:Optional
	Insecure *bool `json:"insecure,omitempty"`
}

// EdgeProxyLogLevel describes the log level for the Edge Proxy.
//
// +kubebuilder:validation:Enum=trace;debug;info;warn;error
type EdgeProxyLogLevel string

const (
	EdgeProxyLogLevelTrace EdgeProxyLogLevel = "trace"
	EdgeProxyLogLevelDebug EdgeProxyLogLevel = "debug"
	EdgeProxyLogLevelInfo  EdgeProxyLogLevel = "info"
	EdgeProxyLogLevelWarn  EdgeProxyLogLevel = "warn"
	EdgeProxyLogLevelError EdgeProxyLogLevel = "error"
)

// SamplingConfig configures the dash0sampling processor.
type SamplingConfig struct {
	// Whether tail-sampling is enabled. When disabled, the sampling processor and its Decision Maker
	// connection are not added to the collector pipeline. RED metrics, dash0resource, and dash0operation
	// processors remain active if the overall Signal Control flag (spec.enabled) is true. This setting is optional,
	// it defaults to true.
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`

	// The Decision Maker endpoint used by the sampling processor. This setting is optional. When not set,
	// the endpoint is derived from the Dash0 ingress endpoint configured in the operator configuration
	// resource by replacing "ingress." with "decision-maker." and ":4317" with ":443". When the Edge Proxy is
	// enabled, this field is ignored and the sampling processor connects to the in-cluster Edge Proxy service
	// instead.
	//
	// +kubebuilder:validation:Optional
	DecisionMakerEndpoint string `json:"decisionMakerEndpoint,omitempty"`

	// The probability ratio for fallback sampling when the Decision Maker is unreachable. A value of
	// "0.01" keeps 1% of spans. Set to "0.0" to block all spans until the Decision Maker is available,
	// or "1.0" to keep all spans. This setting is optional, it defaults to "0.01". Must be a string
	// representation of a float between 0.0 and 1.0.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^(0(\.\d+)?|1(\.0+)?)$`
	FallbackSampleRatio *string `json:"fallbackSampleRatio,omitempty"`

	// Enable verbose logging of sampling decisions. Warning: this produces high log volume. This setting
	// is optional, it defaults to false.
	//
	// +kubebuilder:validation:Optional
	Debug *bool `json:"debug,omitempty"`

	// Whether satisfaction reports to the Decision Maker are batched instead of sent immediately. Batching
	// groups multiple reports into fewer RPC calls, reducing overhead in high-throughput scenarios at the
	// cost of slightly higher sampling-decision latency. This setting is optional, it defaults to false.
	//
	// +kubebuilder:validation:Optional
	EnableBatching *bool `json:"enableBatching,omitempty"`

	// Configuration for the trace reservoir, the buffer that holds spans awaiting a sampling decision.
	// This setting is optional.
	//
	// +kubebuilder:validation:Optional
	Reservoir *ReservoirConfig `json:"reservoir,omitempty"`
}

// ReservoirMetricLevel controls the verbosity of the trace reservoir's telemetry.
// +kubebuilder:validation:Enum=basic;detailed
type ReservoirMetricLevel string

const (
	ReservoirMetricLevelBasic    ReservoirMetricLevel = "basic"
	ReservoirMetricLevelDetailed ReservoirMetricLevel = "detailed"
)

// ReservoirType selects the storage backend of the trace reservoir.
// +kubebuilder:validation:Enum=disk;memory;serialized_memory
type ReservoirType string

const (
	// ReservoirTypeDisk buffers spans in sharded files backed by an ephemeral volume; uses maxDiskBytes.
	ReservoirTypeDisk ReservoirType = "disk"
	// ReservoirTypeMemory buffers spans as in-memory traces; uses maxMemoryBytes.
	ReservoirTypeMemory ReservoirType = "memory"
	// ReservoirTypeSerializedMemory buffers spans as in-memory serialized bytes (lower GC pressure); uses
	// maxMemoryBytes. Recommended for high-throughput scenarios.
	ReservoirTypeSerializedMemory ReservoirType = "serialized_memory"
)

// ReservoirConfig configures the trace reservoir of the dash0sampling processor, the buffer that holds
// spans awaiting a sampling decision.
type ReservoirConfig struct {
	// The storage backend of the trace reservoir. "serialized_memory" and "memory" buffer spans in the
	// collector pod's memory (sized via maxMemoryBytes); "disk" buffers them on an ephemeral volume (sized
	// via maxDiskBytes). "serialized_memory" is recommended for high-throughput scenarios. This setting is
	// optional, it defaults to "serialized_memory".
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=serialized_memory
	Type *ReservoirType `json:"type,omitempty"`

	// The maximum total disk usage of the trace reservoir across all shards. Only used when type is "disk".
	// When the reservoir exceeds this limit, the oldest buffered spans are evicted regardless of age. The
	// operator also derives the reservoir volume's storage size limit and the collector container's
	// ephemeral-storage request from this value, so that the requested storage always exceeds the
	// reservoir's own cap. This setting is optional, it defaults to "1Gi". Values below "64Mi" are raised to
	// "64Mi".
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="1Gi"
	MaxDiskBytes *resource.Quantity `json:"maxDiskBytes,omitempty"`

	// The maximum memory used by the trace reservoir. Only used when type is "memory" or
	// "serialized_memory". When the reservoir exceeds this limit, the oldest buffered spans are evicted
	// regardless of age. This setting is optional; when unset the processor default (100Mi) applies.
	//
	// +kubebuilder:validation:Optional
	MaxMemoryBytes *resource.Quantity `json:"maxMemoryBytes,omitempty"`

	// The verbosity of the trace reservoir's telemetry. "basic" records only essential metrics and is
	// recommended for production. "detailed" records all metrics including histograms and hot-path counters,
	// useful for load testing or debugging. This setting is optional, it defaults to "basic".
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=basic
	MetricLevel *ReservoirMetricLevel `json:"metricLevel,omitempty"`
}

// RedMetricsConfig configures the dash0redmetrics connector.
type RedMetricsConfig struct {
	// Soft limit for the number of time series held in memory. This setting is optional, it defaults
	// to 5000.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	MaxTimeSeries *int32 `json:"maxTimeSeries,omitempty"`

	// Additional low-cardinality span attributes to include as metric dimensions. Each attribute should
	// have at most 3 unique values to avoid high cardinality. Attribute names must be non-empty and
	// unique. This setting is optional, it defaults to an empty list.
	//
	// +kubebuilder:validation:Optional
	// +listType=set
	// +kubebuilder:validation:items:MinLength=1
	AdditionalSpanAttributes []string `json:"additionalSpanAttributes,omitempty"`
}

// SignalToMetricsConfig configures the dash0signaltometrics connector. The connector derives
// custom metrics from spans and log records at the edge based on Dash0SignalToMetrics rules synced to
// the Dash0 control plane.
type SignalToMetricsConfig struct {
	// Whether to wire the dash0signaltometrics connector into the daemonset collector pipeline. When
	// disabled, Dash0SignalToMetrics rules synced to the Dash0 control plane are not evaluated at the
	// edge. This setting is optional, it defaults to true.
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`

	// Soft cap on the number of time series held in memory by the connector. When exceeded, the
	// connector starts expiring idle time series progressively until the count is within the limit.
	// This setting is optional; the connector default (30000) applies when unset.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	MaxTimeSeries *int32 `json:"maxTimeSeries,omitempty"`

	// How often accumulated metrics are flushed to the metrics pipeline. Go duration syntax (e.g.
	// "60s", "5m"). This setting is optional; the connector default (60s) applies when unset.
	//
	// +kubebuilder:validation:Optional
	FlushInterval *metav1.Duration `json:"flushInterval,omitempty"`

	// How long compiled rulesets are cached before the connector re-fetches them from the
	// dash0settingsonedgeextension. Go duration syntax (e.g. "30s", "5m"). Must be between 10s and 1h; a
	// non-zero value outside this range is rejected at admission. This setting is optional; the connector
	// default (60s) applies when unset.
	//
	// +kubebuilder:validation:Optional
	CacheExpiration *metav1.Duration `json:"cacheExpiration,omitempty"`
}

// SpamFilterConfig configures the dash0filter processor. The processor drops spans, metric data points,
// and log records that match Dash0SpamFilter rules synced to the Dash0 control plane.
type SpamFilterConfig struct {
	// Whether to wire the dash0filter processor into the collector pipelines. When disabled,
	// Dash0SpamFilter rules synced to the Dash0 control plane are not evaluated at the edge and no
	// telemetry is dropped by the spam filter. This setting is optional, it defaults to true.
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`

	// How long compiled filter rules are cached before the processor re-fetches them from the
	// dash0settingsonedgeextension. Go duration syntax (e.g. "30s", "5m"). Must be between 10s and 1h; a
	// non-zero value outside this range is rejected at admission. This setting is optional; the processor
	// default (60s) applies when unset.
	//
	// +kubebuilder:validation:Optional
	CacheExpiration *metav1.Duration `json:"cacheExpiration,omitempty"`

	// Allow the dash0filter processor to start even when the dash0settingsonedgeextension is not
	// loaded. Intended for testing and development; in production this should remain unset so the
	// processor fails fast on a misconfigured pipeline. This setting is optional, it defaults to false.
	//
	// +kubebuilder:validation:Optional
	AllowNoSettingsExt *bool `json:"allowNoSettingsExt,omitempty"`
}

// OperationProcessorConfig configures the dash0operation processor.
type OperationProcessorConfig struct {
	// Controls whether the OpenTelemetry span name is used as both dash0.operation.name and
	// dash0.span.name instead of the rule-derived pattern name. Rules are still evaluated to determine
	// the operation type. This setting is optional, it defaults to false.
	//
	// +kubebuilder:validation:Optional
	PreferSpanName *bool `json:"preferSpanName,omitempty"`

	// Custom rules for normalizing high-cardinality operation names. Rules are evaluated in order, first
	// match wins. This setting is optional, it defaults to an empty list.
	//
	// +kubebuilder:validation:Optional
	CardinalityRules []CardinalityRule `json:"cardinalityRules,omitempty"`
}

// CardinalityRule defines a rule for normalizing high-cardinality attribute values.
type CardinalityRule struct {
	// Unique identifier for this rule.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Id string `json:"id"`

	// The attribute to apply this rule to. If empty, the rule applies to all URL-related attributes.
	//
	// +kubebuilder:validation:Optional
	SourceAttribute string `json:"sourceAttribute,omitempty"`

	// A substring pre-filter for performance. Only values containing this substring are evaluated
	// against the matchers.
	//
	// +kubebuilder:validation:Optional
	QuickFilter string `json:"quickFilter,omitempty"`

	// Matchers to apply to attribute values. Evaluated in order, first match wins.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	OperationMatchers []OperationMatcher `json:"operationMatchers"`
}

// OperationMatcher defines a regex-based matcher for normalizing operation names.
type OperationMatcher struct {
	// Regular expression with capture groups for matching attribute values.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	Regex string `json:"regex"`

	// Replacement values for the capture groups.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Replacements []string `json:"replacements"`

	// A substring pre-filter for this specific matcher.
	//
	// +kubebuilder:validation:Optional
	QuickFilter string `json:"quickFilter,omitempty"`

	// If true, replacement values are used as-is without {}-wrapping.
	//
	// +kubebuilder:validation:Optional
	Literal *bool `json:"literal,omitempty"`
}

// Dash0SignalControlStatus defines the observed state of the Dash0SignalControl resource.
type Dash0SignalControlStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

func (d *Dash0SignalControl) IsMarkedForDeletion() bool {
	deletionTimestamp := d.GetDeletionTimestamp()
	return deletionTimestamp != nil && !deletionTimestamp.IsZero()
}

func (d *Dash0SignalControl) IsAvailable() bool {
	if condition := d.getCondition(dash0common.ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0SignalControl) IsDegraded() bool {
	if condition := d.getCondition(dash0common.ConditionTypeDegraded); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0SignalControl) getCondition(conditionType dash0common.ConditionType) *metav1.Condition {
	for _, c := range d.Status.Conditions {
		if c.Type == string(conditionType) {
			return &c
		}
	}
	return nil
}

func (d *Dash0SignalControl) SetAvailableConditionToUnknown() {
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Dash0 has started Signal Control resource reconciliation.",
		},
	)
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileStarted",
			Message: "Dash0 Signal Control resource reconciliation is in progress.",
		},
	)
}

func (d *Dash0SignalControl) EnsureResourceIsMarkedAsAvailable() {
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: "Dash0 Signal Control is available in this cluster now.",
		},
	)
	meta.RemoveStatusCondition(&d.Status.Conditions, string(dash0common.ConditionTypeDegraded))
}

func (d *Dash0SignalControl) EnsureResourceIsMarkedAsAboutToBeDeleted() {
	d.EnsureResourceIsMarkedAsDegraded(
		"Dash0SignalControlResourceHasBeenRemoved",
		"Dash0 Signal Control is inactive in this cluster now.",
	)
}

func (d *Dash0SignalControl) EnsureResourceIsMarkedAsDegraded(
	reason string,
	message string,
) {
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
	)
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
	)
}

func (d *Dash0SignalControl) GetNaturalLanguageResourceTypeName() string {
	return "Dash0 Signal Control resource"
}

func (d *Dash0SignalControl) Get() client.Object {
	return d
}

func (d *Dash0SignalControl) GetName() string {
	return d.Name
}

func (d *Dash0SignalControl) GetUID() types.UID {
	return d.UID
}

func (d *Dash0SignalControl) GetCreationTimestamp() metav1.Time {
	return d.CreationTimestamp
}

func (d *Dash0SignalControl) GetReceiver() client.Object {
	return &Dash0SignalControl{}
}

func (d *Dash0SignalControl) GetListReceiver() client.ObjectList {
	return &Dash0SignalControlList{}
}

func (d *Dash0SignalControl) IsClusterResource() bool {
	return true
}

func (d *Dash0SignalControl) RequestToName(_ ctrl.Request) string {
	return d.Name
}

func (d *Dash0SignalControl) All(list client.ObjectList) []dash0operator.Dash0Resource {
	items := list.(*Dash0SignalControlList).Items
	result := make([]dash0operator.Dash0Resource, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}

func (d *Dash0SignalControl) Items(list client.ObjectList) []client.Object {
	items := list.(*Dash0SignalControlList).Items
	result := make([]client.Object, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}

func (d *Dash0SignalControl) At(list client.ObjectList, index int) dash0operator.Dash0Resource {
	return &list.(*Dash0SignalControlList).Items[index]
}

//+kubebuilder:object:root=true

// Dash0SignalControlList contains a list of Dash0SignalControl resources.
type Dash0SignalControlList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0SignalControl `json:"items"`
}
