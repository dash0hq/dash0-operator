// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"encoding/json"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0operator "github.com/dash0hq/dash0-operator/api/operator"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

// Dash0Monitoring is the schema for the Dash0Monitoring API
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:conversion:hub
// +kubebuilder:printcolumn:name="Instrument Workloads",type="string",JSONPath=".spec.instrumentWorkloads.mode"
// +kubebuilder:printcolumn:name="Collect Logs",type="boolean",JSONPath=".spec.logCollection.enabled"
// +kubebuilder:printcolumn:name="Prometheus Scraping",type="boolean",JSONPath=".spec.prometheusScraping.enabled"
// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=`.status.conditions[?(@.type == "Available")].status`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Dash0Monitoring struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0MonitoringSpec   `json:"spec,omitempty"`
	Status Dash0MonitoringStatus `json:"status,omitempty"`
}

// Dash0MonitoringSpec describes the details of monitoring a single Kubernetes namespace with Dash0 and sending
// telemetry to an observability backend.
type Dash0MonitoringSpec struct {
	// Deprecated: Use `exports` instead. If both `export` and `exports` are specified, `export` is ignored. The mutating
	// webhook migrates `export` to `exports` if only `export` is specified.
	//
	// +kubebuilder:validation:Optional
	Export *dash0common.Export `json:"export,omitempty"`

	// The observability backends to which the telemetry of this namespace will be sent. Each export can combine up to
	// three exporters (Dash0, gRPC and HTTP). Telemetry is sent to every exporter of every export. If omitted, the
	// exports of the Dash0OperatorConfiguration resource are used. In that case, creating the resource fails if there is
	// no operator configuration resource or if it has no exports.
	//
	// +kubebuilder:validation:Optional
	Exports []dash0common.Export `json:"exports,omitempty"`

	// Settings for the automatic instrumentation of workloads in the target namespace. Cluster-wide instrumentation
	// settings are in the operator configuration resource.
	//
	// +kubebuilder:validation:Optional
	InstrumentWorkloads InstrumentWorkloads `json:"instrumentWorkloads,omitempty"`

	// Settings for collecting pod logs in the target namespace.
	//
	// +kubebuilder:validation:Optional
	LogCollection dash0common.LogCollection `json:"logCollection,omitempty"`

	// Settings for collecting Kubernetes events in the target namespace.
	//
	// +kubebuilder:validation:Optional
	EventCollection dash0common.EventCollection `json:"eventCollection,omitempty"`

	// Settings for collecting the manifests of pods and workloads in the target namespace.
	//
	// +kubebuilder:validation:Optional
	KubernetesObjectCollection dash0common.KubernetesObjectCollection `json:"kubernetesObjectCollection,omitempty"`

	// Settings for scraping Prometheus metrics from pods in the target namespace.
	//
	// +kubebuilder:validation:Optional
	PrometheusScraping dash0common.PrometheusScraping `json:"prometheusScraping,omitempty"`

	// Optional filters to drop spans, span events, metrics, metric data points, log records or profiles collected in
	// this namespace (see transform for modifying telemetry). It is a validation error to set filters while the operator
	// configuration resource has `telemetryCollection.enabled=false`.
	//
	// +kubebuilder:validation:Optional
	Filter *dash0common.Filter `json:"filter,omitempty"`

	// Optional transformations for telemetry collected in this namespace, e.g. to remove span, metric data point or log
	// record attributes (see filter for dropping telemetry). The OTTL statements of each signal type are applied in the
	// given order. Only the basic config style of the transform processor is supported, see
	// https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/transformprocessor. It is a
	// validation error to set transforms while the operator configuration resource has
	// `telemetryCollection.enabled=false`.
	//
	// +kubebuilder:validation:Optional
	Transform *dash0common.Transform `json:"transform,omitempty"`

	// Only used internally, this field must not be specified by users.
	NormalizedTransformSpec *dash0common.NormalizedTransformSpec `json:"__dash0_internal__normalizedTransform,omitempty"`

	// Whether to synchronize the Perses dashboard resources in this namespace to Dash0 via the Dash0 API. Defaults to
	// `true`.
	//
	// +kubebuilder:default=true
	SynchronizePersesDashboards *bool `json:"synchronizePersesDashboards"`

	// Whether to synchronize the Prometheus rule resources in this namespace to Dash0 check rules via the Dash0 API.
	// Defaults to `true`.
	//
	// +kubebuilder:default=true
	SynchronizePrometheusRules *bool `json:"synchronizePrometheusRules"`
}

