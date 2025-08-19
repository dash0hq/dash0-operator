// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	dash0operator "github.com/dash0hq/dash0-operator/api/operator"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	annotationNameSpecInstrumentWorkloadsTraceContextPropagators           = "dash0.com/spec.instrumentWorkloads.traceContext.propagators"
	annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators = "dash0.com/status.previousInstrumentWorkloads.traceContext.propagators"
	annotationNameSpecInstrumentWorkloadsLabelSelector                     = "dash0.com/spec.instrumentWorkloads.labelSelector"
	annotationNameStatusPreviousInstrumentWorkloadsLabelSelector           = "dash0.com/status.previousInstrumentWorkloads.labelSelector"
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
	Export *dash0common.Export `json:"export,omitempty"`

	// Opt-out for automatic workload instrumentation for the target namespace. There are three possible settings:
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
	// If set to `created-and-updated`, the operator will not instrument existing workloads in the target namespace.
	// Instead, it will only:
	// * instrument new workloads in the target namespace when they are deployed, and
	// * instrument changed workloads in the target namespace when changes are applied to them.
	// This setting is useful if you want to avoid pod restarts as a side effect of deploying the Dash0 monitoring
	// resource or restarting the Dash0 operator.
	//
	// You can opt out of automatically instrumenting workloads entirely by setting this option to `none`. With
	// `instrumentWorkloads: none`, workloads in the target namespace will never be instrumented to send telemetry to
	// Dash0.
	//
	// If this setting is omitted, the value `all` is assumed and new, updated as well as existing Kubernetes workloads
	// will be automatically intrumented by the operator to send telemetry to Dash0, as described above. There is one
	// exception to this rule: If there is an operator configuration resource with `telemetryCollection.enabled=false`,
	// then the default setting is `none` instead of `all`, and no workloads will be instrumented by the Dash0 operator.
	//
	// It is a validation error to set `telemetryCollection.enabled=false` in the operator configuration resource and
	// `instrumentWorkloadsMode=all` or `instrumentWorkloadsMode=created-and-updated` in any monitoring resource at the
	// same time.
	//
	// More fine-grained per-workload control over instrumentation is available by setting the label
	// dash0.com/enable=false on individual workloads.
	//
	// +kubebuilder:validation:Optional
	InstrumentWorkloads dash0common.InstrumentWorkloadsMode `json:"instrumentWorkloads,omitempty"`

	// Settings for log collection in the target namespace. This setting is optional, by default the operator will
	// collect pod logs in the target namespace; unless there is an operator configuration resource with
	// `telemetryCollection.enabled=false`, then log collection is off by default. It is a validation error to set
	// `telemetryCollection.enabled=false` in the operator configuration resource and `logCollection.enabled=true` in any
	// monitoring resource at the same time.
	//
	// +kubebuilder:validation:Optional
	LogCollection dash0common.LogCollection `json:"logCollection,omitempty"`

	// Settings for scraping Prometheus metrics from pods in the target namespace according to their
	// prometheus.io/scrape annotations. This setting is optional, by default the operator will scrape metrics from pods
	// with these notations in the target namespace; unless there is an operator configuration resource with
	// `telemetryCollection.enabled=false`, then Prometheus scraping is off by default. It is a validation error to set
	// `telemetryCollection.enabled=false` in the operator configuration resource and `prometheusScraping.enabled=true`
	// in any monitoring resource at the same time.
	//
	// +kubebuilder:validation:Optional
	PrometheusScraping dash0common.PrometheusScraping `json:"prometheusScraping,omitempty"`

	// Deprecated: This setting is deprecated. Please use
	//     prometheusScraping:
	//       enabled: false
	// instead of
	//     prometheusScrapingEnabled: false
	//
	// If enabled, the operator will configure its OpenTelemetry collector to scrape metrics from pods in the namespace
	// of this Dash0Monitoring resource according to their prometheus.io/scrape annotations via the OpenTelemetry
	// Prometheus receiver. This setting is optional, it defaults to `true`; unless there is an operator configuration
	// resource with `telemetryCollection.enabled=false`, then it defaults to `false`. It is a validation error to set
	// `telemetryCollection.enabled=false` in the operator configuration resource and `prometheusScrapingEnabled=true`
	// in any monitoring resource at the same time.
	//
	// +kubebuilder:validation:Optional
	PrometheusScrapingEnabled *bool `json:"prometheusScrapingEnabled,omitempty"`

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

