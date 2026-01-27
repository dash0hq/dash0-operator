// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0operator "github.com/dash0hq/dash0-operator/api/operator"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util"
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
	Export *dash0common.Export `json:"export,omitempty"`

	// Settings for automatic instrumentation of workloads in the target namespace. This setting is optional, by default
	// the operator will instrument existing workloads, as well as new workloads at deploy time and changed workloads
	// when they are updated.
	//
	// +kubebuilder:validation:Optional
	InstrumentWorkloads InstrumentWorkloads `json:"instrumentWorkloads,omitempty"`

	// Settings for log collection in the target namespace. This setting is optional, by default the operator will
	// collect pod logs in the target namespace; unless there is an operator configuration resource with
	// `telemetryCollection.enabled=false`, then log collection is off by default. It is a validation error to set
	// `telemetryCollection.enabled=false` in the operator configuration resource and `logCollection.enabled=true` in any
	// monitoring resource at the same time.
	//
	// +kubebuilder:validation:Optional
	LogCollection dash0common.LogCollection `json:"logCollection,omitempty"`

	// Settings for collecting Kubernetes events in the target namespace. This setting is optional, by default the
	// operator will collect Kubernetes events in the target namespace; unless there is an operator configuration
	// resource with `telemetryCollection.enabled=false`, then event collection is off by default. It is a validation
	// error to set `telemetryCollection.enabled=false` in the operator configuration resource and
	//`eventCollection.enabled=true` in any monitoring resource at the same time.
	//
	// +kubebuilder:validation:Optional
	EventCollection dash0common.EventCollection `json:"eventCollection,omitempty"`

	// Settings for scraping Prometheus metrics from pods in the target namespace according to their
	// prometheus.io/scrape annotations. This setting is optional, by default the operator will scrape metrics from pods
	// with these notations in the target namespace; unless there is an operator configuration resource with
	// `telemetryCollection.enabled=false`, then Prometheus scraping is off by default. It is a validation error to set
	// `telemetryCollection.enabled=false` in the operator configuration resource and `prometheusScraping.enabled=true`
	// in any monitoring resource at the same time.
	//
	// +kubebuilder:validation:Optional
	PrometheusScraping dash0common.PrometheusScraping `json:"prometheusScraping,omitempty"`

	// Optional filters for telemetry data that is collected in this namespace. This can be used to drop entire spans,
	// span events, metrics, metric data points, or log records. See "Transform" for advanced transformations (e.g.
	// removing span attributes, metric data point attributes, log record attributes etc.). This setting is optional,
	// by default, no filters are applied. It is a validation error to set `telemetryCollection.enabled=false` in the
	// operator configuration resource and set filters in any monitoring resource at the same time.
	//
	// +kubebuilder:validation:Optional
	Filter *dash0common.Filter `json:"filter,omitempty"`

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
	// This setting is optional, by default, no transformations are applied. It is a validation error to set
	// `telemetryCollection.enabled=false` in the operator configuration resource and set transforms in any monitoring
	// resource at the same time.
	//
	// +kubebuilder:validation:Optional
	Transform *dash0common.Transform `json:"transform,omitempty"`

	// Only used internally, this field must not be specified by users.
	NormalizedTransformSpec *dash0common.NormalizedTransformSpec `json:"__dash0_internal__normalizedTransform,omitempty"`

	// If enabled, the operator will watch Perses dashboard resources in this namespace and create corresponding
	// dashboards in Dash0 via the Dash0 API.
	// See https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md#managing-dash0-dashboards-with-the-operator
	// for details. This setting is optional, it defaults to `true`.
	//
	// +kubebuilder:default=true
	SynchronizePersesDashboards *bool `json:"synchronizePersesDashboards,omitempty"`

	// If enabled, the operator will watch Prometheus rule resources in this namespace and create corresponding check
	// rules in Dash0 via the Dash0 API.
	// See https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md#managing-dash0-check-rules-with-the-operator
	// for details. This setting is optional, it defaults to `true`.
	//
	// +kubebuilder:default=true
	SynchronizePrometheusRules *bool `json:"synchronizePrometheusRules,omitempty"`
}

