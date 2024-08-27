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

// Dash0MonitoringSpec describes the details of monitoring a single Kubernetes namespace with Dash0 and sending
// telemetry to an observability backend.
type Dash0MonitoringSpec struct {
	// The configuration of the observability backend to which telemetry data will be sent. This property is mandatory.
	// This can either be Dash0 or another OTLP-compatible backend. You can also combine up to three exporters (i.e.
	// Dash0 plus gRPC plus HTTP). This allows sending the same data to two or three targets simultaneously. At least
	// one exporter has to be defined.
	//
	// +kubebuilder:validation:Required
	Export `json:"export"`

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
	// +kubebuilder:default=all
	InstrumentWorkloads InstrumentWorkloadsMode `json:"instrumentWorkloads,omitempty"`
}

// Export describes the observability backend to which telemetry data will be sent. This can either be Dash0 or another
// OTLP-compatible backend. You can also combine up to three exporters (i.e. Dash0 plus gRPC plus HTTP). This allows
// sending the same data to two or three targets simultaneously. At least one exporter has to be defined.
//
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=3
type Export struct {
	// The configuration of the Dash0 ingress endpoint to which telemetry data will be sent.
	//
	// +kubebuilder:validation:Optional
	Dash0 *Dash0Configuration `json:"dash0,omitempty"`

	// The settings for an exporter to send telemetry to an arbitrary OTLP-compatible receiver via HTTP.
	//
	// +kubebuilder:validation:Optional
	Http *HttpConfiguration `json:"http,omitempty"`

	// The settings for an exporter to send telemetry to an arbitrary OTLP-compatible receiver via gRPC.
	//
	// +kubebuilder:validation:Optional
	Grpc *GrpcConfiguration `json:"grpc,omitempty"`
}

// Dash0Configuration describes to which Dash0 ingress endpoint telemetry data will be sent.
type Dash0Configuration struct {
	// The URL of the Dash0 ingress endpoint to which telemetry data will be sent. This property is mandatory. The value
	// needs to be the OTLP/gRPC endpoint of your Dash0 organization. The correct OTLP/gRPC endpoint can be copied fom
	// https://app.dash0.com/settings. The correct endpoint value will always start with `ingress.` and end in
	// `dash0.com:4317`.
	//
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// The name of the Dash0 dataset to which telemetry data will be sent. This property is optional. If omitted, the
	// dataset "default" will be used.
	//
	// +kubebuilder:default=default
	Dataset string `json:"dataset,omitempty"`

	// Mandatory authorization settings for sending data to Dash0.
	//
	// +kubebuilder:validation:Required
	Authorization Authorization `json:"authorization"`
}

// Authorization contains the authorization settings for Dash0.
//
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type Authorization struct {
	// The Dash0 authorization token. This property is optional, but either this property or the SecretRef property has
	// to be provided. If both are provided, the token will be used and SecretRef will be ignored. The authorization
	// token for your Dash0 organization can be copied from https://app.dash0.com/settings.
	//
	// +kubebuilder:validation:Optional
	Token *string `json:"token"` // either token or secret ref, with token taking precedence

	// A reference to a Kubernetes secret containing the Dash0 authorization token. This property is optional, and is
	// ignored if the token property is set. The authorization token for your Dash0 organization can be copied from
	// https://app.dash0.com/settings.
	//
	// +kubebuilder:validation:Optional
	SecretRef *SecretRef `json:"secretRef"`
}

type SecretRef struct {
	// The name of the secret containing the Dash0 authorization token. Defaults to "dash0-authorization-secret".
	// +kubebuilder:default=dash0-authorization-secret
	Name string `json:"name"`

	// The key of the value which contains the Dash0 authorization token. Defaults to "token"
	// +kubebuilder:default=token
	Key string `json:"key"`
}

// HttpConfiguration describe the settings for an exporter to send telemetry to an arbitrary OTLP-compatible receiver
// via HTTP.
type HttpConfiguration struct {
	// The URL of the OTLP-compatible receiver to which telemetry data will be sent. This property is mandatory.
	//
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Additional headers to be sent with each HTTP request, for example for authorization. This property is optional.
	//
	// +kubebuilder:validation:Optional
	Headers []Header `json:"headers,omitempty"`

	// The encoding of the OTLP data when sent via HTTP. Can be either proto or json, defaults to proto.
	//
	// +kubebuilder:default=proto
	Encoding OtlpEncoding `json:"encoding,omitempty"`
}

// GrpcConfiguration descibe the settings for an exporter to send telemetry to an arbitrary OTLP-compatible receiver
// via gRPC.
type GrpcConfiguration struct {
	// The URL of the OTLP-compatible receiver to which telemetry data will be sent. This property is mandatory.
	//
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Additional headers to be sent with each gRPC request, for example for authorization. This property is optional.
	//
	// +kubebuilder:validation:Optional
	Headers []Header `json:"headers,omitempty"`
}

// OtlpEncoding describes the encoding of the OTLP data when sent via HTTP.
//
// +kubebuilder:validation:Enum=proto;json
type OtlpEncoding string

const (
	Proto OtlpEncoding = "proto"
	Json  OtlpEncoding = "json"
)

type Header struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	Value string `json:"value"`
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

	// The spec.instrumentWorkloads setting that has been observed in the previous reconcile cycle.
	// +kubebuilder:validation:Optional
	PreviousInstrumentWorkloads InstrumentWorkloadsMode `json:"previousInstrumentWorkloads,omitempty"`
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
