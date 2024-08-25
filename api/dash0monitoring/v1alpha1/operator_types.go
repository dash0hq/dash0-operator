// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	common "github.com/dash0hq/dash0-operator/api/dash0monitoring"
)

const (
	OperatorFinalizerId = "operator.dash0monitoring.com/dash0monitoring-operator-finalizer"
)

type Dash0Export struct {
	// The URL of the observability backend to which telemetry data will be sent. This property is mandatory. The value
	// needs to be the OTLP/gRPC endpoint of your Dash0 organization. The correct OTLP/gRPC endpoint can be copied fom
	// https://app.dash0.com/settings. The correct endpoint value will always start with `ingress.` and end in
	// `dash0.com:4317`.
	//
	// +kubebuilder:validation:Mandatory
	Endpoint string `json:"endpoint"` // Required

	// The identifier of the dataset to which the telemetry will be sent. If unspecified, the telemetry will go to the `default` dataset
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=default
	Dataset string `json:"dataset,omitempty"`

	// +kubebuilder:validation:Mandatory
	Authorization Authorization `json:"authorization"`
}

// The Export datastructure specifies the default backend to which telemetry will be
// sent to by the operator, as well as the backend that will receive operator
// self-monitoring data
//
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type Export struct {
	// Pointer so that JSON serialization does not output an empty struct
	Dash0Export *Dash0Export `json:"dash0,omitempty"`
	// Pointer so that JSON serialization does not output an empty struct
	HttpExport *HttpExport `json:"http,omitempty"`
	// Pointer so that JSON serialization does not output an empty struct
	GrpcExport *GrpcExport `json:"grpc,omitempty"`
}

// The Authorization datastructure specifies which authorization information to send to the Dash0 alongside
// the telemetry data.
//
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type Authorization struct {
	// The Dash0 authorization token. This property is optional, but either this property or the SecretRef property has
	// to be provided. If both are provided, the AuthorizationToken will be used and SecretRef will be ignored. The
	// authorization token for your Dash0 organization can be copied from https://app.dash0.com/settings.
	//
	// +kubebuilder:validation:Optional
	Token string `json:"token"`
	// A reference to a Kubernetes secret containing the Dash0 authorization token. This property is optional, and is
	// ignored if the AuthorizationToken property is set. The authorization token for your Dash0 organization
	// can be copied from https://app.dash0.com/settings.
	//
	// +kubebuilder:validation:Optional
	SecretRef string `json:"secretRef"`
}

type OtlpProtocol string

const (
	OtlpProtocolHttpJson     OtlpProtocol = "http/json"
	OtlpProtocolHttpProtobuf OtlpProtocol = "http/protobuf"
	OtlpProtocolGrpc         OtlpProtocol = "grpc"
)

type HttpExport struct {
	// +kubebuilder:validation:Pattern=`^https?:\/\/.+$`
	Url     string   `json:"url"` // TODO More precise URL validation via kubebuilder:validation
	Headers []string `json:"headers,omitempty"`
	// +kubebuilder:validation:Enum=http/protobuf
	// +kubebuilder:default=http/protobuf
	// TODO Expand the enum when the Go SDK supports JSON encoding
	Encoding OtlpProtocol `json:"protocol,omitempty"`
}

type GrpcExport struct {
	Url     string   `json:"url"` // TODO Regexp validation via kubebuilder:validation
	Headers []string `json:"headers,omitempty"`
}

// Dash0OperatorConfigurationSpec defines the desired state of the Dash0 monitoring resource.
type Dash0OperatorConfigurationSpec struct {
	// Pointer so that JSON serialization does not output an empty struct
	// +kubebuilder:validation:Optional
	Export *Export `json:"export,omitempty"`

	// Global opt-out for self-monitoring for this operator
	// +kubebuilder:default={enabled: true}
	SelfMonitoring SelfMonitoring `json:"selfMonitoring,omitempty"`
}

// SelfMonitoring describes how the operator will report telemetry about its working to the
// backend described in Endpoint
//
// +kubebuilder:validation:Optional
type SelfMonitoring struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// Dash0OperatorConfigurationStatus defines the observed state of the Dash0 monitoring resource.
type Dash0OperatorConfigurationStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// Dash0OperatorConfiguration is the Schema for the Dash0OperatorConfiguration API
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
	if condition := d.getCondition(common.ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0OperatorConfiguration) getCondition(conditionType common.ConditionType) *metav1.Condition {
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
			Type:    string(common.ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Dash0 has started resource reconciliation.", // TODO
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(common.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileStarted",
			Message: "Dash0 is still starting.", // TODO
		})
}

func (d *Dash0OperatorConfiguration) EnsureResourceIsMarkedAsAvailable() {
	// If the available status is already true, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(common.ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: "Dash0 is active in this cluster now.", // TODO
		})
	meta.RemoveStatusCondition(&d.Status.Conditions, string(common.ConditionTypeDegraded))
}

func (d *Dash0OperatorConfiguration) EnsureResourceIsMarkedAsAboutToBeDeleted() {
	d.EnsureResourceIsMarkedAsDegraded(
		"Dash0MonitoringResourceHasBeenRemoved",
		"Dash0 is inactive in this cluster now.", // TODO
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
			Type:    string(common.ConditionTypeAvailable),
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(common.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		})
}

//+kubebuilder:object:root=true

// Dash0OperatorConfigurationList contains a list of Dash0Monitoring resources.
type Dash0OperatorConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0OperatorConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dash0OperatorConfiguration{}, &Dash0OperatorConfigurationList{})
}
