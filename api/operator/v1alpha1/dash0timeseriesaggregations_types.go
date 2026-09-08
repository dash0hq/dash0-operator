// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0TimeSeriesAggregation is the Schema for the dash0timeseriesaggregations API.
// It defines a single rule that aggregates metric time series.
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
type Dash0TimeSeriesAggregation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0TimeSeriesAggregationSpec   `json:"spec,omitempty"`
	Status Dash0TimeSeriesAggregationStatus `json:"status,omitempty"`
}

// Dash0TimeSeriesAggregationSpec defines the desired state of a time series aggregation rule.
type Dash0TimeSeriesAggregationSpec struct {
	// Whether this rule is active.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// Precedence when more than one rule matches a metric. Lower values are evaluated first (higher precedence).
	// Defaults to 0. Rules with equal priority are ordered deterministically by creation time, then by id.
	// +kubebuilder:validation:Optional
	Priority int `json:"priority,omitempty"`

	// Display configuration for the rule (e.g. the human-readable name shown in the Dash0 UI).
	// +kubebuilder:validation:Optional
	Display *Dash0TimeSeriesAggregationDisplay `json:"display,omitempty"`

	// Match selects which metrics this rule applies to.
	// +kubebuilder:validation:Required
	Match Dash0TimeSeriesAggregationMatch `json:"match"`

	// Sample controls the frequency at which matching metric data points are aggregated.
	// +kubebuilder:validation:Required
	Sample Dash0TimeSeriesAggregationSample `json:"sample"`

	// Modifications applied to the attributes of the aggregated metric.
	// +kubebuilder:validation:Optional
	AttributeModifications []Dash0TimeSeriesAggregationAttributeModification `json:"attributeModifications,omitempty"`
}

