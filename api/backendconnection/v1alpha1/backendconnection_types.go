// SPDX-FileCopyrightText: Copyright 2024 BackendConnection Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/backendconnection/util"
	"github.com/dash0hq/dash0-operator/internal/common/controller"
)

const (
	FinalizerId = "operator.dash0.com/backend-connection-finalizer"
)

// BackendConnectionSpec defines the desired state of BackendConnection
type BackendConnectionSpec struct {
}

// BackendConnectionStatus defines the observed state of BackendConnection
type BackendConnectionStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// BackendConnection is the Schema for the backendconnections API
type BackendConnection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackendConnectionSpec   `json:"spec,omitempty"`
	Status BackendConnectionStatus `json:"status,omitempty"`
}

func (bc *BackendConnection) IsMarkedForDeletion() bool {
	deletionTimestamp := bc.GetDeletionTimestamp()
	return deletionTimestamp != nil && !deletionTimestamp.IsZero()
}

func (bc *BackendConnection) IsAvailable() bool {
	if condition := bc.getCondition(util.ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (bc *BackendConnection) getCondition(conditionType util.ConditionType) *metav1.Condition {
	for _, c := range bc.Status.Conditions {
		if c.Type == string(conditionType) {
			return &c

		}
	}
	return nil
}

func (bc *BackendConnection) SetAvailableConditionToUnknown() {
	meta.SetStatusCondition(
		&bc.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Creating or updating a Dash0 backend connection in this namespace now.",
		})
	meta.SetStatusCondition(
		&bc.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileStarted",
			Message: "Creating/updating the Dash0 backend connection is still in progress.",
		})
}

func (bc *BackendConnection) EnsureResourceIsMarkedAsAvailable(resourcesHaveBeenCreated bool, resourcesHaveBeenUpdated bool) {
	// If the available status is already true, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	var message string
	if resourcesHaveBeenCreated {
		message = "Resources for the Dash0 backend connection have been created in this namespace."
	} else if resourcesHaveBeenUpdated {
		message = "Resources for the Dash0 backend connection have been updated in this namespace."
	} else {
		message = "The resources for the Dash0 backend connection in this namespace are up to date."
	}
	meta.SetStatusCondition(
		&bc.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: message,
		})
	meta.RemoveStatusCondition(&bc.Status.Conditions, string(util.ConditionTypeDegraded))
}

func (bc *BackendConnection) EnsureResourceIsMarkedAsAboutToBeDeleted() {
	bc.EnsureResourceIsMarkedAsDegraded(
		"BackendConnectionResourceHasBeenRemoved",
		"BackendConnection is inactive in this namespace now.",
	)
}

func (bc *BackendConnection) EnsureResourceIsMarkedAsDegraded(
	reason string,
	message string,
) {
	// If the available status is already false, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	meta.SetStatusCondition(
		&bc.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	meta.SetStatusCondition(
		&bc.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		})
}

func (bc *BackendConnection) GetResourceTypeName() string {
	return "BackendConnectionResource"
}
func (bc *BackendConnection) GetNaturalLanguageResourceTypeName() string {
	return "Dash0 backend connection resource"
}
func (bc *BackendConnection) Get() client.Object {
	return bc
}
func (bc *BackendConnection) GetName() string {
	return bc.Name
}
func (bc *BackendConnection) GetUid() types.UID {
	return bc.UID
}
func (bc *BackendConnection) GetCreationTimestamp() metav1.Time {
	return bc.CreationTimestamp
}
func (bc *BackendConnection) GetReceiver() client.Object {
	return &BackendConnection{}
}
func (bc *BackendConnection) GetListReceiver() client.ObjectList {
	return &BackendConnectionList{}
}
func (bc *BackendConnection) Items(list client.ObjectList) []client.Object {
	items := list.(*BackendConnectionList).Items
	result := make([]client.Object, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}
func (bc *BackendConnection) At(list client.ObjectList, index int) controller.CustomResource {
	return &list.(*BackendConnectionList).Items[index]
}

//+kubebuilder:object:root=true

// BackendConnectionList contains a list of BackendConnection
type BackendConnectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackendConnection `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackendConnection{}, &BackendConnectionList{})
}