type InstrumentWorkloads struct {
	// Which events trigger the instrumentation of workloads in the target namespace, and whether workloads or pods are
	// instrumented:
	// * `all` (default): The operator instruments existing workloads when the Dash0 monitoring resource is deployed,
	//   instruments existing workloads or updates their instrumentation when the operator starts or restarts (e.g. when
	//   it is updated), and instruments new workloads when they are deployed and changed workloads when they are
	//   updated. Instrumenting existing workloads restarts their pods.
	// * `created-and-updated`: The operator only instruments new workloads when they are deployed and changed workloads
	//   when they are updated. This avoids pod restarts when the monitoring resource is deployed or the operator
	//   restarts.
	// * `pods`: The operator does not modify workloads (deployments, daemon sets, stateful sets, replica sets, jobs, cron
	//   jobs), but instruments each pod when it is created. It never restarts pods: Existing pods keep their state until
	//   they are recreated (e.g. by the next rollout), also when the operator is updated, the mode is changed to `none`
	//   or the monitoring resource is removed. Hence, a workload can run instrumented and uninstrumented pods side by
	//   side. Instrumentation that a pod template still has from an earlier mode is removed with the next update of the
	//   workload, unless that would replace pods that would not be replaced otherwise. If the mode is changed to `none`
	//   or the monitoring resource is removed before that, the pod template is reverted, which replaces the pods. The
	//   label selector is evaluated against the labels of the pod combined with those of its owners, the level closest
	//   to the pod wins; the other modes only use the labels of the workload. Use this mode to avoid drift reports by
	//   GitOps or infrastructure-as-code tools.
	// * `none`: Workloads in the target namespace are never instrumented.
	// If the operator configuration resource has `telemetryCollection.enabled=false`, the default is `none`, and any
	// other mode is a validation error. Individual workloads can be excluded via the label dash0.com/enable=false or
	// spec.instrumentWorkloads.labelSelector.
	//
	// +kubebuilder:validation:Optional
	Mode dash0common.InstrumentWorkloadsMode `json:"mode,omitempty"`

	// A label selector for per-workload control over instrumentation: Only workloads that match it are instrumented. It
	// is ignored if spec.instrumentWorkloads.mode is `none`. The default "dash0.com/enable!=false" matches all workloads
	// except those with the label dash0.com/enable=false. A selector with equals instead of not-equals (e.g.
	// "auto-instrument-this-workload-with-dash0=true") implements an opt-in model instead.
	//
	// +kubebuilder:default=dash0.com/enable!=false
	LabelSelector string `json:"labelSelector,omitempty"`

	// Optional settings for how trace context is handled in instrumented workloads.
	//
	// +kubebuilder:validation:Optional
	TraceContext TraceContext `json:"traceContext,omitempty"`

	// If set to true, the operator enables SQL query parameter capture in the OpenTelemetry Java agent (JDBC) and the
	// OpenTelemetry .NET auto-instrumentation (Microsoft.Data.SqlClient, Entity Framework Core). It sets the following
	// environment variables to "true" in instrumented containers, unless a container sets them to a different value:
	//   - OTEL_INSTRUMENTATION_JDBC_EXPERIMENTAL_CAPTURE_QUERY_PARAMETERS
	//   - OTEL_DOTNET_EXPERIMENTAL_SQLCLIENT_ENABLE_TRACE_DB_QUERY_PARAMETERS
	//   - OTEL_DOTNET_EXPERIMENTAL_EFCORE_ENABLE_TRACE_DB_QUERY_PARAMETERS
	// The parameter values are added to the database spans and may include sensitive data such as PII. These
	// experimental agent flags may be renamed or removed in future agent releases. If the setting is later set to false
	// or removed, the operator removes each variable whose value is still "true".
	//
	// +kubebuilder:validation:Optional
	CaptureSqlQueryParameters *bool `json:"captureSqlQueryParameters,omitempty"`
}

type TraceContext struct {
	// A comma-separated list of trace context propagators, e.g. "tracecontext,xray" for the W3C traceparent header and
	// AWS X-Ray headers. If set, it is added to workloads as the environment variable OTEL_PROPAGATORS (the SDK default
	// is "tracecontext,baggage"). If several propagators extract the same information, the last one wins, so list the
	// preferred propagator last.
	//
	// +kubebuilder:validation:Optional
	Propagators *string `json:"propagators,omitempty"`
}