// Dash0MonitoringStatus defines the observed state of the Dash0Monitoring monitoring resource.
type Dash0MonitoringStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// The spec.instrumentWorkloads settings that have been observed in the previous reconcile cycle.
	// +kubebuilder:validation:Optional
	PreviousInstrumentWorkloads dash0common.InstrumentWorkloadsMode `json:"previousInstrumentWorkloads,omitempty"`

	// Shows results of synchronizing Perses dashboard resources in this namespace via the Dash0 API.
	// +kubebuilder:validation:Optional
	PersesDashboardSynchronizationResults map[string]dash0common.PersesDashboardSynchronizationResults `json:"persesDashboardSynchronizationResults,omitempty"`

	// Shows results of synchronizing Prometheus rule resources in this namespace via the Dash0 API.
	// +kubebuilder:validation:Optional
	PrometheusRuleSynchronizationResults map[string]dash0common.PrometheusRuleSynchronizationResult `json:"prometheusRuleSynchronizationResults,omitempty"`
}

// Dash0Monitoring is the schema for the Dash0Monitoring API
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +groupName=operator.dash0.com
// +kubebuilder:conversion:spoke
type Dash0Monitoring struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0MonitoringSpec   `json:"spec,omitempty"`
	Status Dash0MonitoringStatus `json:"status,omitempty"`
}

func (d *Dash0Monitoring) ReadInstrumentWorkloadsSetting() dash0common.InstrumentWorkloadsMode {
	instrumentWorkloads := d.Spec.InstrumentWorkloads
	if instrumentWorkloads == "" {
		return dash0common.InstrumentWorkloadsModeAll
	}
	if !slices.Contains(dash0common.AllInstrumentWorkloadsMode, instrumentWorkloads) {
		return dash0common.InstrumentWorkloadsModeAll
	}
	return instrumentWorkloads
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

// ConvertFrom converts the hub version (v1beta1) to this Dash0Monitoring resource version (v1alpha1).
func (dst *Dash0Monitoring) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*dash0v1beta1.Dash0Monitoring)
	dst.ObjectMeta = src.ObjectMeta
	dst.Spec.Export = src.Spec.Export
	dst.Spec.InstrumentWorkloads = src.Spec.InstrumentWorkloads.Mode
	if strings.TrimSpace(src.Spec.InstrumentWorkloads.LabelSelector) != "" &&
		src.Spec.InstrumentWorkloads.LabelSelector != util.DefaultAutoInstrumentationLabelSelector {
		if dst.ObjectMeta.Annotations == nil {
			dst.ObjectMeta.Annotations = make(map[string]string)
		}
		dst.ObjectMeta.Annotations[annotationNameSpecInstrumentWorkloadsLabelSelector] =
			src.Spec.InstrumentWorkloads.LabelSelector
	}
	if src.Spec.InstrumentWorkloads.TraceContext.Propagators != nil && *src.Spec.InstrumentWorkloads.TraceContext.Propagators != "" {
		if dst.ObjectMeta.Annotations == nil {
			dst.ObjectMeta.Annotations = make(map[string]string)
		}
		dst.ObjectMeta.Annotations[annotationNameSpecInstrumentWorkloadsTraceContextPropagators] =
			*src.Spec.InstrumentWorkloads.TraceContext.Propagators
	}
	dst.Spec.LogCollection = src.Spec.LogCollection
	dst.Spec.PrometheusScraping = src.Spec.PrometheusScraping
	dst.Spec.PrometheusScraping.Enabled = src.Spec.PrometheusScraping.Enabled
	dst.Spec.Filter = src.Spec.Filter
	dst.Spec.Transform = src.Spec.Transform
	dst.Spec.NormalizedTransformSpec = src.Spec.NormalizedTransformSpec
	dst.Spec.SynchronizePersesDashboards = src.Spec.SynchronizePersesDashboards
	dst.Spec.SynchronizePrometheusRules = src.Spec.SynchronizePrometheusRules
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.PreviousInstrumentWorkloads = src.Status.PreviousInstrumentWorkloads.Mode
	if strings.TrimSpace(src.Status.PreviousInstrumentWorkloads.LabelSelector) != "" &&
		src.Status.PreviousInstrumentWorkloads.LabelSelector != util.DefaultAutoInstrumentationLabelSelector {
		if dst.ObjectMeta.Annotations == nil {
			dst.ObjectMeta.Annotations = make(map[string]string)
		}
		dst.ObjectMeta.Annotations[annotationNameStatusPreviousInstrumentWorkloadsLabelSelector] =
			src.Status.PreviousInstrumentWorkloads.LabelSelector
	}
	if src.Status.PreviousInstrumentWorkloads.TraceContext.Propagators != nil && *src.Status.PreviousInstrumentWorkloads.TraceContext.Propagators != "" {
		if dst.ObjectMeta.Annotations == nil {
			dst.ObjectMeta.Annotations = make(map[string]string)
		}
		dst.ObjectMeta.Annotations[annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators] =
			*src.Status.PreviousInstrumentWorkloads.TraceContext.Propagators
	}
	dst.Status.PersesDashboardSynchronizationResults = src.Status.PersesDashboardSynchronizationResults
	dst.Status.PrometheusRuleSynchronizationResults = src.Status.PrometheusRuleSynchronizationResults
	return nil
}

