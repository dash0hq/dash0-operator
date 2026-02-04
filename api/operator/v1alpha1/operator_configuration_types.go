// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0operator "github.com/dash0hq/dash0-operator/api/operator"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0OperatorConfiguration is the schema for the Dash0OperatorConfiguration API
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Collect Telemetry",type="boolean",JSONPath=".spec.telemetryCollection.enabled"
// +kubebuilder:printcolumn:name="Collect Metrics",type="boolean",JSONPath=".spec.kubernetesInfrastructureMetricsCollection.enabled"
// +kubebuilder:printcolumn:name="Collect Pod Meta",type="boolean",JSONPath=".spec.collectPodLabelsAndAnnotations.enabled"
// +kubebuilder:printcolumn:name="Collect Namespace Meta",type="boolean",JSONPath=".spec.collectNamespaceLabelsAndAnnotations.enabled"
// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=`.status.conditions[?(@.type == "Available")].status`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Dash0OperatorConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0OperatorConfigurationSpec   `json:"spec,omitempty"`
	Status Dash0OperatorConfigurationStatus `json:"status,omitempty"`
}

// Dash0OperatorConfigurationSpec describes cluster-wide configuration settings for the Dash0 operator.
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
	Export *dash0common.Export `json:"export,omitempty"`

	// Global opt-out for self-monitoring for this operator
	//
	// +kubebuilder:default={enabled: true}
	SelfMonitoring SelfMonitoring `json:"selfMonitoring,omitempty"`

	// Settings for collecting Kubernetes infrastructure metrics. This setting is optional, by default the operator will
	// collect Kubernetes infrastructure metrics; unless `telemetryCollection.enabled` is set to `false`, then
	// collecting Kubernetes infrastructure metrics is off by default as well. It is a validation error to set
	// `telemetryCollection.enabled=false` and `kubernetesInfrastructureMetricsCollection.enabled=true` at the same time.
	//
	// +kubebuilder:validation:Optional
	KubernetesInfrastructureMetricsCollection KubernetesInfrastructureMetricsCollection `json:"kubernetesInfrastructureMetricsCollection,omitempty"`

	// Deprecated: This setting is deprecated. Please use
	//     kubernetesInfrastructureMetricsCollection:
	//       enabled: false
	// instead of
	//     kubernetesInfrastructureMetricsCollectionEnabled: false
	//
	// If enabled, the operator will collect Kubernetes infrastructure metrics. This setting is optional, it defaults
	// to true; unless `telemetryCollection.enabled` is set to `false`, then it defaults to `false` as well. It is a
	// validation error to set `telemetryCollection.enabled=false` and
	// `kubernetesInfrastructureMetricsCollectionEnabledEnabled=true` at the same time.
	//
	// +kubebuilder:validation:Optional
	KubernetesInfrastructureMetricsCollectionEnabled *bool `json:"kubernetesInfrastructureMetricsCollectionEnabled,omitempty"`

	// Settings for collecting pod labels and annotations. This setting is optional, by default the operator will
	// collect pod labels and annotations as resource attributes in all namespaces; unless `telemetryCollection.enabled`
	// is set to `false`, then collecting pod labels and annotations is off by default as well. It is a validation error
	// to set `telemetryCollection.enabled=false` and `collectPodLabelsAndAnnotations.enabled=true` at the same time.
	//
	// +kubebuilder:validation:Optional
	CollectPodLabelsAndAnnotations CollectPodLabelsAndAnnotations `json:"collectPodLabelsAndAnnotations,omitempty"`

	// Settings for collecting namespace labels and annotations. This setting is optional, by default the operator will
	// not collect namespace labels and annotations as resource attributes. It is a validation error to set
	// `telemetryCollection.enabled=false` and `collectNamespaceLabelsAndAnnotations.enabled=true` at the same time.
	//
	// +kubebuilder:validation:Optional
	CollectNamespaceLabelsAndAnnotations CollectNamespaceLabelsAndAnnotations `json:"collectNamespaceLabelsAndAnnotations,omitempty"`

	// Settings for discovering scrape targets via Prometheus CRDs (PodMonitor, ServiceMonitor, ScrapeConfig).
	// This setting is optional and opt-in, by default the operator will not consider Prometheus CRDs when configuring
	// the Prometheus receiver in the OpenTelemetry collectors.
	// It is a validation error to set`telemetryCollection.enabled=false` and `prometheusCrdSupport.enabled=true` at
	// the same time.
	//
	// +kubebuilder:validation:Optional
	PrometheusCrdSupport PrometheusCrdSupport `json:"prometheusCrdSupport,omitempty"`

	// If set, the value will be added as the resource attribute k8s.cluster.name to all telemetry. This setting is
	// optional. By default, k8s.cluster.name will not be added to telemetry.
	//
	// +kubebuilder:validation:Optional
	ClusterName string `json:"clusterName,omitempty"`

	// An opt-out switch for all telemetry collection, and to avoid having the operator deploy OpenTelemetry collectors
	// to the cluster. This setting is optional, it defaults to true.
	//
	// +kubebuilder:default={enabled: true}
	TelemetryCollection TelemetryCollection `json:"telemetryCollection,omitempty"`
}

