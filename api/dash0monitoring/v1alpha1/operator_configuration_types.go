// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/dash0monitoring"
)

// Dash0OperatorConfigurationSpec describes cluster-wide configuration settings for the Dash0 Kubernetes operator.
type Dash0OperatorConfigurationSpec struct {
	// The configuration of the default observability backend to which telemetry data will be sent by the operator, as
	// well as the backend that will receive the operator's self-monitoring data. This property is mandatory.
	// This can either be Dash0 or another OTLP-compatible backend. You can also combine up to three exporters (i.e.
	// Dash0 plus gRPC plus HTTP). This allows sending the same data to two or three targets simultaneously. At least
	// one exporter has to be defined.
	//
	// Please note that self-monitoring data is only sent to one backend, with Dash0 taking precedence over gRPC and
	// HTTP, and gRPC taking precedence over HTTP if multiple exports are defined. Furthermore, HTTP export with JSON
	// encoding is not supported for self-monitoring telemetry.
	//
	// +kubebuilder:validation:Required
	Export *Export `json:"export,omitempty"`

	// Global opt-out for self-monitoring for this operator
	// +kubebuilder:default={enabled: true}
	SelfMonitoring SelfMonitoring `json:"selfMonitoring,omitempty"`
}

// SelfMonitoring describes how the operator will report telemetry about its working to the backend.
type SelfMonitoring struct {
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`
}

// Dash0OperatorConfigurationStatus defines the observed state of the Dash0 operator configuration resource.
type Dash0OperatorConfigurationStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// Dash0OperatorConfiguration is the schema for the Dash0OperatorConfiguration API
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +groupName=operator.dash0.com
type Dash0OperatorConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0OperatorConfigurationSpec   `json:"spec,omitempty"`
	Status Dash0OperatorConfigurationStatus `json:"status,omitempty"`
}

func (d *Dash0OperatorConfiguration) IsMarkedForDeletion() bool {
	deletionTimestamp := d.GetDeletionTimestamp()
	return deletionTimestamp != nil && !deletionTimestamp.IsZero()
}

func (d *Dash0OperatorConfiguration) IsAvailable() bool {
	if condition := d.getCondition(ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0OperatorConfiguration) getCondition(conditionType ConditionType) *metav1.Condition {
	for _, c := range d.Status.Conditions {
		if c.Type == string(conditionType) {
			return &c

		}
	}
	return nil
}

func (d *Dash0OperatorConfiguration) SetAvailableConditionToUnknown() {
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Dash0 has started resource reconciliation for the cluster-wide operator configuration.",
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileStarted",
			Message: "Dash0 operator configuration resource reconciliation is in progress.",
		})
}

func (d *Dash0OperatorConfiguration) EnsureResourceIsMarkedAsAvailable() {
	// If the available status is already true, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: "Dash0 operator configuration is available in this cluster now.",
		})
	meta.RemoveStatusCondition(&d.Status.Conditions, string(ConditionTypeDegraded))
}

func (d *Dash0OperatorConfiguration) EnsureResourceIsMarkedAsAboutToBeDeleted() {
	d.EnsureResourceIsMarkedAsDegraded(
		"Dash0OperatorConfigurationResourceHasBeenRemoved",
		"Dash0 operator configuration is inactive in this cluster now.",
	)
}

func (d *Dash0OperatorConfiguration) EnsureResourceIsMarkedAsDegraded(
	reason string,
	message string,
) {
	// If the available status is already false, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(ConditionTypeAvailable),
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		})
}

func (d *Dash0OperatorConfiguration) HasDash0ApiAccessConfigured() bool {
	return d.Spec.Export != nil &&
		d.Spec.Export.Dash0 != nil &&
		d.Spec.Export.Dash0.ApiEndpoint != "" &&
		(d.Spec.Export.Dash0.Authorization.Token != nil || d.Spec.Export.Dash0.Authorization.SecretRef != nil)
}

func (d *Dash0OperatorConfiguration) GetDash0AuthorizationIfConfigured() *Authorization {
	if d.Spec.Export == nil {
		return nil
	}
	if d.Spec.Export.Dash0 == nil {
		return nil
	}

	authorization := d.Spec.Export.Dash0.Authorization
	if (authorization.Token != nil && *authorization.Token != "") ||
		(authorization.SecretRef != nil && authorization.SecretRef.Name != "" && authorization.SecretRef.Key != "") {
		return &authorization
	}
	return nil
}

func (d *Dash0OperatorConfiguration) GetResourceTypeName() string {
	return "Dash0OperatorConfiguration"
}

func (d *Dash0OperatorConfiguration) GetNaturalLanguageResourceTypeName() string {
	return "Dash0 operator configuration resource"
}

func (d *Dash0OperatorConfiguration) Get() client.Object {
	return d
}

func (d *Dash0OperatorConfiguration) GetName() string {
	return d.Name
}

func (d *Dash0OperatorConfiguration) GetUid() types.UID {
	return d.UID
}

func (d *Dash0OperatorConfiguration) GetCreationTimestamp() metav1.Time {
	return d.CreationTimestamp
}

func (d *Dash0OperatorConfiguration) GetReceiver() client.Object {
	return &Dash0OperatorConfiguration{}
}

func (d *Dash0OperatorConfiguration) GetListReceiver() client.ObjectList {
	return &Dash0OperatorConfigurationList{}
}

func (d *Dash0OperatorConfiguration) IsClusterResource() bool {
	return true
}

func (d *Dash0OperatorConfiguration) RequestToName(ctrl.Request) string {
	return d.Name
}

func (d *Dash0OperatorConfiguration) Items(list client.ObjectList) []client.Object {
	items := list.(*Dash0OperatorConfigurationList).Items
	result := make([]client.Object, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}

func (d *Dash0OperatorConfiguration) At(list client.ObjectList, index int) dash0common.Dash0Resource {
	return &list.(*Dash0OperatorConfigurationList).Items[index]
}

//+kubebuilder:object:root=true

// Dash0OperatorConfigurationList contains a list of Dash0OperatorConfiguration resources.
type Dash0OperatorConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0OperatorConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dash0OperatorConfiguration{}, &Dash0OperatorConfigurationList{})
}
