// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0operator "github.com/dash0hq/dash0-operator/api/operator"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
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
// +kubebuilder:printcolumn:name="Collect Node Meta",type="boolean",JSONPath=".spec.collectNodeLabelsAndAnnotations.enabled"
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
	// Deprecated: Use `exports` instead. If both `export` and `exports` are specified, `export` will be ignored.
	// The mutating webhook will automatically migrate `export` to `exports` if only `export` is specified.
	//
	// The configuration of the default observability backend to which telemetry data will be sent by the operator, as
	// well as the backend that will receive the operator's self-monitoring data. This property is mandatory.
	// This can either be Dash0 or another OTLP-compatible backend. You can also combine up to three exporters (i.e.,
	// Dash0 plus gRPC plus HTTP). This allows sending the same data to two or three targets simultaneously. At least
	// one exporter has to be defined.
	//
	// Please note that self-monitoring data is only sent to one backend, with Dash0 taking precedence over gRPC and
	// HTTP, and gRPC taking precedence over HTTP if multiple exports are defined. Furthermore, HTTP export with JSON
	// encoding is not supported for self-monitoring telemetry.
	Export *dash0common.Export `json:"export,omitempty"`

	// The configuration of the default observability backends to which telemetry data will be sent by the operator, as
	// well as the backend that will receive the operator's self-monitoring data. This property is mandatory.
	// These can either be Dash0 or another OTLP-compatible backend. Every Export entry can contain up to three distinct
	// exporters (i.e., Dash0 plus gRPC plus HTTP).
	//
	// The telemetry data will be sent to all backends of all Export entries. At least one exporter has to be defined.
	//
	// Please note that self-monitoring data is only sent to a single backend of the first defined Export.
	// If there are multiple backends in the Export, the Dash0 export is taking precedence over gRPC and HTTP, and gRPC taking
	// precedence over HTTP if multiple exports are defined. Furthermore, HTTP export with JSON encoding is not supported
	// for self-monitoring telemetry.
	Exports []dash0common.Export `json:"exports,omitempty"`

	// Global opt-out for self-monitoring for this operator
	//
	// +kubebuilder:default={enabled: true}
	SelfMonitoring SelfMonitoring `json:"selfMonitoring,omitempty"`

	// Settings for collecting Kubernetes infrastructure metrics. This setting is optional, by default, the operator will
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

	// Settings for collecting node labels and annotations. This setting is optional, by default the operator will
	// not collect node labels and annotations as resource attributes. It is a validation error to set
	// `telemetryCollection.enabled=false` and `collectNodeLabelsAndAnnotations.enabled=true` at the same time.
	//
	// +kubebuilder:validation:Optional
	CollectNodeLabelsAndAnnotations CollectNodeLabelsAndAnnotations `json:"collectNodeLabelsAndAnnotations,omitempty"`

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

	// InstrumentWorkloads contains cluster-wide settings that govern how the operator auto-instruments workloads.
	//
	// Note: There are also instrumentWorkloads settings in the Dash0 monitoring resource, for per-namespace settings for
	// workload instrumentation.
	//
	// +kubebuilder:default={instrumentationDelivery: init-container}
	InstrumentWorkloads InstrumentWorkloads `json:"instrumentWorkloads,omitempty"`

	// An opt-out switch for all telemetry collection, and to avoid having the operator deploy OpenTelemetry collectors
	// to the cluster. This setting is optional, it defaults to true.
	//
	// +kubebuilder:default={enabled: true}
	TelemetryCollection TelemetryCollection `json:"telemetryCollection,omitempty"`

	// Settings for automatically monitoring namespaces. This feature is off by default and needs to be enabled
	// explicitly.
	//
	// +kubebuilder:default={enabled: false, labelSelector: "dash0.com/enable!=false"}
	AutoMonitorNamespaces AutoMonitorNamespaces `json:"autoMonitorNamespaces,omitempty"`

	// MonitoringTemplate describes the Dash0Monitoring resources that will be created for namespaces in case
	// automatic namespace monitoring is enabled.
	//
	// +kubebuilder:validation:Optional
	MonitoringTemplate *MonitoringTemplate `json:"monitoringTemplate,omitempty"`

	// Profiling describes the profiling configuration for the operator.
	//
	// +kubebuilder:validation:Optional
	Profiling *Profiling `json:"profiling,omitempty"`

	// Settings for the agent0-connector.
	//
	// +kubebuilder:validation:Optional
	Agent0Connector Agent0Connector `json:"agent0Connector,omitempty"`

	// Settings for the synthetics-worker.
	//
	// +kubebuilder:validation:Optional
	SyntheticsWorker SyntheticsWorker `json:"syntheticsWorker,omitempty"`
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
	Enabled *bool `json:"enabled,omitempty"`
}