// NamespaceInstrumentationConfig holds configuration values relevant for instrumenting workloads which apply to one
// namespace, e.g. settings from the monitoring resource.
type NamespaceInstrumentationConfig struct {
	InstrumentationLabelSelector    string
	TraceContextPropagators         *string
	PreviousTraceContextPropagators *string
	// CaptureSqlQueryParameters reflects spec.instrumentWorkloads.captureSqlQueryParameters. When true, the workload
	// modifier sets the SQL query parameter capture environment variables to "true" on instrumented containers.
	CaptureSqlQueryParameters *bool
	// PreviousCaptureSqlQueryParameters reflects the CaptureSqlQueryParameters value at the time of the previous
	// reconcile. Used during uninstrumentation / re-instrumentation to decide whether to remove previously set SQL
	// query parameter capture environment variables.
	PreviousCaptureSqlQueryParameters *bool
	// LogCollectionEnabled is true if the operator collects pod logs from this namespace via the collector's file_log
	// receiver. When true, the workload modifier sets OTEL_LOGS_EXPORTER=none on instrumented containers to prevent
	// the workload's own OTel SDK from also exporting logs via OTLP (which would result in duplicates).
	LogCollectionEnabled bool
	// PreviousLogCollectionEnabled reflects the LogCollectionEnabled value at the time of the previous reconcile. Used
	// during uninstrumentation / re-instrumentation to decide whether to remove a previously set OTEL_LOGS_EXPORTER
	// env var.
	PreviousLogCollectionEnabled bool
	// InstrumentPods is true if spec.instrumentWorkloads.mode is `pods`. Pods are then instrumented by the webhook when
	// they are created, and workloads are not modified.
	InstrumentPods bool
}

// Dash0MonitoringStatus defines the observed state of the Dash0Monitoring monitoring resource.
type Dash0MonitoringStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// The spec.instrumentWorkloads.mode setting that has been observed in the previous reconcile cycle.
	// +kubebuilder:validation:Optional
	PreviousInstrumentWorkloads InstrumentWorkloads `json:"previousInstrumentWorkloads,omitempty"`

	// The spec.logCollection setting that has been observed in the previous reconcile cycle. Used to detect when log
	// collection has been toggled, which requires re-instrumenting workloads so that OTEL_LOGS_EXPORTER is
	// added/removed accordingly.
	// +kubebuilder:validation:Optional
	PreviousLogCollection dash0common.LogCollection `json:"previousLogCollection,omitempty"`

	// Shows results of synchronizing Perses dashboard resources in this namespace via the Dash0 API.
	// +kubebuilder:validation:Optional
	PersesDashboardSynchronizationResults map[string]dash0common.PersesDashboardSynchronizationResults `json:"persesDashboardSynchronizationResults,omitempty"`

	// Shows results of synchronizing Prometheus rule resources in this namespace via the Dash0 API.
	// +kubebuilder:validation:Optional
	PrometheusRuleSynchronizationResults map[string]dash0common.PrometheusRuleSynchronizationResult `json:"prometheusRuleSynchronizationResults,omitempty"`
}

// Hub marks this version as the hub for conversions, all other versions are implicitly spokes. See
// https://book.kubebuilder.io/multiversion-tutorial/conversion-concepts.
func (*Dash0Monitoring) Hub() {}

func (d *Dash0Monitoring) ReadInstrumentWorkloadsMode() dash0common.InstrumentWorkloadsMode {
	instrumentWorkloadsMode := d.Spec.InstrumentWorkloads.Mode
	if instrumentWorkloadsMode == "" {
		return dash0common.InstrumentWorkloadsModeAll
	}
	if !slices.Contains(dash0common.AllInstrumentWorkloadsModes, instrumentWorkloadsMode) {
		return dash0common.InstrumentWorkloadsModeAll
	}
	return instrumentWorkloadsMode
}

func (d *Dash0Monitoring) IsMarkedForDeletion() bool {
	deletionTimestamp := d.GetDeletionTimestamp()
	return deletionTimestamp != nil && !deletionTimestamp.IsZero()
}

func (d *Dash0Monitoring) IsAvailable() bool {
	if condition := d.getCondition(dash0common.ConditionTypeAvailable); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0Monitoring) IsDegraded() bool {
	if condition := d.getCondition(dash0common.ConditionTypeDegraded); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func (d *Dash0Monitoring) getCondition(conditionType dash0common.ConditionType) *metav1.Condition {
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
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Dash0 has started monitoring resource reconciliation for this namespace.",
		})
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeDegraded),
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
			Type:    string(dash0common.ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: "Dash0 monitoring is active in this namespace now.",
		})
	meta.RemoveStatusCondition(&d.Status.Conditions, string(dash0common.ConditionTypeDegraded))
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

