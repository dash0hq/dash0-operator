// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// SLO is the Schema for the SLOs API. It adopts the upstream OpenSLO v1 document shape (apiVersion "openslo/v1",
// kind "SLO"). The spec is a typed subset of the OpenSLO v1 SLO specification: exactly the subset that the Dash0
// SLO API supports (a single objective, an inline ratioMetric indicator with good + total Prometheus sources,
// Occurrences budgeting, and a rolling time window). Its JSON shape mirrors the Dash0 API SloSpec field-for-field,
// so the controller can carry the spec across to the API body without any field-level conversion.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=slos,scope=Namespaced
// +groupName=openslo.com
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="dash0.com/managed-by-operator=true"
type SLO struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SLOSpec   `json:"spec,omitempty"`
	Status SLOStatus `json:"status,omitempty"`
}

// SLOSpec defines the desired state of an SLO. It models only the subset of the OpenSLO v1 SLO specification that
// the Dash0 SLO API supports. The JSON tags match the Dash0 API SloSpec exactly.
type SLOSpec struct {
	// Description is an optional human-readable description of the SLO.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxLength=1050
	Description string `json:"description,omitempty"`

	// Service is the name of the service this SLO is associated with.
	// +kubebuilder:validation:Optional
	Service string `json:"service,omitempty"`

	// BudgetingMethod is the error-budget accounting method. Only Occurrences (a ratio of good events to total
	// events) is supported by Dash0.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Occurrences
	BudgetingMethod string `json:"budgetingMethod"`

	// TimeWindow is the time window over which the SLO is evaluated. Exactly one item is supported, and it must be a
	// rolling window (for example, a rolling 28d window). Calendar-aligned windows are not supported.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1
	TimeWindow []SLOTimeWindow `json:"timeWindow"`

	// Indicator is the inline service level indicator (SLI) for the SLO. Only an inline indicator with a ratioMetric
	// is supported; indicatorRef and thresholdMetric are not supported.
	// +kubebuilder:validation:Required
	Indicator SLOIndicator `json:"indicator"`

	// Objectives are the objective thresholds for the SLO. Dash0 supports exactly one objective.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1
	Objectives []SLOObjective `json:"objectives"`
}

// SLOTimeWindow is a rolling time window over which the SLO is evaluated.
type SLOTimeWindow struct {
	// Duration is the length of the rolling window as an OpenSLO duration string (for example, "28d" or "4w").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^\d+(ms|s|m|h|d|w|M|Q|y)$`
	Duration string `json:"duration"`

	// IsRolling indicates whether this is a rolling time window. Dash0 only supports rolling windows, so this must
	// be set to true.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=true
	IsRolling bool `json:"isRolling"`
}

// SLOIndicator is an inline service level indicator (SLI).
type SLOIndicator struct {
	// Metadata holds optional metadata for the inline SLI.
	// +kubebuilder:validation:Optional
	Metadata *SLOIndicatorMetadata `json:"metadata,omitempty"`

	// Spec is the SLI specification.
	// +kubebuilder:validation:Required
	Spec SLOIndicatorSpec `json:"spec"`
}

// SLOIndicatorMetadata holds metadata for an inline SLI.
type SLOIndicatorMetadata struct {
	// Name is the name of the SLI.
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`

	// DisplayName is a human-readable display name for the SLI.
	// +kubebuilder:validation:Optional
	DisplayName string `json:"displayName,omitempty"`
}

// SLOIndicatorSpec is the specification of an inline SLI. Only a ratioMetric is supported.
type SLOIndicatorSpec struct {
	// RatioMetric is a ratio-based SLI metric with a good and a total Prometheus source.
	// +kubebuilder:validation:Required
	RatioMetric SLORatioMetric `json:"ratioMetric"`
}

// SLORatioMetric is a ratio-based SLI metric. Dash0 supports the good + total shape.
type SLORatioMetric struct {
	// Counter indicates whether the underlying metric is a monotonically increasing counter (true) or a gauge-like
	// value (false).
	// +kubebuilder:validation:Optional
	Counter *bool `json:"counter,omitempty"`

	// Good is the metric source counting good (successful) events.
	// +kubebuilder:validation:Required
	Good SLOMetricSourceWrapper `json:"good"`

	// Total is the metric source counting all events.
	// +kubebuilder:validation:Required
	Total SLOMetricSourceWrapper `json:"total"`
}

// SLOMetricSourceWrapper wraps a metric source for use as the numerator or denominator of a ratio metric.
type SLOMetricSourceWrapper struct {
	// MetricSource is the underlying metric source.
	// +kubebuilder:validation:Required
	MetricSource SLOMetricSource `json:"metricSource"`
}

// SLOMetricSource is the connection and query details for a metric data source. Only the Prometheus type is
// supported.
type SLOMetricSource struct {
	// Type is the metric source type. Only "Prometheus" is supported.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Prometheus
	Type string `json:"type"`

	// Spec holds the data-source-specific query parameters (for Prometheus, a PromQL query).
	// +kubebuilder:validation:Required
	Spec SLOMetricSourceSpec `json:"spec"`
}

// SLOMetricSourceSpec holds the query for a Prometheus metric source.
type SLOMetricSourceSpec struct {
	// Query is the PromQL query used to compute the metric.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Query string `json:"query"`
}

// SLOObjective is an objective threshold for the SLO.
type SLOObjective struct {
	// DisplayName is a human-readable name for this objective.
	// +kubebuilder:validation:Optional
	DisplayName string `json:"displayName,omitempty"`

	// Target is the budget target for this objective as a fraction in the range [0.0, 1.0) (for example, 0.99 for a
	// 99 percent target).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +kubebuilder:validation:ExclusiveMaximum=true
	Target float64 `json:"target"`
}

// SLOStatus defines the observed state of an SLO. It follows the same shape used by the operator's first-party synced
// CRDs (see Dash0SyntheticCheckStatus).
type SLOStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                       `json:"synchronizedAt"`
	ValidationIssues       []string                                          `json:"validationIssues,omitempty"`
	SynchronizationResults []SLOSynchronizationResultPerEndpointAndDataset   `json:"synchronizationResults"`
}

// SLOSynchronizationResultPerEndpointAndDataset defines the synchronization result per endpoint and dataset.
type SLOSynchronizationResultPerEndpointAndDataset struct {
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

// SLOList contains a list of SLO.
//
// +kubebuilder:object:root=true
type SLOList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SLO `json:"items"`
}