type PrometheusCrdSupport struct {
	// If enabled, the operator will add support for Prometheus CRDs (PodMonitor, ServiceMonitor, ScrapeConfig) by
	// deploying the OpenTelemetry target-allocator.
	// It is a validation error to set`telemetryCollection.enabled=false` and `prometheusCrdSupport.enabled=true` at
	// the same time.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`
}

// Agent0Connector contains settings for the agent0-connector.
type Agent0Connector struct {
	// An opt-out switch for the agent0-connector deployment. This setting is optional. Setting it to `false` prevents the
	// operator from deploying the agent0-connector, even when the agent0-connector is enabled via the Helm chart. It is a
	// validation error to set it to `true` when the agent0-connector is disabled via the Helm chart.
	//
	// Using this setting to disable agent0-connector when it is enabled via Helm and the Helm chart manages the
	// Dash0OperatorConfiguration resource (operator.dash0Export.enabled=true) is not supported.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`
}

// IsEnabled reports whether the operator deploys the agent0-connector. The parameter enabledViaHelm is the value of the
// Helm value operator.agent0Connector.enabled, which the operator holds for the lifetime of the process. The
// agent0-connector requires that Helm value; this resource can only opt out of it, which is why an unset Enabled flag
// follows the Helm value.
func (a Agent0Connector) IsEnabled(enabledViaHelm bool) bool {
	return enabledViaHelm && pointers.ReadBoolPointerWithDefault(a.Enabled, true)
}

// SyntheticsWorker contains settings for the synthetics-worker feature, the private-location runner that dials
// outbound to Dash0 and executes synthetic checks. A cluster can shard checks across multiple Dash0 private
// locations by configuring multiple Instances, each running as its own Kubernetes Deployment.
type SyntheticsWorker struct {
	// An opt-out switch for the synthetics-worker feature. This setting is optional. Setting it to `false` prevents
	// the operator from deploying any synthetics-worker instance, even when the synthetics-worker is enabled via the
	// Helm chart. It is a validation error to set it to `true` when the synthetics-worker is disabled via the Helm
	// chart.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`

	// Instances lists the synthetics-worker deployments the operator manages, one per Dash0 private location. Each
	// instance's LocationID must be unique within this list; it is also used to derive the names of that instance's
	// Kubernetes resources. At least one instance is required when the synthetics-worker is enabled.
	//
	// +kubebuilder:validation:Optional
	// +listType=map
	// +listMapKey=locationId
	Instances []SyntheticsWorkerInstance `json:"instances,omitempty"`
}

