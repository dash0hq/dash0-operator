// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0operator "github.com/dash0hq/dash0-operator/api/operator"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0IntelligentEdge is the schema for the Dash0IntelligentEdge API. It configures the intelligent edge
// feature cluster-wide. This is a singleton resource — only one instance may exist per cluster.
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Enabled",type="boolean",JSONPath=".spec.enabled"
// +kubebuilder:printcolumn:name="Barker",type="boolean",JSONPath=".spec.barker.enabled"
// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=`.status.conditions[?(@.type == "Available")].status`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Dash0IntelligentEdge struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0IntelligentEdgeSpec   `json:"spec,omitempty"`
	Status Dash0IntelligentEdgeStatus `json:"status,omitempty"`
}

// Dash0IntelligentEdgeSpec describes the cluster-wide configuration for the intelligent edge tail-sampling feature.
// When enabled, the operator modifies the collector pipeline to include tail-sampling components (dash0resource,
// dash0operation, dash0sampling processors and dash0redmetrics connector) and optionally deploys the Barker proxy.
type Dash0IntelligentEdgeSpec struct {
	// Whether intelligent edge is enabled. When disabled, no IE components are added to the collector pipeline
	// and no IE resources (e.g. barker) are deployed, regardless of other settings in this resource. This
	// setting is optional, it defaults to true.
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`

	// Configuration for the Barker proxy. When enabled, the operator deploys Barker as a Deployment and Service
	// in the operator namespace. Barker consolidates collector-to-Decision-Maker connections, reducing egress
	// and improving reliability. Recommended for clusters with multiple collector instances.
	//
	// +kubebuilder:validation:Optional
	Barker BarkerConfig `json:"barker,omitempty"`

	// Configuration for the dash0sampling processor in the collector pipeline.
	//
	// +kubebuilder:validation:Optional
	Sampling SamplingConfig `json:"sampling,omitempty"`

	// Configuration for the dash0redmetrics connector. This connector generates RED (Rate, Errors, Duration)
	// metrics from 100% of spans before sampling, ensuring metric accuracy regardless of the sample rate.
	//
	// +kubebuilder:validation:Optional
	RedMetrics RedMetricsConfig `json:"redMetrics,omitempty"`

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

// BarkerConfig configures the Barker Decision Maker proxy.
type BarkerConfig struct {
	// Whether to deploy the Barker proxy. When enabled, collectors connect to Barker instead of directly
	// to the cloud Decision Maker. This consolidates N collector connections into 1-2 upstream connections.
	// This setting is optional, it defaults to true.
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`

	// The log level for the Barker proxy. This setting is optional, it defaults to "info".
	//
	// +kubebuilder:default=info
	// +kubebuilder:validation:Enum=trace;debug;info;warn;error
	LogLevel BarkerLogLevel `json:"logLevel,omitempty"`

	// Enable verbose network logging in Barker. This is separate from the log level and produces
	// detailed output about gRPC connections and message flow. Warning: high log volume. This setting
	// is optional, it defaults to false.
	//
	// +kubebuilder:validation:Optional
	Debug *bool `json:"debug,omitempty"`
}

// BarkerLogLevel describes the log level for the Barker proxy.
//
// +kubebuilder:validation:Enum=trace;debug;info;warn;error
type BarkerLogLevel string

const (
	BarkerLogLevelTrace BarkerLogLevel = "trace"
	BarkerLogLevelDebug BarkerLogLevel = "debug"
	BarkerLogLevelInfo  BarkerLogLevel = "info"
	BarkerLogLevelWarn  BarkerLogLevel = "warn"
	BarkerLogLevelError BarkerLogLevel = "error"
)

