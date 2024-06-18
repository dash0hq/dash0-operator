// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	FinalizerId = "operator.dash0.com/finalizer"
)

// Dash0Spec defines the desired state of the Dash0 custom resource.
type Dash0Spec struct {
	// Global opt-out for workload instrumentation for the target namespace. You can opt-out of instrumenting workloads
	// entirely by setting this option to false. By default, this setting is true and Kubernetes workloads will be
	// intrumented by the operator to send telemetry to Dash0. Setting it to false will prevent workload instrumentation
	// in the target namespace. More fine-grained control over instrumentation is available via the settings
	// InstrumentExistingWorkloads, InstrumentNewWorkloads and UninstrumentWorkloadsOnDelete, as well as by setting the
	// label dash0.com/enable=false on individual workloads.
	//
	// The default value for this option is true.
	//
	// +kubebuilder:validation:Optional
	InstrumentWorkloads *bool `json:"instrumentWorkloads"`

	// Opt-out for workload instrumentation of existing workloads for the target namespace. You can opt-out of
	// instrumenting existing workloads by setting this option to false. By default, when the Dash0 custom resource
	// is deployed to a namespace (and the Dash0 Kubernetes operator is active), the operator will instrument the
	// workloads already running in that namesapce, to send telemetry to Dash0. Setting this option to false will
	// prevent that behavior, but workloads that are deployed after Dash0 custom resource will still be instrumented
	// (see option InstrumentNewWorkloads). More fine-grained control over instrumentation on a per-workload level is
	// available by setting the label dash0.com/enable=false on individual workloads.
	//
	// The default value for this option is true.
	//
	// This option has no effect if InstrumentWorkloads is set to false.
	//
	// +kubebuilder:validation:Optional
	InstrumentExistingWorkloads *bool `json:"instrumentExistingWorkloads"`

	// Opt-out for workload instrumentation of newly deployed workloads for the target namespace. You can opt-out of
	// instrumenting workloads at the time they are deployed by setting this option to false. By default, when the Dash0
	// custom resource is present in a namespace (and the Dash0 Kubernetes operator is active), the operator will
	// instrument new workloads to send telemetry to Dash0, at the time they are deployed to that namespace.
	// Setting this option to false will prevent that behavior, but workloads existing when the Dash0 custom resource
	// is deployed will still be instrumented (see option InstrumentExistingWorkloads). More fine-grained control over
	// instrumentation on a per-workload level is available by setting the label dash0.com/enable=false on individual
	// workloads.
	//
	// The default value for this option is true.
	//
	// This option has no effect if InstrumentWorkloads is set to false.
	//
	// +kubebuilder:validation:Optional
	InstrumentNewWorkloads *bool `json:"InstrumentNewWorkloads"`

	// Opt-out for removing the Dash0 instrumentation from workloads when the Dash0 custom resource is removed from a
	// namespace, or when the Dash0 Kubernetes operator is deleted entirely. By default, this setting is true and the
	// operator will revert the instrumentation modifications it applied to workloads to send telemetry to Dash0.
	// Setting this option to false will prevent this behavior.
	//
	// The default value for this option is true.
	//
	// +kubebuilder:validation:Optional
	UninstrumentWorkloadsOnDelete *bool `json:"uninstrumentWorkloadsOnDelete"`
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

func (d *Dash0) IsMarkedForDeletion() bool {
	deletionTimestamp := d.GetDeletionTimestamp()
	return deletionTimestamp != nil && !deletionTimestamp.IsZero()
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

func (d *Dash0) EnsureResourceIsMarkedAsAboutToBeDeleted() {
	d.EnsureResourceIsMarkedAsDegraded(
		"Dash0CustomResourceHasBeenRemoved",
		"Dash0 is inactive in this namespace now.",
		"Dash0CustomResourceHasBeenRemoved",
		"Dash0 is about to be deleted.",
	)
}

func (d *Dash0) EnsureResourceIsMarkedAsDegraded(
	availableReason string,
	availableMessage string,
	degradedReason string,
	degradedMessage string,
) {
	// If the available status is already false, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionFalse,
			Reason:  availableReason,
			Message: availableMessage,
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  degradedReason,
			Message: degradedMessage,
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