// Dash0TimeSeriesAggregationDisplay defines display configuration.
type Dash0TimeSeriesAggregationDisplay struct {
	// Human-readable name for this rule.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// Dash0TimeSeriesAggregationMatch selects which metrics are aggregated.
type Dash0TimeSeriesAggregationMatch struct {
	// Matcher applied to the metric name.
	// +kubebuilder:validation:Required
	MetricNameMatcher Dash0TimeSeriesAggregationMatcher `json:"metricNameMatcher"`

	// Additional attribute filters that further restrict which metrics contribute to the aggregation. A metric-name
	// match with no attribute filters is valid, so this may be omitted.
	// +kubebuilder:validation:Optional
	OtherFilters []Dash0TimeSeriesAggregationAttributeFilter `json:"otherFilters,omitempty"`
}

// Dash0TimeSeriesAggregationAttributeFilter is a single filter against an attribute key.
type Dash0TimeSeriesAggregationAttributeFilter struct {
	// The attribute key to match against.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`

	// The match operator.
	// +kubebuilder:validation:Required
	Operator Dash0TimeSeriesAggregationFilterOperator `json:"operator"`

	// Single value for operators that accept one value (is, is_not, contains, matches, ...).
	// +kubebuilder:validation:Optional
	Value *string `json:"value,omitempty"`

	// List of values for the is_one_of / is_not_one_of operators.
	// +kubebuilder:validation:Optional
	Values []string `json:"values,omitempty"`
}

// Dash0TimeSeriesAggregationMatcher is a key-less variant of an attribute filter: it matches against a single value
// (e.g. the metric name or an attribute key) without specifying the key itself.
type Dash0TimeSeriesAggregationMatcher struct {
	// The match operator.
	// +kubebuilder:validation:Required
	Operator Dash0TimeSeriesAggregationFilterOperator `json:"operator"`

	// Single value for operators that accept one value.
	// +kubebuilder:validation:Optional
	Value *string `json:"value,omitempty"`

	// List of values for the is_one_of / is_not_one_of operators.
	// +kubebuilder:validation:Optional
	Values []string `json:"values,omitempty"`
}

// Dash0TimeSeriesAggregationFilterOperator enumerates the supported filter / matcher operators.
// The set mirrors the AttributeFilterOperator enum in the dash0 backend's openapi-types/filtering.yml.
// +kubebuilder:validation:Enum=is;is_not;is_set;is_not_set;is_one_of;is_not_one_of;gt;lt;gte;lte;matches;does_not_match;contains;does_not_contain;starts_with;does_not_start_with;ends_with;does_not_end_with;is_any
type Dash0TimeSeriesAggregationFilterOperator string

// Dash0TimeSeriesAggregationSample controls the aggregation frequency and retention of source time series.
type Dash0TimeSeriesAggregationSample struct {
	// The frequency at which metric data points are aggregated. Set this to the scrape/export interval of your metrics
	// to avoid downsampling, or to a larger value to reduce granularity.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(\d+(ms|s|m|h|d|w|M|Q|y))+$`
	Interval string `json:"interval"`

	// The amount of time to account for delays in scraping, exporting, and data arrival. Defaults to 20s.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^(\d+(ms|s|m|h|d|w|M|Q|y))+$`
	Delay string `json:"delay,omitempty"`

	// Duration after which a source time series with no new data is dropped from memory. Must be at least delay plus
	// interval (raised to that when a smaller value is configured) and longer than the reporting period of the matched
	// sources. Defaults to 5 times the interval, subject to that same minimum.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^(\d+(ms|s|m|h|d|w|M|Q|y))+$`
	StaleAfter string `json:"staleAfter,omitempty"`
}

// Dash0TimeSeriesAggregationAttributeModification adds or removes attributes from the aggregated metric.
type Dash0TimeSeriesAggregationAttributeModification struct {
	// Whether the matched attributes are dropped or kept.
	// +kubebuilder:validation:Required
	Kind Dash0TimeSeriesAggregationAttributeModificationKind `json:"kind"`

	// The details of the modification.
	// +kubebuilder:validation:Required
	Spec Dash0TimeSeriesAggregationAttributeModificationSpec `json:"spec"`
}

// Dash0TimeSeriesAggregationAttributeModificationKind is the type of an attribute modification.
// +kubebuilder:validation:Enum=drop_attributes;keep_attributes
type Dash0TimeSeriesAggregationAttributeModificationKind string

const (
	Dash0TimeSeriesAggregationDropAttributes Dash0TimeSeriesAggregationAttributeModificationKind = "drop_attributes"
	Dash0TimeSeriesAggregationKeepAttributes Dash0TimeSeriesAggregationAttributeModificationKind = "keep_attributes"
)

// Dash0TimeSeriesAggregationAttributeModificationSpec defines which attributes a modification applies to.
type Dash0TimeSeriesAggregationAttributeModificationSpec struct {
	// The telemetry context the modification applies to.
	// +kubebuilder:validation:Optional
	Context Dash0TimeSeriesAggregationAttributeModificationContext `json:"context,omitempty"`

	// Matcher applied to the attribute keys the modification affects.
	// +kubebuilder:validation:Required
	KeyMatcher Dash0TimeSeriesAggregationMatcher `json:"keyMatcher"`
}

// Dash0TimeSeriesAggregationAttributeModificationContext is the telemetry context an attribute modification applies to.
// +kubebuilder:validation:Enum=resource;scope;datapoint
type Dash0TimeSeriesAggregationAttributeModificationContext string

// Dash0TimeSeriesAggregationStatus defines the observed state of a Dash0TimeSeriesAggregation resource.
type Dash0TimeSeriesAggregationStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus                      `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                                            `json:"synchronizedAt"`
	ValidationIssues       []string                                                               `json:"validationIssues,omitempty"`
	SynchronizationResults []Dash0TimeSeriesAggregationSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults"`
}

// Dash0TimeSeriesAggregationSynchronizationResultPerEndpointAndDataset captures the result of a single sync attempt.
type Dash0TimeSeriesAggregationSynchronizationResultPerEndpointAndDataset struct {
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

// Dash0TimeSeriesAggregationList contains a list of Dash0TimeSeriesAggregation resources.
//
// +kubebuilder:object:root=true
type Dash0TimeSeriesAggregationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0TimeSeriesAggregation `json:"items"`
}
