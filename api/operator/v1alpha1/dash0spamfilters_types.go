// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha2 "github.com/dash0hq/dash0-operator/api/operator/v1alpha2"
)

// Dash0SpamFilter is the Schema for the Dash0SpamFilter API
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
// +kubebuilder:conversion:spoke
type Dash0SpamFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0SpamFilterSpec   `json:"spec,omitempty"`
	Status Dash0SpamFilterStatus `json:"status,omitempty"`
}

// Dash0SpamFilterSpec defines the desired state of Dash0SpamFilter
type Dash0SpamFilterSpec struct {
	// The signal contexts this spam filter applies to. Only the first element is used;
	// additional elements are ignored when converting to the v1alpha2 hub version.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:items:Enum=datapoint;log;span;web_event
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

// Ensure Dash0SpamFilter implements the conversion.Convertible interface.
var _ conversion.Convertible = &Dash0SpamFilter{}

// ConvertTo converts this Dash0SpamFilter resource (v1alpha1) to the hub version (v1alpha2).
func (src *Dash0SpamFilter) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*dash0v1alpha2.Dash0SpamFilter)
	dst.ObjectMeta = src.ObjectMeta

	// Spec: contexts (array) → context (scalar)
	if len(src.Spec.Contexts) > 0 {
		dst.Spec.Context = src.Spec.Contexts[0]
	}
	dst.Spec.Filter = make([]dash0v1alpha2.Dash0SpamFilterCondition, len(src.Spec.Filter))
	for i, f := range src.Spec.Filter {
		dst.Spec.Filter[i] = dash0v1alpha2.Dash0SpamFilterCondition{
			Key:      f.Key,
			Operator: f.Operator,
			Value:    f.Value,
		}
	}

	// Status
	dst.Status.SynchronizationStatus = src.Status.SynchronizationStatus
	dst.Status.SynchronizedAt = src.Status.SynchronizedAt
	dst.Status.ValidationIssues = src.Status.ValidationIssues
	dst.Status.SynchronizationResults = make(
		[]dash0v1alpha2.Dash0SpamFilterSynchronizationResultPerEndpointAndDataset,
		len(src.Status.SynchronizationResults),
	)
	for i, r := range src.Status.SynchronizationResults {
		dst.Status.SynchronizationResults[i] = dash0v1alpha2.Dash0SpamFilterSynchronizationResultPerEndpointAndDataset{
			SynchronizationStatus: r.SynchronizationStatus,
			Dash0ApiEndpoint:      r.Dash0ApiEndpoint,
			Dash0Dataset:          r.Dash0Dataset,
			Dash0Id:               r.Dash0Id,
			Dash0Origin:           r.Dash0Origin,
			SynchronizationError:  r.SynchronizationError,
		}
	}

	return nil
}

// ConvertFrom converts the hub version (v1alpha2) to this Dash0SpamFilter resource version (v1alpha1).
func (dst *Dash0SpamFilter) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*dash0v1alpha2.Dash0SpamFilter)
	dst.ObjectMeta = src.ObjectMeta

	// Spec: context (scalar) → contexts (array)
	if src.Spec.Context != "" {
		dst.Spec.Contexts = []string{src.Spec.Context}
	}
	dst.Spec.Filter = make([]Dash0SpamFilterCondition, len(src.Spec.Filter))
	for i, f := range src.Spec.Filter {
		dst.Spec.Filter[i] = Dash0SpamFilterCondition{
			Key:      f.Key,
			Operator: f.Operator,
			Value:    f.Value,
		}
	}

	// Status
	dst.Status.SynchronizationStatus = src.Status.SynchronizationStatus
	dst.Status.SynchronizedAt = src.Status.SynchronizedAt
	dst.Status.ValidationIssues = src.Status.ValidationIssues
	dst.Status.SynchronizationResults = make(
		[]Dash0SpamFilterSynchronizationResultPerEndpointAndDataset,
		len(src.Status.SynchronizationResults),
	)
	for i, r := range src.Status.SynchronizationResults {
		dst.Status.SynchronizationResults[i] = Dash0SpamFilterSynchronizationResultPerEndpointAndDataset{
			SynchronizationStatus: r.SynchronizationStatus,
			Dash0ApiEndpoint:      r.Dash0ApiEndpoint,
			Dash0Dataset:          r.Dash0Dataset,
			Dash0Id:               r.Dash0Id,
			Dash0Origin:           r.Dash0Origin,
			SynchronizationError:  r.SynchronizationError,
		}
	}

	return nil
}
