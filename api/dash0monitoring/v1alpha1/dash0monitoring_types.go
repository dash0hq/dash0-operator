// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// A reference to a Kubernetes secret containing the Dash0 authorization token. This property is optional, and is
	// ignored if the AuthorizationToken property is set. The authorization token for your Dash0 organization
	// can be copied from https://app.dash0.com/settings.
	//
	// +kubebuilder:validation:Optional
	SecretRef string `json:"secretRef"`

	// Global opt-out for workload instrumentation for the target namespace. There are three possible settings: `all`,
	// `created-and-updated` and `none`. By default, the setting `all` is assumed.
	//
	// If set to `all` (or omitted), the operator will:
	// * instrument existing workloads in the target namespace (i.e. workloads already running in the namespace) when
	//   the Dash0 monitoring resource is deployed,
	// * instrument existing workloads or update the instrumentation of already instrumented workloads in the target
	//   namespace when the Dash0 Kubernetes operator is first started or restarted (for example when updating the
	//   operator),
	// * instrument new workloads in the target namespace when they are deployed, and
	// * instrument changed workloads in the target namespace when changes are applied to them.
	// Note that the first two actions (instrumenting existing workloads) will result in restarting the pods of the
	// affected workloads.
	//
	// If set to `created-and-updated`, the operator will not instrument existing workloads in the target namespace.
	// Instead, it will only:
	// * instrument new workloads in the target namespace when they are deployed, and
	// * instrument changed workloads in the target namespace when changes are applied to them.
	// This setting is useful if you want to avoid pod restarts as a side effect of deploying the Dash0 monitoring
	// resource or restarting the Dash0 Kubernetes operator.
	//
	// You can opt out of instrumenting workloads entirely by setting this option to `none`. With
	// `instrumentWorkloads: none`, workloads in the target namespace will never be instrumented to send telemetry to
	// Dash0.
	//
	// If this setting is omitted, the value `all` is assumed and new/updated as well as existing Kubernetes workloads
	// will be intrumented by the operator to send telemetry to Dash0, as described above.
	//
	// More fine-grained per-workload control over instrumentation is available by setting the label
	// dash0.com/enable=false on individual workloads.
	//
	// +kubebuilder:validation:Optional
	InstrumentWorkloads InstrumentWorkloadsMode `json:"instrumentWorkloads,omitempty"`

	// Opt-out for removing the Dash0 instrumentation from workloads when the Dash0 monitoring resource is removed from
	// a namespace, or when the Dash0 Kubernetes operator is deleted entirely. By default, this setting is true and the
	// operator will revert the instrumentation modifications it applied to workloads to send telemetry to Dash0.
	// Setting this option to false will prevent this behavior. Note that removing instrumentation will typically result
	// in a restart of the pods of the affected workloads.
	//
	// The default value for this option is true.
	//
	// +kubebuilder:validation:Optional
	UninstrumentWorkloadsOnDelete *bool `json:"uninstrumentWorkloadsOnDelete"`
}

// InstrumentWorkloadsMode describes when exactly workloads will be instrumented.  Only one of the following modes
// may be specified. If none of the following policies is specified, the default one is All. See
// Dash0MonitoringSpec#InstrumentWorkloads for more details.
//
// +kubebuilder:validation:Enum=all;created-and-updated;none
type InstrumentWorkloadsMode string

const (
	// All allows instrumenting existing as well as new and updated workloads.
	All InstrumentWorkloadsMode = "all"

	// CreatedAndUpdated disables instrumenting existing workloads, but new and updated workloads will be instrumented.
	CreatedAndUpdated InstrumentWorkloadsMode = "created-and-updated"

	// None will disable instrumentation of workloads entirely for a namespace.
	None InstrumentWorkloadsMode = "none"
)

var allInstrumentWorkloadsMode = []InstrumentWorkloadsMode{All, CreatedAndUpdated, None}

func ReadBooleanOptOutSetting(setting *bool) bool {
	return readOptionalBooleanWithDefault(setting, true)
}

func readOptionalBooleanWithDefault(setting *bool, defaultValue bool) bool {
	if setting == nil {
		return defaultValue
	}
	return *setting
}

type ConditionType string

const (
	ConditionTypeAvailable ConditionType = "Available"
	ConditionTypeDegraded  ConditionType = "Degraded"
)

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

func (d *Dash0Monitoring) ReadInstrumentWorkloadsSetting() InstrumentWorkloadsMode {
	instrumentWorkloads := d.Spec.InstrumentWorkloads
	if instrumentWorkloads == "" {
		return All
	}
	if !slices.Contains(allInstrumentWorkloadsMode, instrumentWorkloads) {
		return All
	}
	return instrumentWorkloads
}

func (d *Dash0Monitoring) IsMarkedForDeletion() bool {
	deletionTimestamp := d.GetDeletionTimestamp()
	return deletionTimestamp != nil && !deletionTimestamp.IsZero()
}

func (d *Dash0Monitoring) IsAvailable() bool {
	if condition := d.getCondition(ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0Monitoring) getCondition(conditionType ConditionType) *metav1.Condition {
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
			Type:    string(ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Dash0 has started resource reconciliation.",
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(ConditionTypeDegraded),
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
			Type:    string(ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: "Dash0 is active in this namespace now.",
		})
	meta.RemoveStatusCondition(&d.Status.Conditions, string(ConditionTypeDegraded))
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