// SyntheticsWorkerInstance describes a single synthetics-worker Deployment, pinned to one Dash0 private location.
type SyntheticsWorkerInstance struct {
	// LocationID is the customer-chosen identifier of the private location that this instance executes checks for.
	// Dash0 resolves it to the location's identity. It must be unique within spec.syntheticsWorker.instances, and is
	// used to derive the names of this instance's Kubernetes resources.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=40
	LocationID string `json:"locationId"`

	// Authorization holds the Dash0 authorization token for this instance's workload, either as a literal token or as
	// a reference to a Kubernetes secret. Required when the synthetics-worker is enabled. This is a pointer so that
	// the field can be omitted entirely; the Authorization type itself requires at least one of its own properties to
	// be set, which would reject an explicit empty object.
	//
	// +kubebuilder:validation:Optional
	Authorization *dash0common.Authorization `json:"authorization,omitempty"`

	// Replicas is the number of pods this instance runs. This setting is optional, it defaults to 1.
	//
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources describes the compute resource requirements for this instance's container. This setting is optional.
	//
	// +kubebuilder:validation:Optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeAffinity constrains which nodes this instance's pods can be scheduled on, e.g. to pin a private location's
	// worker to nodes in a particular region or zone. This setting is optional.
	//
	// +kubebuilder:validation:Optional
	NodeAffinity *corev1.NodeAffinity `json:"nodeAffinity,omitempty"`

	// Tolerations allow this instance's pods to be scheduled on nodes with matching taints. This setting is optional.
	//
	// +kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// IsEnabled reports whether the operator deploys the synthetics-worker. The parameter enabledViaHelm is the value of
// the Helm value operator.syntheticsWorker.enabled, which the operator holds for the lifetime of the process. The
// synthetics-worker requires that Helm value; this resource can only opt out of it, which is why an unset Enabled
// flag follows the Helm value.
func (s SyntheticsWorker) IsEnabled(enabledViaHelm bool) bool {
	return enabledViaHelm && pointers.ReadBoolPointerWithDefault(s.Enabled, true)
}

// InstrumentationDelivery selects how the Dash0 instrumentation files (the OpenTelemetry injector and the
// auto-instrumentation agents) are made available to instrumented workload containers.
//
// +kubebuilder:validation:Enum=auto;image-volume;init-container
type InstrumentationDelivery string

const (
	// InstrumentationDeliveryAuto lets the operator decide which delivery mechanism to use, based on the Kubernetes
	// version. If the Kubernetes API server version and the kubelet version of every node are 1.36 or later, the
	// operator uses image volumes; otherwise it falls back to the init container approach.
	InstrumentationDeliveryAuto InstrumentationDelivery = "auto"

	// InstrumentationDeliveryImageVolume forces the operator to use the image volume delivery mechanisms on Kubernetes
	// versions 1.31 - 1.35. The setting is ignored if the Kubernetes API server version or the kubelet version of any
	// node is older than 1.31.
	InstrumentationDeliveryImageVolume InstrumentationDelivery = "image-volume"

	// InstrumentationDeliveryInitContainer forces the operator to use the init container plus emptyDir volume delivery
	// approach, even on Kubernetes 1.36 or newer.
	InstrumentationDeliveryInitContainer InstrumentationDelivery = "init-container"
)

// InstrumentWorkloads contains cluster-wide settings that govern how the operator instruments workloads.
type InstrumentWorkloads struct {
	// InstrumentationDelivery controls how the Dash0 instrumentation files are made available to instrumented
	// workload containers. Allowed values:
	//   - "auto": use image volumes if the Kubernetes API server version and the kubelet version of all nodes is 1.36 or
	//     later, otherwise use the init container approach. The operator determines the minimum kubelet version by
	//     inspecting the cluster's nodes once at startup, until that has finished it uses the init container approach.
	//     Nodes joining the cluster later are not taken into account.
	//   - "image-volume": always use a Kubernetes image volume sourced from the Dash0 instrumentation image. If the
	//     operator has already detected that the Kubernetes API server version or the kubelet version of a node is
	//     older than 1.31, it logs a warning and falls back to the init container approach. That fallback is
	//     best-effort: it does not cover workloads instrumented before the operator has inspected the cluster's nodes,
	//     nor nodes that join the cluster later. Note that on Kubernetes 1.34 and earlier, image volumes need to be
	//     enabled at cluster creation time, since the feature gate is disabled by default in versions older than 1.35.
	//   - "init-container" (default): always use the traditional init container plus emptyDir volume approach, regardless
	//     of the Kubernetes version.
	//
	// Note: Changing the instrumentationDelivery setting in the operator configuration resource while the operator is
	// running will not trigger a bulk re-instrumentation of all existing workloads, even for namespaces that are set to
	// instrumentWorkloadsMode=all. Once a workload has been successfully instrumented, there is no benefit in
	// re-instrumenting it with a different delivery mechanism. The new setting will be applied when instrumenting
	// newly deployed workloads, or when a workload is updated/re-deployed.
	//
	// +kubebuilder:default=init-container
	InstrumentationDelivery InstrumentationDelivery `json:"instrumentationDelivery,omitempty"`
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
	Enabled *bool `json:"enabled,omitempty"`
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
	Enabled *bool `json:"enabled,omitempty"`
}

type CollectNodeLabelsAndAnnotations struct {
	// Opt-in for collecting all node labels and annotations as resource attributes. If set to `true`, the operator
	// will collect Kubernetes node labels and annotations as resource attributes.
	//
	// This setting is optional, it defaults to `false`, that is, if this setting is omitted, the value `false` is
	// assumed and the operator will not collect node labels and annotations as resource attributes. It is a
	// validation error to set `telemetryCollection.enabled=false` and
	// `collectNodeLabelsAndAnnotations.enabled=true` at the same time.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`
}

type TelemetryCollection struct {
	// If disabled, the operator will not collect any telemetry, in particular it will not deploy any OpenTelemetry
	// collectors to the cluster. This is useful if you want to do infrastructure-as-code (dashboards, check rules) with
	// the operator, but do not want it to deploy the OpenTelemetry collector. This setting is optional, it defaults to
	// `true` (i.e., by default telemetry collection is enabled).
	//
	// Note that setting this to false does not disable the operator's self-monitoring telemetry, use the setting
	// selfMonitoring.enabled to disable self-monitoring if required (self-monitoring does not require an OpenTelemetry
	// collector).
	//
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled"`
}

type AutoMonitorNamespaces struct {

	// Controls whether monitoring is set up for namespaces automatically. By default, a Dash0Monitoring resource has to
	// be added to each namespace that you want to monitor. With automatic namespace monitoring, you can let the Dash0
	// operator automate this. This is useful if you want to monitor all or almost all namespaces in your cluster. It is
	// also useful if you create new namespaces frequently and want to have them monitored right away, without additional
	// setup. It is best suited if almost all namespace should be monitored in the same fashion.
	//
	// If enabled, the operator will:
	// * automatically add monitoring to all existing namespaces at startup, and
	// * automatically add monitoring to new namespaces, as they are created.
	//
	// Even when enabled, individual namespaces can opt out of automatic monitoring via label selectors
	// (see spec.autoMonitorNamespaces.labelSelector).
	//
	// Namespaces which are subject to automatic namespace monitoring will be monitored according to the settings of
	// spec.monitoringTemplate.
	//
	// +kubebuilder:default=false
	Enabled *bool `json:"enabled"`

	// An optional configurable label selector for controlling which namespaces are automatically monitored. Namespaces
	// which match this label selector will be monitored automatically (if spec.autoMonitorNamespaces.enabled is also
	// set). Namespaces which do not match this label selector will not be monitored, regardless of the value of
	// spec.autoMonitorNamespaces.enabled.
	//
	// This attribute is ignored if spec.autoMonitorNamespaces.enabled has not been set to true explicitly.
	//
	// By default, this label selector has the value "dash0.com/enable!=false" - that is, the following namespaces will
	// be monitored (assuming spec.autoMonitorNamespaces.enabled is true):
	// - namespaces which do not have the label dash0.com/enable at all, and
	// - namespaces which have the label dash0.com/enable with a value other than "false".
	//
	// Namespaces which are subject to automatic namespace monitoring will be monitored according to the settings of
	// spec.monitoringTemplate.
	//
	// +kubebuilder:default=dash0.com/enable!=false
	LabelSelector string `json:"labelSelector,omitempty"`
}

func (a AutoMonitorNamespaces) IsEnabled() bool {
	return pointers.ReadBoolPointerWithDefault(a.Enabled, false)
}

// MonitoringTemplate describes the Dash0Monitoring resources that will be created for namespaces in case
// automatic namespace monitoring is enabled.
type MonitoringTemplate struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the desired settings for monitoring namespaces, i.e., the details of monitoring Kubernetes
	// namespaces with Dash0
	// +kubebuilder:validation:Optional
	Spec dash0v1beta1.Dash0MonitoringSpec `json:"spec,omitempty"`
}

