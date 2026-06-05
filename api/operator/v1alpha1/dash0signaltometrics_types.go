// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0SignalToMetrics is the Schema for the dash0signaltometrics API.
// It defines a single rule that derives a custom metric from spans or log records.
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
type Dash0SignalToMetrics struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0SignalToMetricsSpec   `json:"spec,omitempty"`
	Status Dash0SignalToMetricsStatus `json:"status,omitempty"`
}

// Dash0SignalToMetricsSpec defines the desired state of a SignalToMetrics rule.
type Dash0SignalToMetricsSpec struct {
	// Whether this rule is active.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// Display configuration for the rule (e.g. the human-readable name shown in the Dash0 UI).
	// +kubebuilder:validation:Required
	Display Dash0SignalToMetricsDisplay `json:"display"`

	// Match selects which signals this rule applies to.
	// +kubebuilder:validation:Required
	Match Dash0SignalToMetricsMatch `json:"match"`

	// Output controls the metric that is produced for matching signals.
	// +kubebuilder:validation:Required
	Output Dash0SignalToMetricsOutput `json:"output"`
}

// Dash0SignalToMetricsDisplay defines display configuration.
type Dash0SignalToMetricsDisplay struct {
	// Human-readable name for this rule.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// Dash0SignalToMetricsMatch selects which signals are converted to metrics.
type Dash0SignalToMetricsMatch struct {
	// The signal type this rule applies to. Spans produce exponential histograms; log records produce count metrics.
	// +kubebuilder:validation:Required
	Signal Dash0SignalToMetricsSignalType `json:"signal"`

	// Filters that determine which spans / log records contribute to the output metric.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Filters []Dash0SignalToMetricsAttributeFilter `json:"filters"`
}

// Dash0SignalToMetricsSignalType is the signal type a rule applies to.
// +kubebuilder:validation:Enum=spans;logs
type Dash0SignalToMetricsSignalType string

const (
	Dash0SignalToMetricsSignalTypeSpans Dash0SignalToMetricsSignalType = "spans"
	Dash0SignalToMetricsSignalTypeLogs  Dash0SignalToMetricsSignalType = "logs"
)

// Dash0SignalToMetricsAttributeFilter is a single filter against an attribute key.
type Dash0SignalToMetricsAttributeFilter struct {
	// The attribute key to match against.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`

	// The match operator.
	// +kubebuilder:validation:Required
	Operator Dash0SignalToMetricsFilterOperator `json:"operator"`

	// Single value for operators that accept one value (is, is_not, contains, matches, ...).
	// +kubebuilder:validation:Optional
	Value *string `json:"value,omitempty"`

	// List of values for the is_one_of / is_not_one_of operators.
	// +kubebuilder:validation:Optional
	Values []string `json:"values,omitempty"`
}

// Dash0SignalToMetricsMatcher is a key-less variant of an attribute filter used for keepResourceAttributes /
// keepSignalAttributes selection: it matches against attribute keys.
type Dash0SignalToMetricsMatcher struct {
	// The match operator.
	// +kubebuilder:validation:Required
	Operator Dash0SignalToMetricsFilterOperator `json:"operator"`

	// Single value for operators that accept one value.
	// +kubebuilder:validation:Optional
	Value *string `json:"value,omitempty"`

	// List of values for the is_one_of / is_not_one_of operators.
	// +kubebuilder:validation:Optional
	Values []string `json:"values,omitempty"`
}

// Dash0SignalToMetricsFilterOperator enumerates the supported filter / matcher operators.
// The set mirrors the AttributeFilterOperator enum in the dash0 backend's openapi-types/filtering.yml.
// +kubebuilder:validation:Enum=is;is_not;is_set;is_not_set;is_one_of;is_not_one_of;gt;lt;gte;lte;matches;does_not_match;contains;does_not_contain;starts_with;does_not_start_with;ends_with;does_not_end_with;is_any
type Dash0SignalToMetricsFilterOperator string

// Dash0SignalToMetricsOutput controls the resulting metric.
type Dash0SignalToMetricsOutput struct {
	// The name of the resulting metric.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Optional description used as metadata on the generated metric.
	// +kubebuilder:validation:Optional
	Description *string `json:"description,omitempty"`

	// Interval at which a data point is emitted for matched signals. Backend bounds: 5s..10m.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(\d+(ms|s|m|h|d|w|M|Q|y))+$`
	Interval string `json:"interval"`

	// Key matchers that select resource attributes to carry over to the output metric.
	// +kubebuilder:validation:Optional
	KeepResourceAttributes []Dash0SignalToMetricsMatcher `json:"keepResourceAttributes,omitempty"`

	// Key matchers that select span/log record attributes to carry over as metric dimensions.
	// +kubebuilder:validation:Optional
	KeepSignalAttributes []Dash0SignalToMetricsMatcher `json:"keepSignalAttributes,omitempty"`
}

// Dash0SignalToMetricsStatus defines the observed state of a Dash0SignalToMetrics resource.
type Dash0SignalToMetricsStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus                `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                                      `json:"synchronizedAt"`
	ValidationIssues       []string                                                         `json:"validationIssues,omitempty"`
	SynchronizationResults []Dash0SignalToMetricsSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults"`
}

// Dash0SignalToMetricsSynchronizationResultPerEndpointAndDataset captures the result of a single sync attempt.
type Dash0SignalToMetricsSynchronizationResultPerEndpointAndDataset struct {
	SynchronizationStatus dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	Dash0ApiEndpoint      string                                            `json:"dash0ApiEndpoint,omitempty"`
	Dash0Dataset          string                                            `json:"dash0Dataset,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Id string `json:"dash0Id,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin          string `json:"dash0Origin,omitempty"`
	SynchronizationError string `json:"synchronizationError,omitempty"`
	// HttpStatusCode is the HTTP status code that the Dash0 API returned for the failed synchronization attempt, if the
	// failure was caused by an unexpected HTTP response. It is 0 (absent) for successful synchronizations and for
	// transport-level errors (network errors, timeouts) where no HTTP response was received. It is used to decide
	// whether a failed synchronization should be retried.
	// +kubebuilder:validation:Optional
	HttpStatusCode int `json:"httpStatusCode,omitempty"`
}

// Dash0SignalToMetricsList contains a list of Dash0SignalToMetrics resources.
//
// +kubebuilder:object:root=true
type Dash0SignalToMetricsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0SignalToMetrics `json:"items"`
}
