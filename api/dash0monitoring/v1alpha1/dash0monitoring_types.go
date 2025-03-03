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
	//   namespace when the Dash0 operator is first started or restarted (for example when updating the
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
	// resource or restarting the Dash0 operator.
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

	// Optional filters for telemetry data that is collected in this namespace. This can be used to drop entire spans,
	// span events, metrics, metric data points, or log records. See "Transform" for advanced transformations (e.g.
	// removing span attributes, metric data point attributes, log record attributes etc.).
	//
	// +kubebuilder:validation:Optional
	Filter *Filter `json:"filter,omitempty"`

	// Optional custom transformations for telemetry data that is collected in this namespace. This can be used to
	// remove span attributes, metric data point attributes, log record attributes etc. See "Filter" for basic filters
	// that can be used to drop entire spans, span events, metrics, metric data points, or log records.
	//
	// For each signal type (traces, metrics, logs), a list of OTTL statements can be defined. These will be applied to
	// the telemetry collected in the namespace, following the order specified in the configuration. Each statement can
	// access and transform telemetry using OTTL functions.
	// See https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/transformprocessor
	// for details and examples. Note that this configuration currently supports the
	// [basic config style](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/transformprocessor/README.md#basic-config)
	// of the transform processor. The
	// [advanced config style](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/transformprocessor/README.md#advanced-config)
	// is not supported.
	//
	// +kubebuilder:validation:Optional
	Transform *Transform `json:"transform,omitempty"`
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

// FilterTransformErrorMode determine how the filter or the transform processor reacts to errors that occur while
// processing a condition.
//
// +kubebuilder:validation:Enum=ignore;silent;propagate
type FilterTransformErrorMode string

const (
	// FilterTransformErrorModeIgnore ignores errors returned by conditions, logs them, and continues with the next
	// condition or statement.
	FilterTransformErrorModeIgnore FilterTransformErrorMode = "ignore"

	// FilterTransformErrorModeSilent ignores errors returned by conditions, does not log them, and continues with the
	// next condition or statement.
	FilterTransformErrorModeSilent FilterTransformErrorMode = "silent"

	// FilterTransformErrorModePropagate return the error up the processing pipeline. This will result in the payload
	// being dropped.
	FilterTransformErrorModePropagate FilterTransformErrorMode = "propagate"
)

type Filter struct {
	// An optional field which will determine how the filter processor reacts to errors that occur while processing a
	// condition. Possible values:
	// - ignore: ignore errors returned by conditions, log them, continue with to the next condition.
	//           This is the recommended mode and also the default mode if this property is omitted.
	// - silent: ignore errors returned by conditions, do not log them, continue with to the next condition.
	// - propagate: return the error up the processing pipeline. This will result in the payload being dropped from the
	//           collector. Not recommended.
	//
	// This is optional, default to "ignore", there is usually no reason to change this.
	//
	// Note that although this can be specified per namespace, the filter conditions will be aggregated into one
	// single filter processor in the resulting OpenTelemetry collector configuration; if different error modes are
	// specified in different namespaces, the "most severe" error mode will be used (propagate > ignore > silent).
	//
	// +kubebuilder:default=ignore
	ErrorMode FilterTransformErrorMode `json:"error_mode,omitempty"`

	// Filters for the traces signal.
	// This can be used to drop _spans_ or _span events_.
	//
	// +kubebuilder:validation:Optional
	Traces *TraceFilter `json:"traces,omitempty"`

	// Filters for the metrics signal.
	// This can be used to drop entire _metrics_, or individual _data points_.
	//
	// +kubebuilder:validation:Optional
	Metrics *MetricFilter `json:"metrics,omitempty"`

	// Filters for the logs signal.
	// This can be used to drop _log records_.
	//
	// +kubebuilder:validation:Optional
	Logs *LogFilter `json:"logs,omitempty"`
}

func (f *Filter) HasAnyFilters() bool {
	if f.Traces != nil && f.Traces.HasAnyFilters() {
		return true
	}
	if f.Metrics != nil && f.Metrics.HasAnyFilters() {
		return true
	}
	if f.Logs != nil && f.Logs.HasAnyFilters() {
		return true
	}
	return false
}