type InstrumentWorkloads struct {
	// Controls the automatic workload instrumentation triggers for the target namespace, that is, which events will
	// trigger the auto-instrumentation of a workload. There are three possible
	// settings:
	// `all`, `created-and-updated` and `none`. By default, the setting `all` is assumed, unless there is an operator
	// configuration resource with `telemetryCollection.enabled=false`, then the setting `none` is assumed.
	//
	// If set to `all`, the operator will:
	// * automatically instrument existing workloads in the target namespace (i.e. workloads already running in the
	//   namespace) when the Dash0 monitoring resource is deployed,
	// * instrument existing workloads or update the instrumentation of already instrumented workloads in the target
	//   namespace when the Dash0 operator is first started or restarted (for example when updating the operator),
	// * instrument new workloads in the target namespace when they are deployed, and
	// * instrument changed workloads in the target namespace when changes are applied to them.
	// Note that the first two actions (instrumenting existing workloads) will result in restarting the pods of the
	// affected workloads.
	//
	// If set to `created-and-updated`, the operator will not instrument existing workloads in the target namespace,
	// neither when a Dash0 monitoring resource is deployed, nor when the Dash0 operator is started or restarted.
	// Instead, it will only:
	// * instrument new workloads in the target namespace when they are deployed, and
	// * instrument changed workloads in the target namespace when changes are applied to them.
	// This setting is useful if you want to avoid pod restarts as a side effect of deploying the Dash0 monitoring
	// resource or restarting the Dash0 operator.
	//
	// You can opt out of automatically instrumenting workloads entirely by setting this option to `none`. With
	// `mode: none`, workloads in the target namespace will never be instrumented to send telemetry to Dash0.
	//
	// If this setting is omitted, the value `all` is assumed and new, updated as well as existing Kubernetes workloads
	// will be automatically intrumented by the operator to send telemetry to Dash0, as described above. There is one
	// exception to this rule: If there is an operator configuration resource with `telemetryCollection.enabled=false`,
	// then the default setting is `none` instead of `all`, and no workloads will be instrumented by the Dash0 operator.
	//
	// It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and
	// `mode: all` or `mode: created-and-updated` in any monitoring resource at the
	// same time.
	//
	// More fine-grained per-workload control over instrumentation is available by setting the label
	// dash0.com/enable=false on individual workloads, or by using a custom label selector via
	// spec.instrumentWorkloads.labelSelector.
	//
	// +kubebuilder:validation:Optional
	Mode dash0common.InstrumentWorkloadsMode `json:"mode,omitempty"`

	// An optional configurable label selector for fine-grained per-workload control over instrumentation. Workloads
	// which match this label selector will be instrumented (according to the value of spec.instrumentWorkloads.mode).
	// Workloads which do not match this label selector will never be instrumented, regardless of the value of
	// spec.instrumentWorkloads.mode.
	//
	// This attribute is ignored if spec.instrumentWorkloads.mode=none.
	//
	// By default, this label selector has the value "dash0.com/enable!=false" - that is, the following workloads will
	// be instrumented (assuming spec.instrumentWorkloads.mode!=none)
	// - workloads that do not have the label dash0.com/enable at all, or
	// - workloads that have the label dash0.com/enable with a value other than "false".
	//
	// It is recommended to leave this setting unset (i.e. leave the default "dash0.com/enable!=false" in place), unless
	// you have a specific use case that requires a different label selector. One such use case is implementing an
	// opt-in model for workload instrumentation instead of the usual opt-out model. That is, instead of instrumenting
	// all workloads by default and only disabling instrumentation for a few specific workloads, you want to
	// deliberately turn on instrumentation for a few specific workloads and leave all others uninstrumented. Use a
	// label selector with equals instead of not-equals to achieve this, i.e.
	// spec.instrumentWorkloads.labelSelector="auto-instrument-this-workload-with-dash0=true".
	//
	// +kubebuilder:default=dash0.com/enable!=false
	LabelSelector string `json:"labelSelector,omitempty"`

	// Optional settings for how trace context is handled in instrumented workloads.
	//
	// +kubebuilder:validation:Optional
	TraceContext TraceContext `json:"traceContext,omitempty"`
}

type TraceContext struct {
	// An optional comma-separated list of trace context propagators. If set, the environment variable OTEL_PROPAGATORS
	// is added to workloads with the value of this field. This allows configuring the OpenTelemetry SDK to use specific
	// propagators for trace context propagation. The value can be a comma-separated list of propagators, for exampmle
	// "tracecontext,xray" for the W3C trace context traceparent header and AWS X-Ray headers.
	//
	// Note that you usually want to list the preferred propagator last, if multiple propagators are specified. The
	// reason is that both `Extract` (reading trace context information from headers and adding it to the span) and
	// `Inject` (addding trace context information to headers on outgoing requests) are both run in the order in which
	// the propagators are defined. For extract that means that the _last one wins_, if multiple propagators extract the
	// same information (e.g. trace ID and span ID). Hence, the need to specify them in reverse order of priority.
	//
	// By default, the value is not set and the environment variable OTEL_PROPAGATORS will not be added to workloads.
	// (The default values for OpenTelemetry SDKs when OTEL_PROPAGATORS is not set is "tracecontext,baggage".)
	//
	// See also: https://opentelemetry.io/docs/languages/sdk-configuration/general/#otel_propagators
	//
	// +kubebuilder:validation:Optional
	Propagators *string `json:"propagators,omitempty"`
}

// Dash0MonitoringStatus defines the observed state of the Dash0Monitoring monitoring resource.
type Dash0MonitoringStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// The spec.instrumentWorkloads.mode setting that has been observed in the previous reconcile cycle.
	// +kubebuilder:validation:Optional
	PreviousInstrumentWorkloads InstrumentWorkloads `json:"previousInstrumentWorkloads,omitempty"`

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
	if !slices.Contains(dash0common.AllInstrumentWorkloadsMode, instrumentWorkloadsMode) {
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

func (d *Dash0Monitoring) GetNamespaceInstrumentationConfig() util.NamespaceInstrumentationConfig {
	return util.NamespaceInstrumentationConfig{
		InstrumentationLabelSelector:    d.Spec.InstrumentWorkloads.LabelSelector,
		TraceContextPropagators:         d.Spec.InstrumentWorkloads.TraceContext.Propagators,
		PreviousTraceContextPropagators: d.Status.PreviousInstrumentWorkloads.TraceContext.Propagators,
	}
}

func (d *Dash0Monitoring) HasDash0ApiAccessConfigured() bool {
	return d.Spec.Export != nil &&
		d.Spec.Export.Dash0 != nil &&
		d.Spec.Export.Dash0.ApiEndpoint != "" &&
		(d.Spec.Export.Dash0.Authorization.Token != nil || d.Spec.Export.Dash0.Authorization.SecretRef != nil)
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
