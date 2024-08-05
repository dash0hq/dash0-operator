// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/common/controller"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

const (
	FinalizerId = "operator.dash0.com/dash0-monitoring-finalizer"
)

// Dash0MonitoringSpec defines the desired state of the Dash0 monitoring resource.
type Dash0MonitoringSpec struct {
	// The URL of the observability backend to which telemetry data will be sent. This property is mandatory. The value
	// needs to be the OTLP/gRPC endpoint of your Dash0 organization. The correct OTLP/gRPC endpoint can be copied fom
	// https://app.dash0.com/settings. The correct endpoint value will always start with `ingress.` and end in
	// `dash0.com:4317`.
	//
	// +kubebuilder:validation:Mandatory
	IngressEndpoint string `json:"ingressEndpoint"`

	// The Dash0 authorization token. This property is optional, but either this property or the SecretRef property has
	// to be provided. If both are provided, the AuthorizationToken will be used and SecretRef will be ignored. The
	// authorization token for your Dash0 organization can be copied from https://app.dash0.com/settings.
	//
	// +kubebuilder:validation:Optional
	AuthorizationToken string `json:"authorizationToken"`

	// A reference to a Kubernetes secret containing the Dash0 authorization token. This property is optional, but either
	// this property or the AuthorizationToken property has to be provided. If both are provided, the AuthorizationToken
	// will be used and SecretRef will be ignored. The authorization token for your Dash0 organization can be copied
	// from https://app.dash0.com/settings.
	//
	// +kubebuilder:validation:Optional
	SecretRef string `json:"secretRef"`

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
	// instrumenting existing workloads by setting this option to false. By default, when the Dash0 monitoring resource
	// is deployed to a namespace (and the Dash0 Kubernetes operator is active), the operator will instrument the
	// workloads already running in that namesapce, to send telemetry to Dash0. Setting this option to false will
	// prevent that behavior, but workloads that are deployed after Dash0 monitoring resource will still be instrumented
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
	// monitoring resource is present in a namespace (and the Dash0 Kubernetes operator is active), the operator will
	// instrument new workloads to send telemetry to Dash0, at the time they are deployed to that namespace.
	// Setting this option to false will prevent that behavior, but workloads existing when the Dash0 monitoring resource
	// is deployed will still be instrumented (see option InstrumentExistingWorkloads). More fine-grained control over
	// instrumentation on a per-workload level is available by setting the label dash0.com/enable=false on individual
	// workloads.
	//
	// The default value for this option is true.
	//
	// This option has no effect if InstrumentWorkloads is set to false.
	//
	// +kubebuilder:validation:Optional
	InstrumentNewWorkloads *bool `json:"instrumentNewWorkloads"`

	// Opt-out for removing the Dash0 instrumentation from workloads when the Dash0 monitoring resource is removed from a
	// namespace, or when the Dash0 Kubernetes operator is deleted entirely. By default, this setting is true and the
	// operator will revert the instrumentation modifications it applied to workloads to send telemetry to Dash0.
	// Setting this option to false will prevent this behavior.
	//
	// The default value for this option is true.
	//
	// +kubebuilder:validation:Optional
	UninstrumentWorkloadsOnDelete *bool `json:"uninstrumentWorkloadsOnDelete"`
}

// Dash0MonitoringStatus defines the observed state of the Dash0 monitoring resource.
type Dash0MonitoringStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Dash0Monitoring is the Schema for the Dash0Monitoring API
type Dash0Monitoring struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0MonitoringSpec   `json:"spec,omitempty"`
	Status Dash0MonitoringStatus `json:"status,omitempty"`
}

func (d *Dash0Monitoring) IsMarkedForDeletion() bool {
	deletionTimestamp := d.GetDeletionTimestamp()
	return deletionTimestamp != nil && !deletionTimestamp.IsZero()
}

func (d *Dash0Monitoring) IsAvailable() bool {
	if condition := d.getCondition(util.ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0Monitoring) getCondition(conditionType util.ConditionType) *metav1.Condition {
	for _, c := range d.Status.Conditions {
		if c.Type == string(conditionType) {
			return &c

		}
	}
	return nil
}

func (d *Dash0Monitoring) SetAvailableConditionToUnknown() {
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

func (d *Dash0Monitoring) EnsureResourceIsMarkedAsAvailable() {
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

func (d *Dash0Monitoring) EnsureResourceIsMarkedAsAboutToBeDeleted() {
	d.EnsureResourceIsMarkedAsDegraded(
		"Dash0MonitoringResourceHasBeenRemoved",
		"Dash0 is inactive in this namespace now.",
	)
}

func (d *Dash0Monitoring) EnsureResourceIsMarkedAsDegraded(
	reason string,
	message string,
) {
	// If the available status is already false, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		})
}

func (d *Dash0Monitoring) GetResourceTypeName() string {
	return "Dash0MonitoringResource"
}
func (d *Dash0Monitoring) GetNaturalLanguageResourceTypeName() string {
	return "Dash0 monitoring resource"
}
func (d *Dash0Monitoring) Get() client.Object {
	return d
}
func (d *Dash0Monitoring) GetName() string {
	return d.Name
}
func (d *Dash0Monitoring) GetUid() types.UID {
	return d.UID
}
func (d *Dash0Monitoring) GetCreationTimestamp() metav1.Time {
	return d.CreationTimestamp
}
func (d *Dash0Monitoring) GetReceiver() client.Object {
	return &Dash0Monitoring{}
}
func (d *Dash0Monitoring) GetListReceiver() client.ObjectList {
	return &Dash0MonitoringList{}
}
func (d *Dash0Monitoring) Items(list client.ObjectList) []client.Object {
	items := list.(*Dash0MonitoringList).Items
	result := make([]client.Object, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}
func (d *Dash0Monitoring) At(list client.ObjectList, index int) controller.CustomResource {
	return &list.(*Dash0MonitoringList).Items[index]
}

//+kubebuilder:object:root=true

// Dash0MonitoringList contains a list of Dash0Monitoring resources.
type Dash0MonitoringList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0Monitoring `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dash0Monitoring{}, &Dash0MonitoringList{})
}
