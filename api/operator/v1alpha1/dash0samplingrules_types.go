// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0SamplingRule is the Schema for the dash0samplingrules API
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
type Dash0SamplingRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0SamplingRuleSpec   `json:"spec,omitempty"`
	Status Dash0SamplingRuleStatus `json:"status,omitempty"`
}

// Dash0SamplingRuleSpec defines the desired state of Dash0SamplingRule
type Dash0SamplingRuleSpec struct {
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// +kubebuilder:validation:Optional
	Display *Dash0SamplingRuleDisplay `json:"display,omitempty"`

	// +kubebuilder:validation:Required
	Conditions Dash0SamplingRuleCondition `json:"conditions"`

	// +kubebuilder:validation:Optional
	RateLimit *Dash0SamplingRuleRateLimit `json:"rateLimit,omitempty"`
}

// Dash0SamplingRuleDisplay defines the display configuration
type Dash0SamplingRuleDisplay struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// Dash0SamplingRuleRateLimit defines the rate limit configuration (maximum traces per minute)
type Dash0SamplingRuleRateLimit struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	Rate int `json:"rate"`
}

// Dash0SamplingRuleCondition defines a sampling condition. This is a discriminated union on the kind field.
// +kubebuilder:validation:XValidation:rule="(self.kind == 'probabilistic' && has(self.spec) && has(self.spec.rate)) || (self.kind == 'ottl' && has(self.spec) && has(self.spec.ottl)) || (self.kind == 'error') || (self.kind == 'and' && has(self.spec))",message="condition spec must match the condition kind"
type Dash0SamplingRuleCondition struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=probabilistic;ottl;error;and
	Kind string `json:"kind"`

	// +kubebuilder:validation:Optional
	Spec *Dash0SamplingRuleConditionSpec `json:"spec,omitempty"`
}

// Dash0SamplingRuleConditionSpec defines the specification for a sampling condition
type Dash0SamplingRuleConditionSpec struct {
	// For probabilistic conditions: sampling rate between 0 and 1 (e.g. "0.5" for 50%)
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^(0(\.\d+)?|1(\.0+)?)$`
	Rate *string `json:"rate,omitempty"`

	// For OTTL conditions: an OpenTelemetry Transformation Language expression
	// +kubebuilder:validation:Optional
	Ottl *string `json:"ottl,omitempty"`

	// For 'and' conditions: a list of sub-conditions that must all be satisfied.
	// Each element must be a valid condition object with kind and optional spec fields.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:pruning:PreserveUnknownFields
	Conditions []apiextensionsv1.JSON `json:"conditions,omitempty"`
}

// Dash0SamplingRuleStatus defines the observed state of Dash0SamplingRule
type Dash0SamplingRuleStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus             `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                                   `json:"synchronizedAt"`
	ValidationIssues       []string                                                      `json:"validationIssues,omitempty"`
	SynchronizationResults []Dash0SamplingRuleSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults"`
}

// Dash0SamplingRuleSynchronizationResultPerEndpointAndDataset defines the synchronization result per endpoint and dataset
type Dash0SamplingRuleSynchronizationResultPerEndpointAndDataset struct {
	SynchronizationStatus dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	Dash0ApiEndpoint      string                                            `json:"dash0ApiEndpoint,omitempty"`
	Dash0Dataset          string                                            `json:"dash0Dataset,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Id string `json:"dash0Id,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin          string `json:"dash0Origin,omitempty"`
	SynchronizationError string `json:"synchronizationError,omitempty"`
}

// Dash0SamplingRuleList contains a list of Dash0SamplingRule
//
// +kubebuilder:object:root=true
type Dash0SamplingRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0SamplingRule `json:"items"`
}