// Profiling describes whether the operator should configure its OpenTelemetry collectors to accept, process
// and export profiling data. When enabled, the daemonset collector will include additional connectors, routing, and
// pipeline definitions for the profiles signal type.
type Profiling struct {
	// If enabled, the operator will set up the pipelines to receive, process and forward profiling data over OTLP.
	// This setting is optional, it defaults to `false`.
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`
}

// Dash0OperatorConfigurationStatus defines the observed state of the Dash0 operator configuration resource.
type Dash0OperatorConfigurationStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// PreviousAutoMonitorNamespacesLabelSelector records the label selector that was active when the operator
	// configuration resource was reconciled the last time by the auto-namespace-monitoring controller. If it differs from
	// the current spec.autoMonitorNamespaces.labelSelector, the namespace watch will be recreated with the new selector.
	//
	// +kubebuilder:validation:Optional
	PreviousAutoMonitorNamespacesLabelSelector string `json:"previousAutoMonitorNamespacesLabelSelector,omitempty"`

	// PreviousMonitoringTemplate records the monitoring template that was active when the operator configuration
	// resource was reconciled the last time by the auto-namespace-monitoring controller.
	//
	// +kubebuilder:validation:Optional
	PreviousMonitoringTemplate *MonitoringTemplate `json:"previousMonitoringTemplate,omitempty"`

	// Agent0Connector reports whether the operator has deployed the agent0-connector, and why not if it hasn't. A
	// disabled agent0-connector is reported with deployed=false and reason=Disabled, which is what distinguishes it
	// from one that failed to deploy. This is absent until the operator reconciles the agent0-connector for the first
	// time, which it only does when the agent0-connector is enabled via the Helm chart
	// (operator.agent0Connector.enabled).
	//
	// +kubebuilder:validation:Optional
	Agent0Connector *Agent0ConnectorStatus `json:"agent0Connector,omitempty"`

	// SyntheticsWorker reports whether the operator has deployed the synthetics-worker, and why not if it hasn't. A
	// disabled synthetics-worker is reported with deployed=false and reason=Disabled, which is what distinguishes it
	// from one that failed to deploy. This is absent until the operator reconciles the synthetics-worker for the
	// first time, which it only does when the synthetics-worker is enabled via the Helm chart
	// (operator.syntheticsWorker.enabled).
	//
	// +kubebuilder:validation:Optional
	SyntheticsWorker *SyntheticsWorkerStatus `json:"syntheticsWorker,omitempty"`
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
		},
	)
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileStarted",
			Message: "Dash0 operator configuration resource reconciliation is in progress.",
		},
	)
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
		},
	)
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
		},
	)
	meta.SetStatusCondition(
		&d.Status.Conditions,
		metav1.Condition{
			Type:    string(dash0common.ConditionTypeDegraded),
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
	)
}

// EffectiveExports returns Exports if set, otherwise wraps the deprecated Export field into a single-element slice.
// This ensures code that reads exports works for resources that have not yet been migrated by the mutating webhook.
func (d *Dash0OperatorConfiguration) EffectiveExports() []dash0common.Export {
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

func (d *Dash0OperatorConfiguration) HasExportsConfigured() bool {
	return d != nil && len(d.EffectiveExports()) > 0
}

func (d *Dash0OperatorConfiguration) ExportsCount() int {
	if !d.HasExportsConfigured() {
		return 0
	} else {
		return dash0common.CountExports(d.EffectiveExports())
	}
}

func (d *Dash0OperatorConfiguration) HasDash0ExportConfigured() bool {
	return len(d.GetDash0Exports()) > 0
}

func (d *Dash0OperatorConfiguration) GetFirstDash0Export() *dash0common.Export {
	exports := d.EffectiveExports()
	for i := range exports {
		if exports[i].HasDash0ExportConfigured() {
			return &exports[i]
		}
	}
	return nil
}

func (d *Dash0OperatorConfiguration) GetDash0Exports() []dash0common.Dash0Configuration {
	var dash0Configs []dash0common.Dash0Configuration
	exports := d.EffectiveExports()
	for i := range exports {
		if exports[i].HasDash0ExportConfigured() {
			dash0Configs = append(dash0Configs, *exports[i].Dash0)
		}
	}
	return dash0Configs
}

func (d *Dash0OperatorConfiguration) HasDash0ApiAccessConfigured() bool {
	return len(d.GetDash0ExportsWithApiAccess()) > 0
}

func (d *Dash0OperatorConfiguration) GetDash0ExportsWithApiAccess() []dash0common.Dash0Configuration {
	var res []dash0common.Dash0Configuration
	for _, export := range d.EffectiveExports() {
		// intentionally not doing any further filtering here, as validation will happen later where errors are properly logged
		if export.Dash0 != nil {
			res = append(res, *export.Dash0)
		}
	}
	return res
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

func (d *Dash0OperatorConfiguration) LogResourceAsEvent(logger logd.Logger) {
	redactedOperatorConfiguration := d.cloneAndRedact()
	if operatorConfigurationResourceMarshalled, err := json.Marshal(redactedOperatorConfiguration); err != nil {
		logger.Error(err, "cannot marshal Dash0OperatorConfiguration resource for dash0.operator_configuration event")
	} else {
		logger.Info(
			// per https://opentelemetry.io/docs/specs/semconv/general/events, events should not use the log body
			"",
			"otel.event.name", "dash0.operator_configuration_resource",
			"dash0.monitoring.operator_configuration_resource.snapshot", string(operatorConfigurationResourceMarshalled),
			"dash0.monitoring.operator_configuration_resource.namespace", d.GetNamespace(),
			"dash0.monitoring.operator_configuration_resource.name", d.GetName(),
		)
	}
}

func (d *Dash0OperatorConfiguration) cloneAndRedact() Dash0OperatorConfiguration {
	redactedResource := Dash0OperatorConfiguration{}
	d.DeepCopyInto(&redactedResource)
	redactedResource.ManagedFields = nil
	// This annotation embeds a verbatim copy of the spec (including the plaintext token) that Export.Redact() cannot reach.
	delete(redactedResource.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
	for _, export := range redactedResource.EffectiveExports() {
		export.Redact()
	}
	return redactedResource
}

// Agent0ConnectorStatus reports whether the operator has deployed the agent0-connector, that is, whether it has
// successfully created or updated the agent0-connector's service account, cluster role, cluster role binding and
// deployment. It does not report whether the agent0-connector's pod is up: a successful deployment can still fail to
// start, which the deployment resource itself reports.
//
// This is deliberately not a status condition: the agent0-connector is an optional feature, and an issue with it
// neither makes the operator configuration resource unavailable nor degraded.
type Agent0ConnectorStatus struct {
	// Deployed reports whether the operator has successfully created or updated the agent0-connector resources the
	// last time it tried.
	Deployed bool `json:"deployed"`

	// Reason is a programmatic identifier for the last negative outcome, e.g. "InvalidClusterRoleRules".
	//
	// +kubebuilder:validation:Optional
	Reason string `json:"reason,omitempty"`

	// Message describes the last outcome in a human-readable form.
	//
	// +kubebuilder:validation:Optional
	Message string `json:"message,omitempty"`

	// LastTransitionTime is the time at which Deployed last changed.
	//
	// +kubebuilder:validation:Optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// SetAgent0ConnectorStatus records the outcome of the last attempt to create or update the agent0-connector resources.
// It reports whether the recorded state changed, so that the caller only queues a Kubernetes event on a transition
// instead of on every reconciliation.
//
// A change is the value of Deployed flipping, the status appearing for the first time, or the reason changing while
// the agent0-connector is not deployed - an operator who fixes one misconfiguration and runs into the next one has to
// learn about the second one as well. LastTransitionTime only advances when Deployed flips, mirroring the semantics of
// a status condition.
func (d *Dash0OperatorConfiguration) SetAgent0ConnectorStatus(deployed bool, reason string, message string) bool {
	previous := d.Status.Agent0Connector
	changed := previous == nil ||
		previous.Deployed != deployed ||
		(!deployed && previous.Reason != reason)

	lastTransitionTime := metav1.Now()
	if previous != nil && previous.Deployed == deployed {
		lastTransitionTime = previous.LastTransitionTime
	}
	d.Status.Agent0Connector = &Agent0ConnectorStatus{
		Deployed:           deployed,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: lastTransitionTime,
	}
	return changed
}

// SyntheticsWorkerStatus reports the aggregate and per-instance state of the synthetics-worker feature, that is,
// whether the operator has successfully created or updated each instance's service account and deployment. It does
// not report whether an instance's pod is up: a successful deployment can still fail to start, which the deployment
// resource itself reports.
//
// This is deliberately not a status condition: the synthetics-worker is an optional feature, and an issue with it
// neither makes the operator configuration resource unavailable nor degraded.
type SyntheticsWorkerStatus struct {
	// Deployed reports whether every configured instance was successfully created or updated the last time the
	// operator tried. It is false if the feature is disabled, or if any single instance failed.
	Deployed bool `json:"deployed"`

	// Reason is a programmatic identifier for the last negative outcome, e.g. "NoAuthorizationToken". When multiple
	// instances fail for different reasons, this reports "PartiallyDeployed"; see Instances for the per-instance
	// reasons.
	//
	// +kubebuilder:validation:Optional
	Reason string `json:"reason,omitempty"`

	// Message describes the last outcome in a human-readable form.
	//
	// +kubebuilder:validation:Optional
	Message string `json:"message,omitempty"`

	// LastTransitionTime is the time at which Deployed last changed.
	//
	// +kubebuilder:validation:Optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`

	// Instances reports the individual outcome for each configured synthetics-worker instance.
	//
	// +kubebuilder:validation:Optional
	// +listType=map
	// +listMapKey=locationId
	Instances []SyntheticsWorkerInstanceStatus `json:"instances,omitempty"`
}