// SamplingConfig configures the dash0sampling processor.
type SamplingConfig struct {
	// Whether tail-sampling is enabled. When disabled, the sampling processor and its Decision Maker
	// connection are not added to the collector pipeline. RED metrics, dash0resource, and dash0operation
	// processors remain active if the overall IE flag (spec.enabled) is true. This setting is optional,
	// it defaults to true.
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`

	// The Decision Maker endpoint used by the sampling processor. This setting is optional. When not set,
	// the endpoint is derived from the Dash0 ingress endpoint configured in the operator configuration
	// resource by replacing "ingress." with "decision-maker." and ":4317" with ":443". When Barker is
	// enabled, this field is ignored and the sampling processor connects to the in-cluster Barker service
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
	// have at most 3 unique values to avoid high cardinality. This setting is optional, it defaults to
	// an empty list.
	//
	// +kubebuilder:validation:Optional
	AdditionalSpanAttributes []string `json:"additionalSpanAttributes,omitempty"`
}

// OperationProcessorConfig configures the dash0operation processor.
type OperationProcessorConfig struct {
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

// Dash0IntelligentEdgeStatus defines the observed state of the Dash0IntelligentEdge resource.
type Dash0IntelligentEdgeStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

func (d *Dash0IntelligentEdge) IsMarkedForDeletion() bool {
	deletionTimestamp := d.GetDeletionTimestamp()
	return deletionTimestamp != nil && !deletionTimestamp.IsZero()
}

func (d *Dash0IntelligentEdge) IsAvailable() bool {
	if condition := d.getCondition(dash0common.ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0IntelligentEdge) IsDegraded() bool {
	if condition := d.getCondition(dash0common.ConditionTypeDegraded); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0IntelligentEdge) getCondition(conditionType dash0common.ConditionType) *metav1.Condition {
	for _, c := range d.Status.Conditions {
		if c.Type == string(conditionType) {
			return &c
		}
	}
	return nil
}

func (d *Dash0IntelligentEdge) SetAvailableConditionToUnknown() {
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Dash0 has started intelligent edge resource reconciliation.",
		},
	)
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileStarted",
			Message: "Dash0 intelligent edge resource reconciliation is in progress.",
		},
	)
}

func (d *Dash0IntelligentEdge) EnsureResourceIsMarkedAsAvailable() {
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: "Dash0 intelligent edge is available in this cluster now.",
		},
	)
	meta.RemoveStatusCondition(&d.Status.Conditions, string(dash0common.ConditionTypeDegraded))
}

func (d *Dash0IntelligentEdge) EnsureResourceIsMarkedAsAboutToBeDeleted() {
	d.EnsureResourceIsMarkedAsDegraded(
		"Dash0IntelligentEdgeResourceHasBeenRemoved",
		"Dash0 intelligent edge is inactive in this cluster now.",
	)
}

func (d *Dash0IntelligentEdge) EnsureResourceIsMarkedAsDegraded(
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

func (d *Dash0IntelligentEdge) GetNaturalLanguageResourceTypeName() string {
	return "Dash0 intelligent edge resource"
}

func (d *Dash0IntelligentEdge) Get() client.Object {
	return d
}

func (d *Dash0IntelligentEdge) GetName() string {
	return d.Name
}

func (d *Dash0IntelligentEdge) GetUID() types.UID {
	return d.UID
}

func (d *Dash0IntelligentEdge) GetCreationTimestamp() metav1.Time {
	return d.CreationTimestamp
}

func (d *Dash0IntelligentEdge) GetReceiver() client.Object {
	return &Dash0IntelligentEdge{}
}

func (d *Dash0IntelligentEdge) GetListReceiver() client.ObjectList {
	return &Dash0IntelligentEdgeList{}
}

func (d *Dash0IntelligentEdge) IsClusterResource() bool {
	return true
}

func (d *Dash0IntelligentEdge) RequestToName(_ ctrl.Request) string {
	return d.Name
}

func (d *Dash0IntelligentEdge) All(list client.ObjectList) []dash0operator.Dash0Resource {
	items := list.(*Dash0IntelligentEdgeList).Items
	result := make([]dash0operator.Dash0Resource, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}

func (d *Dash0IntelligentEdge) Items(list client.ObjectList) []client.Object {
	items := list.(*Dash0IntelligentEdgeList).Items
	result := make([]client.Object, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}

func (d *Dash0IntelligentEdge) At(list client.ObjectList, index int) dash0operator.Dash0Resource {
	return &list.(*Dash0IntelligentEdgeList).Items[index]
}

//+kubebuilder:object:root=true

// Dash0IntelligentEdgeList contains a list of Dash0IntelligentEdge resources.
type Dash0IntelligentEdgeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0IntelligentEdge `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dash0IntelligentEdge{}, &Dash0IntelligentEdgeList{})
}