func (d *Dash0Monitoring) All(list client.ObjectList) []dash0operator.Dash0Resource {
	items := list.(*Dash0MonitoringList).Items
	result := make([]dash0operator.Dash0Resource, len(items))
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

func (d *Dash0Monitoring) At(list client.ObjectList, index int) dash0operator.Dash0Resource {
	return &list.(*Dash0MonitoringList).Items[index]
}

func (d *Dash0Monitoring) GetNamespaceInstrumentationConfig() NamespaceInstrumentationConfig {
	return NamespaceInstrumentationConfig{
		InstrumentationLabelSelector:      d.Spec.InstrumentWorkloads.LabelSelector,
		TraceContextPropagators:           d.Spec.InstrumentWorkloads.TraceContext.Propagators,
		PreviousTraceContextPropagators:   d.Status.PreviousInstrumentWorkloads.TraceContext.Propagators,
		CaptureSqlQueryParameters:         d.Spec.InstrumentWorkloads.CaptureSqlQueryParameters,
		PreviousCaptureSqlQueryParameters: d.Status.PreviousInstrumentWorkloads.CaptureSqlQueryParameters,
		LogCollectionEnabled:              pointers.ReadBoolPointerWithDefault(d.Spec.LogCollection.Enabled, true),
		PreviousLogCollectionEnabled:      pointers.ReadBoolPointerWithDefault(d.Status.PreviousLogCollection.Enabled, true),
		InstrumentPods:                    d.ReadInstrumentWorkloadsMode() == dash0common.InstrumentWorkloadsModePods,
	}
}

// EffectiveExports returns Exports if set, otherwise wraps the deprecated Export field into a single-element slice.
// This ensures code that reads exports works for resources that have not yet been migrated by the mutating webhook.
func (d *Dash0Monitoring) EffectiveExports() []dash0common.Export {
	if len(d.Spec.Exports) > 0 {
		return d.Spec.Exports
	}
	//nolint:staticcheck
	if d.Spec.Export != nil {
		//nolint:staticcheck
		return []dash0common.Export{*d.Spec.Export}
	}
	return nil
}

func (d *Dash0Monitoring) HasExportsConfigured() bool {
	return d != nil && len(d.EffectiveExports()) > 0
}

func (d *Dash0Monitoring) HasDash0ExportConfigured() bool {
	for _, export := range d.EffectiveExports() {
		if export.HasDash0ExportConfigured() {
			return true
		}
	}

	return false
}

func (d *Dash0Monitoring) ExportsCount() int {
	if !d.HasExportsConfigured() {
		return 0
	} else {
		return dash0common.CountExports(d.EffectiveExports())
	}
}

func (d *Dash0Monitoring) HasDash0ApiAccessConfigured() bool {
	for _, export := range d.EffectiveExports() {
		if export.HasDash0ApiAccessConfigured() {
			return true
		}
	}

	return false
}

func (d *Dash0Monitoring) GetDash0Exports() []dash0common.Dash0Configuration {
	var res []dash0common.Dash0Configuration
	for _, export := range d.EffectiveExports() {
		// intentionally not doing any further filtering here, as validation will happen later where errors are properly logged
		if export.Dash0 != nil {
			res = append(res, *export.Dash0)
		}
	}
	return res
}

func (d *Dash0Monitoring) LogResourceAsEvent(logger logd.Logger) {
	redactedMonitoringResource := d.cloneAndRedact()
	if redactedMonitoringResourceMarshalled, err := json.Marshal(redactedMonitoringResource); err != nil {
		logger.Error(err, "cannot marshal Dash0Monitoring resource for dash0.monitoring_resource event")
	} else {
		logger.Info(
			// per https://opentelemetry.io/docs/specs/semconv/general/events, events should not use the log body
			"",
			"otel.event.name", "dash0.monitoring_resource",
			"dash0.monitoring.monitoring_resource.snapshot", string(redactedMonitoringResourceMarshalled),
			"dash0.monitoring.monitoring_resource.namespace", d.GetNamespace(),
			"dash0.monitoring.monitoring_resource.name", d.GetName(),
		)
	}
}

func (d *Dash0Monitoring) cloneAndRedact() Dash0Monitoring {
	redactedResource := Dash0Monitoring{}
	d.DeepCopyInto(&redactedResource)
	redactedResource.ManagedFields = nil
	// This annotation embeds a verbatim copy of the spec (including the plaintext token) that Export.Redact() cannot reach.
	delete(redactedResource.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
	for _, export := range redactedResource.EffectiveExports() {
		export.Redact()
	}
	return redactedResource
}

//+kubebuilder:object:root=true

// Dash0MonitoringList contains a list of Dash0Monitoring resources.
type Dash0MonitoringList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0Monitoring `json:"items"`
}
