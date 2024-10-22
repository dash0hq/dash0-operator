// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/dash0monitoring"
)

const (
	MonitoringFinalizerId = "operator.dash0.com/dash0-monitoring-finalizer"
)

// Dash0MonitoringSpec describes the details of monitoring a single Kubernetes namespace with Dash0 and sending
// telemetry to an observability backend.
type Dash0MonitoringSpec struct {
	// The configuration of the observability backend to which telemetry data will be sent. This property is optional.
	// If not set, the operator will use the default export configuration from the cluster-wide
	// Dash0OperatorConfiguration resource, if present. If no Dash0OperatorConfiguration resource has been created for
	// the cluster, or if the Dash0OperatorConfiguration resource does not have at least one export defined, creating a
	// Dash0Monitoring resource without export settings will result in an error.
	//
	// The export can either be Dash0 or another OTLP-compatible backend. You can also combine up to three exporters
	// (i.e. Dash0 plus gRPC plus HTTP). This allows sending the same data to two or three targets simultaneously. When
	// the export setting is present, it has to contain at least one exporter.
	//
	// +kubebuilder:validation:Optional
	Export *Export `json:"export"`

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

	// If enabled, the operator will watch Perses dashboard resources in this namespace and create corresponding
	// dashboards in Dash0 via the Dash0 API.
	// See https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md#managing-dash0-dashboards-with-the-operator
	// for details. This setting is optional, it defaults to true.
	//
	// +kubebuilder:default=true
	SynchronizePersesDashboards *bool `json:"synchronizePersesDashboards,omitempty"`

	// If enabled, the operator will watch Prometheus rule resources in this namespace and create corresponding check
	// rules in Dash0 via the Dash0 API.
	// See https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md#managing-dash0-check-rules-with-the-operator
	// for details. This setting is optional, it defaults to true.
	//
	// +kubebuilder:default=true
	SynchronizePrometheusRules *bool `json:"synchronizePrometheusRules,omitempty"`

	// If enabled, the operator will configure its OpenTelemetry collector to scrape metrics from pods in the namespace
	// of this Dash0Monitoring resource according to their prometheus.io/scrape annotations via the OpenTelemetry
	// Prometheus receiver. This setting is optional, it defaults to true.
	//
	// +kubebuilder:default=true
	PrometheusScrapingEnabled *bool `json:"prometheusScrapingEnabled,omitempty"`
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

// SynchronizationStatus describes the result of synchronizing a third-party Kubernetes resource (Perses
// dashboard, Prometheus rule) to the Dash0 API.
//
// +kubebuilder:validation:Enum=successful;partially-successful;failed
type SynchronizationStatus string

const (
	// Successful means all items have been synchronized.
	Successful SynchronizationStatus = "successful"

	// PartiallySuccessful means some items have been synchronized and for some the synchronization has failed.
	PartiallySuccessful SynchronizationStatus = "partially-successful"

	// Failed means synchronization has failed for all items.
	Failed SynchronizationStatus = "failed"
)

type PersesDashboardSynchronizationResults struct {
	SynchronizationStatus SynchronizationStatus `json:"synchronizationStatus"`
	SynchronizedAt        metav1.Time           `json:"synchronizedAt"`
	SynchronizationError  string                `json:"synchronizationError,omitempty"`
	ValidationIssues      []string              `json:"validationIssues,omitempty"`
}

type PrometheusRuleSynchronizationResult struct {
	SynchronizationStatus      SynchronizationStatus `json:"synchronizationStatus"`
	SynchronizedAt             metav1.Time           `json:"synchronizedAt"`
	AlertingRulesTotal         int                   `json:"alertingRulesTotal"`
	SynchronizedRulesTotal     int                   `json:"synchronizedRulesTotal"`
	SynchronizedRules          []string              `json:"synchronizedRules,omitempty"`
	SynchronizationErrorsTotal int                   `json:"synchronizationErrorsTotal"`
	SynchronizationErrors      map[string]string     `json:"synchronizationErrors,omitempty"`
	InvalidRulesTotal          int                   `json:"invalidRulesTotal"`
	InvalidRules               map[string][]string   `json:"invalidRules,omitempty"`
}

// Dash0MonitoringStatus defines the observed state of the Dash0Monitoring monitoring resource.
type Dash0MonitoringStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// The spec.instrumentWorkloads setting that has been observed in the previous reconcile cycle.
	// +kubebuilder:validation:Optional
	PreviousInstrumentWorkloads InstrumentWorkloadsMode `json:"previousInstrumentWorkloads,omitempty"`

	// Shows results of synchronizing Perses dashboard resources in this namespace via the Dash0 API.
	// +kubebuilder:validation:Optional
	PersesDashboardSynchronizationResults map[string]PersesDashboardSynchronizationResults `json:"persesDashboardSynchronizationResults,omitempty"`

	// Shows results of synchronizing Prometheus rule resources in this namespace via the Dash0 API.
	// +kubebuilder:validation:Optional
	PrometheusRuleSynchronizationResults map[string]PrometheusRuleSynchronizationResult `json:"prometheusRuleSynchronizationResults,omitempty"`
}

// Dash0Monitoring is the schema for the Dash0Monitoring API
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +groupName=operator.dash0.com
type Dash0Monitoring struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default={instrumentWorkloads: "all", prometheusScrapingEnabled: true}
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
			Message: "Dash0 has started monitoring resource reconciliation for this namespace.",
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileStarted",
			Message: "Dash0 monitoring resource reconciliation is in progress.",
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
			Message: "Dash0 monitoring is active in this namespace now.",
		})
	meta.RemoveStatusCondition(&d.Status.Conditions, string(ConditionTypeDegraded))
}

func (d *Dash0Monitoring) EnsureResourceIsMarkedAsAboutToBeDeleted() {
	d.EnsureResourceIsMarkedAsDegraded(
		"Dash0MonitoringResourceHasBeenRemoved",
		"Dash0 monitoring is inactive in this namespace now.",
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

func (d *Dash0Monitoring) GetResourceTypeName() string {
	return "Dash0Monitoring"
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

func (d *Dash0Monitoring) IsClusterResource() bool {
	return false
}

func (d *Dash0Monitoring) RequestToName(ctrl.Request) string {
	return fmt.Sprintf("%s/%s", d.Namespace, d.Name)
}

func (d *Dash0Monitoring) Items(list client.ObjectList) []client.Object {
	items := list.(*Dash0MonitoringList).Items
	result := make([]client.Object, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}

func (d *Dash0Monitoring) At(list client.ObjectList, index int) dash0common.Dash0Resource {
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