// SyntheticsWorkerInstanceStatus reports whether the operator has deployed one synthetics-worker instance.
type SyntheticsWorkerInstanceStatus struct {
	// LocationID identifies the instance this status refers to, matching spec.syntheticsWorker.instances[].locationId.
	LocationID string `json:"locationId"`

	// Deployed reports whether the operator has successfully created or updated this instance's resources the last
	// time it tried.
	Deployed bool `json:"deployed"`

	// Reason is a programmatic identifier for the last negative outcome, e.g. "NoAuthorizationToken".
	//
	// +kubebuilder:validation:Optional
	Reason string `json:"reason,omitempty"`

	// Message describes the last outcome in a human-readable form.
	//
	// +kubebuilder:validation:Optional
	Message string `json:"message,omitempty"`

	// LastTransitionTime is the time at which Deployed last changed for this instance.
	//
	// +kubebuilder:validation:Optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// SetSyntheticsWorkerStatus records the outcome of the last attempt to create or update every synthetics-worker
// instance. It reports whether the recorded state changed, so that the caller only queues a Kubernetes event on a
// transition instead of on every reconciliation.
//
// The aggregate Deployed is true only if every instance deployed. A change is the aggregate Deployed flipping, the
// status appearing for the first time, the aggregate reason changing while not deployed, or any single instance's
// Deployed/Reason changing - an operator who fixes one misconfiguration and runs into the next one has to learn about
// the second one as well. LastTransitionTime (aggregate and per-instance) only advances when that entry's Deployed
// flips, mirroring the semantics of a status condition.
func (d *Dash0OperatorConfiguration) SetSyntheticsWorkerStatus(instances []SyntheticsWorkerInstanceStatus) bool {
	previous := d.Status.SyntheticsWorker

	now := metav1.Now()
	previousByLocationID := map[string]SyntheticsWorkerInstanceStatus{}
	if previous != nil {
		for _, instance := range previous.Instances {
			previousByLocationID[instance.LocationID] = instance
		}
	}

	changed := previous == nil || len(previous.Instances) != len(instances)
	aggregateDeployed := len(instances) > 0
	firstFailureReason := ""
	firstFailureMessage := ""
	for i, instance := range instances {
		if previousInstance, ok := previousByLocationID[instance.LocationID]; ok {
			if previousInstance.Deployed == instance.Deployed {
				instance.LastTransitionTime = previousInstance.LastTransitionTime
			} else {
				changed = true
			}
			if !instance.Deployed && previousInstance.Reason != instance.Reason {
				changed = true
			}
		} else {
			changed = true
			instance.LastTransitionTime = now
		}
		instances[i] = instance

		if !instance.Deployed {
			aggregateDeployed = false
			if firstFailureReason == "" {
				firstFailureReason = instance.Reason
				firstFailureMessage = instance.Message
			} else if firstFailureReason != instance.Reason {
				firstFailureReason = "PartiallyDeployed"
				firstFailureMessage = "some synthetics-worker instances failed to deploy for different reasons"
			}
		}
	}

	aggregateReason := "Deployed"
	aggregateMessage := "The operator has deployed the synthetics-worker."
	switch {
	case len(instances) == 0:
		aggregateReason = "NoInstancesConfigured"
		aggregateMessage = "The synthetics-worker is enabled but spec.syntheticsWorker.instances is empty."
	case !aggregateDeployed:
		aggregateReason = firstFailureReason
		aggregateMessage = firstFailureMessage
	}

	previousDeployed := previous != nil && previous.Deployed
	if previousDeployed != aggregateDeployed {
		changed = true
	}
	if previous != nil && !aggregateDeployed && previous.Reason != aggregateReason {
		changed = true
	}

	lastTransitionTime := now
	if previous != nil && previousDeployed == aggregateDeployed {
		lastTransitionTime = previous.LastTransitionTime
	}
	d.Status.SyntheticsWorker = &SyntheticsWorkerStatus{
		Deployed:           aggregateDeployed,
		Reason:             aggregateReason,
		Message:            aggregateMessage,
		LastTransitionTime: lastTransitionTime,
		Instances:          instances,
	}
	return changed
}

// SetSyntheticsWorkerDisabledStatus records that the synthetics-worker feature itself is disabled (as opposed to
// enabled but partially or fully failing to deploy, which is reported via SetSyntheticsWorkerStatus). It reports
// whether the recorded state changed, using the same semantics as SetSyntheticsWorkerStatus.
func (d *Dash0OperatorConfiguration) SetSyntheticsWorkerDisabledStatus(reason string, message string) bool {
	previous := d.Status.SyntheticsWorker
	changed := previous == nil || previous.Deployed || previous.Reason != reason || len(previous.Instances) != 0

	lastTransitionTime := metav1.Now()
	if previous != nil && !previous.Deployed {
		lastTransitionTime = previous.LastTransitionTime
	}
	d.Status.SyntheticsWorker = &SyntheticsWorkerStatus{
		Deployed:           false,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: lastTransitionTime,
	}
	return changed
}

//+kubebuilder:object:root=true

// Dash0OperatorConfigurationList contains a list of Dash0OperatorConfiguration resources.
type Dash0OperatorConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0OperatorConfiguration `json:"items"`
}