// ConvertTo converts this Dash0Monitoring resource (v1alpha1) to the hub version (v1beta1).
func (src *Dash0Monitoring) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*dash0v1beta1.Dash0Monitoring)
	dst.ObjectMeta = src.ObjectMeta
	dst.Spec.Export = src.Spec.Export
	dst.Spec.InstrumentWorkloads = dash0v1beta1.InstrumentWorkloads{
		LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
		Mode:          src.Spec.InstrumentWorkloads,
	}
	if src.ObjectMeta.Annotations != nil {
		labelSelector, labelSelectorFound :=
			src.ObjectMeta.Annotations[annotationNameSpecInstrumentWorkloadsLabelSelector]
		if labelSelectorFound && strings.TrimSpace(labelSelector) != "" {
			dst.Spec.InstrumentWorkloads.LabelSelector = labelSelector
		}
		delete(dst.ObjectMeta.Annotations, annotationNameSpecInstrumentWorkloadsLabelSelector)

		propagators, propagatorsFound :=
			src.ObjectMeta.Annotations[annotationNameSpecInstrumentWorkloadsTraceContextPropagators]
		if propagatorsFound && strings.TrimSpace(propagators) != "" {
			dst.Spec.InstrumentWorkloads.TraceContext.Propagators = ptr.To(propagators)
		}
		delete(dst.ObjectMeta.Annotations, annotationNameSpecInstrumentWorkloadsTraceContextPropagators)
	}
	dst.Spec.LogCollection = src.Spec.LogCollection
	dst.Spec.PrometheusScraping = src.Spec.PrometheusScraping
	prometheusScrapingEnabledLegacy := util.ReadBoolPointerWithDefault(src.Spec.PrometheusScrapingEnabled, true)
	prometheusScrapingEnabledNew := util.ReadBoolPointerWithDefault(src.Spec.PrometheusScraping.Enabled, true)
	if !prometheusScrapingEnabledLegacy || !prometheusScrapingEnabledNew {
		// If either setting has been disabled explicitly, the Prometheus scraping has to be disabled in the v1beta1
		// version as well.
		dst.Spec.PrometheusScraping.Enabled = ptr.To(false)
	}
	dst.Spec.Filter = src.Spec.Filter
	dst.Spec.Transform = src.Spec.Transform
	dst.Spec.NormalizedTransformSpec = src.Spec.NormalizedTransformSpec
	dst.Spec.SynchronizePersesDashboards = src.Spec.SynchronizePersesDashboards
	dst.Spec.SynchronizePrometheusRules = src.Spec.SynchronizePrometheusRules
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.PreviousInstrumentWorkloads = dash0v1beta1.InstrumentWorkloads{
		LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
		Mode:          src.Status.PreviousInstrumentWorkloads,
	}
	if src.ObjectMeta.Annotations != nil {
		previousLabelSelector, previousLabelSelectorFound :=
			src.ObjectMeta.Annotations[annotationNameStatusPreviousInstrumentWorkloadsLabelSelector]
		if previousLabelSelectorFound && strings.TrimSpace(previousLabelSelector) != "" {
			dst.Status.PreviousInstrumentWorkloads.LabelSelector = previousLabelSelector
		}
		delete(dst.ObjectMeta.Annotations, annotationNameStatusPreviousInstrumentWorkloadsLabelSelector)

		previousPropagators, previousPropagatorsFound :=
			src.ObjectMeta.Annotations[annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators]
		if previousPropagatorsFound && strings.TrimSpace(previousPropagators) != "" {
			dst.Status.PreviousInstrumentWorkloads.TraceContext.Propagators = ptr.To(previousPropagators)
		}
		delete(dst.ObjectMeta.Annotations, annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators)
	}
	dst.Status.PersesDashboardSynchronizationResults = src.Status.PersesDashboardSynchronizationResults
	dst.Status.PrometheusRuleSynchronizationResults = src.Status.PrometheusRuleSynchronizationResults
	return nil
}
