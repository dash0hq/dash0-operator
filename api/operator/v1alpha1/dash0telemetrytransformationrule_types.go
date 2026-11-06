// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0TelemetryTransformationRule defines a telemetry transformation rule synchronized to Dash0.
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
type Dash0TelemetryTransformationRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec Dash0TelemetryTransformationRuleSpec `json:"spec"`

	// +kubebuilder:validation:Optional
	Status Dash0TelemetryTransformationRuleStatus `json:"status,omitempty"`
}

// Dash0TelemetryTransformationRuleSpec defines the desired state of a telemetry transformation rule.
//
// +kubebuilder:validation:XValidation:rule="self.context in ['resource', 'scope'] || (self.signal == 'traces' && self.context in ['span', 'spanevent']) || (self.signal == 'metrics' && self.context in ['metric', 'datapoint']) || (self.signal == 'logs' && self.context == 'log') || (self.signal == 'profiles' && self.context == 'profile')",message="context must be valid for the selected signal"
type Dash0TelemetryTransformationRuleSpec struct {
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// +kubebuilder:validation:Optional
	Display *Dash0TelemetryTransformationRuleDisplay `json:"display,omitempty"`

	// +kubebuilder:validation:Required
	Signal Dash0TelemetryTransformationRuleSignalType `json:"signal"`

	// +kubebuilder:validation:Required
	Context Dash0TelemetryTransformationRuleContext `json:"context"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=400
	// +kubebuilder:validation:MultipleOf=100
	// +kubebuilder:default=200
	Priority *int `json:"priority,omitempty"`

	// +kubebuilder:validation:Optional
	Conditions []string `json:"conditions,omitempty"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +listType=atomic
	Transformations []Dash0TelemetryTransformationRuleTransformation `json:"transformations"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=10
	Examples []Dash0TelemetryTransformationRuleExample `json:"examples,omitempty"`
}

// Dash0TelemetryTransformationRuleDisplay defines the rule's display configuration.
type Dash0TelemetryTransformationRuleDisplay struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// Dash0TelemetryTransformationRuleSignalType identifies the telemetry signal to transform.
// +kubebuilder:validation:Enum=traces;metrics;logs;profiles
type Dash0TelemetryTransformationRuleSignalType string

const (
	Dash0TelemetryTransformationRuleSignalTypeTraces   Dash0TelemetryTransformationRuleSignalType = "traces"
	Dash0TelemetryTransformationRuleSignalTypeMetrics  Dash0TelemetryTransformationRuleSignalType = "metrics"
	Dash0TelemetryTransformationRuleSignalTypeLogs     Dash0TelemetryTransformationRuleSignalType = "logs"
	Dash0TelemetryTransformationRuleSignalTypeProfiles Dash0TelemetryTransformationRuleSignalType = "profiles"
)

// Dash0TelemetryTransformationRuleContext identifies the OTTL evaluation context.
// +kubebuilder:validation:Enum=resource;scope;span;spanevent;metric;datapoint;log;profile
type Dash0TelemetryTransformationRuleContext string

const (
	Dash0TelemetryTransformationRuleContextResource  Dash0TelemetryTransformationRuleContext = "resource"
	Dash0TelemetryTransformationRuleContextScope     Dash0TelemetryTransformationRuleContext = "scope"
	Dash0TelemetryTransformationRuleContextSpan      Dash0TelemetryTransformationRuleContext = "span"
	Dash0TelemetryTransformationRuleContextSpanEvent Dash0TelemetryTransformationRuleContext = "spanevent"
	Dash0TelemetryTransformationRuleContextMetric    Dash0TelemetryTransformationRuleContext = "metric"
	Dash0TelemetryTransformationRuleContextDataPoint Dash0TelemetryTransformationRuleContext = "datapoint"
	Dash0TelemetryTransformationRuleContextLog       Dash0TelemetryTransformationRuleContext = "log"
	Dash0TelemetryTransformationRuleContextProfile   Dash0TelemetryTransformationRuleContext = "profile"
)

// Dash0TelemetryTransformationRuleTransformation defines a named, ordered list of OTTL statements.
type Dash0TelemetryTransformationRuleTransformation struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	// +listType=atomic
	Statements []string `json:"statements"`
}

// Dash0TelemetryTransformationRuleExample defines a named OTLP/JSON export request for testing the rule.
type Dash0TelemetryTransformationRuleExample struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	Otlp apiextensionsv1.JSON `json:"otlp"`
}

// Dash0TelemetryTransformationRuleStatus defines the observed state of a telemetry transformation rule.
type Dash0TelemetryTransformationRuleStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus                            `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                                                  `json:"synchronizedAt"`
	ValidationIssues       []string                                                                     `json:"validationIssues,omitempty"`
	SynchronizationResults []Dash0TelemetryTransformationRuleSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults"`
}

// Dash0TelemetryTransformationRuleSynchronizationResultPerEndpointAndDataset defines a single synchronization result.
type Dash0TelemetryTransformationRuleSynchronizationResultPerEndpointAndDataset struct {
	SynchronizationStatus dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	Dash0ApiEndpoint      string                                            `json:"dash0ApiEndpoint,omitempty"`
	Dash0Dataset          string                                            `json:"dash0Dataset,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Id string `json:"dash0Id,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin          string `json:"dash0Origin,omitempty"`
	SynchronizationError string `json:"synchronizationError,omitempty"`
	// HttpStatusCode is absent for successful synchronizations and transport errors without an HTTP response.
	// +kubebuilder:validation:Optional
	HttpStatusCode int `json:"httpStatusCode,omitempty"`
}

// Dash0TelemetryTransformationRuleList contains a list of telemetry transformation rules.
//
// +kubebuilder:object:root=true
type Dash0TelemetryTransformationRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0TelemetryTransformationRule `json:"items"`
}