type TraceFilter struct {
	// A list of conditions for filtering spans.
	// This is a list of OTTL conditions.
	// All spans where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// Example:
	// - 'attributes["http.route"] == "/ready"'
	// - 'attributes["http.route"] == "/metrics"'
	//
	// +kubebuilder:validation:Optional
	SpanFilter []string `json:"span,omitempty"`

	// A list of conditions for filtering span events.
	// This is a list of OTTL conditions.
	// All span events where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// If all span events for a span are dropped, the span will be left intact.
	//
	// +kubebuilder:validation:Optional
	SpanEventFilter []string `json:"spanevent,omitempty"`
}

func (f *TraceFilter) HasAnyFilters() bool {
	return len(f.SpanFilter) > 0 || len(f.SpanEventFilter) > 0
}

type MetricFilter struct {
	// A list of conditions for filtering metrics.
	// This is a list of OTTL conditions.
	// All metrics where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// Example:
	// - 'name == "k8s.replicaset.available"'
	// - 'name == "k8s.replicaset.desired"'
	// - 'type == METRIC_DATA_TYPE_HISTOGRAM'
	//
	// +kubebuilder:validation:Optional
	MetricFilter []string `json:"metric,omitempty"`

	// A list of conditions for filtering metrics data points.
	// This is a list of OTTL conditions.
	// All data points where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// Note: If all datapoints for a metric are dropped, the metric will also be dropped.
	// Example:
	// - 'metric.name == "a.noisy.metric.with.many.datapoints" and value_int == 0' # filter metrics by value
	// - 'resource.attributes["service.name"] == "my_service_name"' # filter data points by resource attributes
	//
	// +kubebuilder:validation:Optional
	DataPointFilter []string `json:"datapoint,omitempty"`
}

func (f *MetricFilter) HasAnyFilters() bool {
	return len(f.MetricFilter) > 0 || len(f.DataPointFilter) > 0
}

type LogFilter struct {
	// A list of conditions for filtering log records.
	// This is a list of OTTL conditions.
	// All log records where at least one condition evaluates to true will be dropped.
	// (That is, the conditions are implicitly connected by a logical OR.)
	// Example:
	// - 'IsMatch(body, ".*password.*")'
	// - 'severity_number < SEVERITY_NUMBER_WARN'
	//
	// +kubebuilder:validation:Optional
	LogRecordFilter []string `json:"log_records,omitempty"`
}

func (f *LogFilter) HasAnyFilters() bool {
	return len(f.LogRecordFilter) > 0
}

type Transform struct {
	// An optional field which will determine how the transform processor reacts to errors that occur while processing a
	// statement. Possible values:
	// - ignore: ignore errors returned by statements, log them, continue with to the next statement.
	// - silent: ignore errors returned by statements, do not log them, continue with to the next statement.
	// - propagate: return the error up the processing pipeline. This will result in the payload being dropped from the
	//   collector.
	//
	// This is optional, default to "ignore".
	//
	// Note that although this can be specified per namespace, the transform statements will be aggregated into one
	// single transform processor in the resulting OpenTelemetry collector configuration; if different error modes are
	// specified in different namespaces, the "most severe" error mode will be used (propagate > ignore > silent).
	//
	// +kubebuilder:default=ignore
	ErrorMode FilterTransformErrorMode `json:"error_mode,omitempty"`

	// Transform statements for the traces signal.
	//
	// +kubebuilder:validation:Optional
	Traces []string `json:"trace_statements,omitempty"`

	// Transform statements for the metrics signal.
	//
	// +kubebuilder:validation:Optional
	Metrics []string `json:"metric_statements,omitempty"`

	// Transform statements for the logs signal.
	//
	// +kubebuilder:validation:Optional
	Logs []string `json:"log_statements,omitempty"`
}

func (t *Transform) HasAnyStatements() bool {
	if len(t.Traces) > 0 {
		return true
	}
	if len(t.Metrics) > 0 {
		return true
	}
	if len(t.Logs) > 0 {
		return true
	}
	return false
}

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

func (d *Dash0Monitoring) IsDegraded() bool {
	if condition := d.getCondition(ConditionTypeDegraded); condition != nil {
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

func (d *Dash0Monitoring) GetUID() types.UID {
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

func (d *Dash0Monitoring) All(list client.ObjectList) []dash0common.Dash0Resource {
	items := list.(*Dash0MonitoringList).Items
	result := make([]dash0common.Dash0Resource, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
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
