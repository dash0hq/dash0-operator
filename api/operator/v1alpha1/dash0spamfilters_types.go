// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0SpamFilter is the Schema for the Dash0SpamFilter API
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
type Dash0SpamFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0SpamFilterSpec   `json:"spec,omitempty"`
	Status Dash0SpamFilterStatus `json:"status,omitempty"`
}

// Dash0SpamFilterSpec defines the desired state of Dash0SpamFilter
type Dash0SpamFilterSpec struct {
	// The signal contexts this spam filter applies to (e.g., log, span, metric).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Contexts []string `json:"contexts"`

	// The filter conditions that define which telemetry data to drop.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Filter []Dash0SpamFilterCondition `json:"filter"`
}

// Dash0SpamFilterCondition defines a single filter condition for spam filtering,
// matching the Dash0 API's AttributeFilter schema.
type Dash0SpamFilterCondition struct {
	// The attribute key to match against.
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// The comparison operator (e.g., "is", "is_not", "contains", "starts_with").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=is;is_not;is_set;is_not_set;is_one_of;is_not_one_of;gt;lt;gte;lte;matches;does_not_match;contains;does_not_contain;starts_with;does_not_start_with;ends_with;does_not_end_with;is_any
	Operator string `json:"operator"`

	// The value to compare against. Optional for operators like "is_set" and "is_not_set".
	// +kubebuilder:validation:Optional
	Value *string `json:"value,omitempty"`
}

// Dash0SpamFilterStatus defines the observed state of Dash0SpamFilter
type Dash0SpamFilterStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus           `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                                 `json:"synchronizedAt"`
	ValidationIssues       []string                                                    `json:"validationIssues,omitempty"`
	SynchronizationResults []Dash0SpamFilterSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults"`
}

// Dash0SpamFilterSynchronizationResultPerEndpointAndDataset defines the synchronization result per endpoint and dataset
type Dash0SpamFilterSynchronizationResultPerEndpointAndDataset struct {
	SynchronizationStatus dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	Dash0ApiEndpoint      string                                            `json:"dash0ApiEndpoint,omitempty"`
	Dash0Dataset          string                                            `json:"dash0Dataset,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Id string `json:"dash0Id,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin          string `json:"dash0Origin,omitempty"`
	SynchronizationError string `json:"synchronizationError,omitempty"`
}

//+kubebuilder:object:root=true

// Dash0SpamFilterList contains a list of Dash0SpamFilter resources.
type Dash0SpamFilterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0SpamFilter `json:"items"`
}