// SelfMonitoring describes how the operator will report telemetry about its working to the backend.
type SelfMonitoring struct {
	// If enabled, the operator will collect self-monitoring telemetry and send it to the configured Dash0 backend.
	// This setting is optional, it defaults to `true`.
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`
}

type KubernetesInfrastructureMetricsCollection struct {
	// If enabled, the operator will collect Kubernetes infrastructure metrics. This setting is optional, it defaults
	// to `true`; unless `telemetryCollection.enabled` is set to `false`, then
	// `kubernetesInfrastructureMetricsCollection.enabled` defaults to `false` as well. It is a validation error to set
	// `telemetryCollection.enabled=false` and `kubernetesInfrastructureMetricsCollection.enabled=true` at the same
	// time.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled"`
}

type PrometheusCrdSupport struct {
	// If enabled, the operator will add support for Prometheus CRDs (PodMonitor, ServiceMonitor, ScrapeConfig) by
	// deploying the OpenTelemetry target-allocator.
	// It is a validation error to set`telemetryCollection.enabled=false` and `prometheusCrdSupport.enabled=true` at
	// the same time.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled"`
}

type CollectPodLabelsAndAnnotations struct {
	// Opt-out for collecting all pod labels and annotations as resource attributes. If set to `false`, the operator
	// will not collect Kubernetes labels and annotations as resource attributes.
	//
	// This setting is optional, it defaults to `true`, that is, if this setting is omitted, the value `true` is assumed
	// and the operator will collect pod labels and annotations as resource attributes; unless
	// `telemetryCollection.enabled` is set to `false`, then  `collectPodLabelsAndAnnotations.enabled` defaults to
	// `false` as well. It is a validation error to set `telemetryCollection.enabled=false` and
	// `collectPodLabelsAndAnnotations.enabled=true` at the same time.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled"`
}

type CollectNamespaceLabelsAndAnnotations struct {
	// Opt-in for collecting all namespace labels and annotations as resource attributes. If set to `true`, the operator
	// will collect Kubernetes namespace labels and annotations as resource attributes.
	//
	// This setting is optional, it defaults to `false`, that is, if this setting is omitted, the value `false` is
	// assumed and the operator will not collect namespace labels and annotations as resource attributes. It is a
	// validation error to set `telemetryCollection.enabled=false` and
	// `collectNamespaceLabelsAndAnnotations.enabled=true` at the same time.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled"`
}

type TelemetryCollection struct {
	// If disabled, the operator will not collect any telemetry, in particular it will not deploy any OpenTelemetry
	// collectors to the cluster. This is useful if you want to do infrastructure-as-code (dashboards, check rules) with
	// the operator, but do not want it to deploy the OpenTelemetry collector. This setting is optional, it defaults to
	// `true` (i.e. by default telemetry collection is enabled).
	//
	// Note that setting this to false does not disable the operator's self-monitoring telemetry, use the setting
	// selfMonitoring.enabled to disable self-monitoring if required (self-monitoring does not require an OpenTelemetry
	// collector).
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`
}

// Dash0OperatorConfigurationStatus defines the observed state of the Dash0 operator configuration resource.
type Dash0OperatorConfigurationStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

func (d *Dash0OperatorConfiguration) IsMarkedForDeletion() bool {
	deletionTimestamp := d.GetDeletionTimestamp()
	return deletionTimestamp != nil && !deletionTimestamp.IsZero()
}

func (d *Dash0OperatorConfiguration) IsAvailable() bool {
	if condition := d.getCondition(dash0common.ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0OperatorConfiguration) IsDegraded() bool {
	if condition := d.getCondition(dash0common.ConditionTypeDegraded); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0OperatorConfiguration) getCondition(conditionType dash0common.ConditionType) *metav1.Condition {
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
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Dash0 has started resource reconciliation for the cluster-wide operator configuration.",
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeDegraded),
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
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: "Dash0 operator configuration is available in this cluster now.",
		})
	meta.RemoveStatusCondition(&d.Status.Conditions, string(dash0common.ConditionTypeDegraded))
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
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeDegraded),
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

func (d *Dash0OperatorConfiguration) GetDash0AuthorizationIfConfigured() *dash0common.Authorization {
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

func (d *Dash0OperatorConfiguration) GetNaturalLanguageResourceTypeName() string {
	return "Dash0 operator configuration resource"
}

func (d *Dash0OperatorConfiguration) Get() client.Object {
	return d
}

func (d *Dash0OperatorConfiguration) GetName() string {
	return d.Name
}

func (d *Dash0OperatorConfiguration) GetUID() types.UID {
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

func (d *Dash0OperatorConfiguration) All(list client.ObjectList) []dash0operator.Dash0Resource {
	items := list.(*Dash0OperatorConfigurationList).Items
	result := make([]dash0operator.Dash0Resource, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}

func (d *Dash0OperatorConfiguration) Items(list client.ObjectList) []client.Object {
	items := list.(*Dash0OperatorConfigurationList).Items
	result := make([]client.Object, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}

func (d *Dash0OperatorConfiguration) At(list client.ObjectList, index int) dash0operator.Dash0Resource {
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
