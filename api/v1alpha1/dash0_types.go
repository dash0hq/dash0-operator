// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dash0hq/dash0-operator/internal/util"
)

// Dash0Spec defines the desired state of the Dash0 custom resource.
type Dash0Spec struct {
}

// Dash0Status defines the observed state of the Dash0 custom resource.
type Dash0Status struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Dash0 is the Schema for the dash0s API
type Dash0 struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0Spec   `json:"spec,omitempty"`
	Status Dash0Status `json:"status,omitempty"`
}

func (d *Dash0) IsAvailable() bool {
	if condition := d.getCondition(util.ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0) getCondition(conditionType util.ConditionType) *metav1.Condition {
	for _, c := range d.Status.Conditions {
		if c.Type == string(conditionType) {
			return &c

		}
	}
	return nil
}

func (d *Dash0) SetAvailableConditionToUnknown() {
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Dash0 has started resource reconciliation.",
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileStarted",
			Message: "Dash0 is still starting.",
		})
}

func (d *Dash0) EnsureResourceIsMarkedAsAvailable() {
	// If the available status is already true, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: "Dash0 is active in this namespace now.",
		})
	meta.RemoveStatusCondition(&d.Status.Conditions, string(util.ConditionTypeDegraded))
}

func (d *Dash0) EnsureResourceIsMarkedAsUnavailable() {
	// If the available status is already false, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionFalse,
			Reason:  "Dash0CustomResourceHasBeenRemoved",
			Message: "Dash0 is inactive in this namespace now.",
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  "Dash0CustomResourceHasBeenRemoved",
			Message: "Dash0 is about to be deleted.",
		})
}

//+kubebuilder:object:root=true

// Dash0List contains a list of Dash0
type Dash0List struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0 `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dash0{}, &Dash0List{})
}
