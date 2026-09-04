// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type Action string

const (
	ActionInstrumentation       Action = "Instrumentation"
	ActionUninstrumentation     Action = "Uninstrumentation"
	ActionAgent0ConnectorDeploy Action = "Agent0ConnectorDeployment"
)

type Reason string

// Instrumentation-related event reasons:
const (
	ReasonSuccessfulInstrumentation              Reason = "SuccessfulInstrumentation"
	ReasonPartiallyUnsuccessfulInstrumentation   Reason = "PartiallyUnsuccessfulInstrumentation"
	ReasonNoInstrumentationNecessary             Reason = "AlreadyInstrumented"
	ReasonFailedInstrumentation                  Reason = "FailedInstrumentation"
	ReasonSuccessfulUninstrumentation            Reason = "SuccessfulUninstrumentation"
	ReasonPartiallyUnsuccessfulUninstrumentation Reason = "PartiallyUnsuccessfulUninstrumentation"
	ReasonNoUninstrumentationNecessary           Reason = "AlreadyNotInstrumented"
	ReasonFailedUninstrumentation                Reason = "FailedUninstrumentation"
)

// Event reasons unrelated to workload instrumentation:
const (
	ReasonAgent0ConnectorDeployed    Reason = "Agent0ConnectorDeployed"
	ReasonAgent0ConnectorNotDeployed Reason = "Agent0ConnectorNotDeployed"
	ReasonAgent0ConnectorDisabled    Reason = "Agent0ConnectorDisabled"
)

// AllInstrumentationEvents lists the events the instrumentation webhook queues for a workload. The webhook cannot set
// the involved object's UID, so MonitoringReconciler#attachDanglingEvents looks them up by reason and attaches them
// afterwards.
var AllInstrumentationEvents = []Reason{
	ReasonSuccessfulInstrumentation,
	ReasonPartiallyUnsuccessfulInstrumentation,
	ReasonNoInstrumentationNecessary,
	ReasonFailedInstrumentation,
	ReasonSuccessfulUninstrumentation,
	ReasonPartiallyUnsuccessfulUninstrumentation,
	ReasonNoUninstrumentationNecessary,
	ReasonFailedUninstrumentation,
}

type CollectorConfig struct {
	Images            Images
	OperatorNamespace string
	// OTelCollectorNamePrefix is used as a prefix for OTel collector Kubernetes resources created by the operator, set
	// to value of the environment variable OTEL_COLLECTOR_NAME_PREFIX, which is set to the Helm release name by the
	// operator Helm chart.
	OTelCollectorNamePrefix string
	// The collector needs to know about the target-allocator name prefix, so it can build the service name needed for the
	// config of the prometheus_receiver
	TargetAllocatorNamePrefix              string
	Agent0ConnectorEnabledViaHelm          bool
	SendBatchSize                          *uint32
	SendBatchMaxSize                       *uint32
	K8sAttributesDisableReplicasetInformer bool
	K8sAttributesWaitForMetadata           bool
	K8sAttributesWaitForMetadataTimeout    string
	NodeIp                                 string
	NodeName                               string
	// KubeletStatsAutoDetectEndpoint controls whether the operator probes the node's kubelet at startup to determine the
	// kubeletstats receiver endpoint and TLS mode automatically. It is set from the Helm value
	// operator.collectors.kubeletstats.autoDetectEndpoint (default true). When false, KubeletStatsReceiverConfig is used
	// to configure the kubeletstats receiver instead of probing.
	KubeletStatsAutoDetectEndpoint bool
	// KubeletStatsReceiverConfig is the fixed kubeletstats receiver configuration provided via the Helm chart
	// (operator.collectors.kubeletstats.endpoint / authType / insecureSkipVerify). It is only set (non-nil) when
	// KubeletStatsAutoDetectEndpoint is false and is used verbatim instead of probing the node's kubelet.
	KubeletStatsReceiverConfig *KubeletStatsReceiverConfig
	PseudoClusterUid           types.UID
	// KubernetesApiServerVersion is the Kubernetes API server version, detected at operator manager startup; its
	// Detected flag reports whether that detection succeeded. Used to decide whether the operator's services can carry
	// spec.trafficDistribution.
	KubernetesApiServerVersion cluster.KubernetesVersionInfo
	IsIPv6Cluster              bool
	IsDocker                   bool
	DisableHostPorts           bool
	// OtlpGrpcHostPort and OtlpHttpHostPort are the host ports the collector DaemonSet pods use for the gRPC/HTTP OTLP
	// receivers, set from the Helm values operator.collectors.otlpGrpcHostPort / otlpHttpHostPort via the CLI flags
	// --dash0-otel-collector-otlp-grpc-host-port / --dash0-otel-collector-otlp-http-host-port. They only take effect
	// when DisableHostPorts is false.
	OtlpGrpcHostPort       int32
	OtlpHttpHostPort       int32
	IsGkeAutopilot         bool
	DevelopmentMode        bool
	DebugVerbosityDetailed bool
	EnableProfExtension    bool
	CompressConfigMap      bool
}

// KubeletStatsReceiverConfig holds the configuration for the kubeletstats receiver in the DaemonSet collector. It is
// either determined automatically by probing the node's kubelet (see the otelcolresources package) or provided
// explicitly via the Helm chart when endpoint auto-detection is disabled.
type KubeletStatsReceiverConfig struct {
	Enabled            bool
	Endpoint           string
	AuthType           string
	InsecureSkipVerify bool
}

type TargetAllocatorConfig struct {
	Images            Images
	OperatorNamespace string
	// TargetAllocatorNamePrefix is used as a prefix for OTel target-allocator Kubernetes resources created by the operator, set
	// to value of the environment variable OTEL_TARGET_ALLOCATOR_NAME_PREFIX, which is set to the Helm release name by the
	// operator Helm chart.
	TargetAllocatorNamePrefix string
	// CollectorComponent is used as a label matcher, so scrape targets are only assigned to Dash0 daemonset collectors.
	CollectorComponent string
	IsGkeAutopilot     bool
	DevelopmentMode    bool
}

type Agent0ConnectorConfig struct {
	Images            Images
	OperatorNamespace string
	// NamePrefix is used as a prefix for the agent0-connector Kubernetes resources created by the operator. It is the
	// same prefix that is used for the collector workloads and the target-allocator, that is, the Helm release name.
	NamePrefix string
	// PseudoClusterUid is the UID of the kube-system namespace (equal to the k8s.cluster.uid resource attribute). The
	// agent0-connector workload uses it as its client ID when connecting to the Dash0 backend.
	PseudoClusterUid types.UID
	// ServerAddress is the address of the Dash0 backend service the agent0-connector workload connects to. It is set
	// from the Helm value operator.agent0Connector.serverAddress and passed to the workload via the
	// DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS environment variable.
	ServerAddress string
	// Insecure disables TLS for the agent0-connector workload's connection to the Dash0 backend. It is set from the Helm
	// value operator.agent0Connector.insecure and passed to the workload via the DASH0_AGENT0_CONNECTOR_INSECURE
	// environment variable. It is only intended for local development.
	Insecure bool
	// Authorization holds the Dash0 authorization token for the agent0-connector workload, either as a literal token (set
	// from the Helm value operator.agent0Connector.token) or as a reference to a Kubernetes secret (set from the Helm
	// value operator.agent0Connector.secretRef). It is passed to the workload via the DASH0_AGENT0_CONNECTOR_AUTH_TOKEN
	// environment variable.
	Authorization   dash0common.Authorization
	DevelopmentMode bool
}

type Images struct {
	OperatorImage                               string
	InitContainerImage                          string
	InitContainerImagePullPolicy                corev1.PullPolicy
	CollectorImage                              string
	CollectorImagePullPolicy                    corev1.PullPolicy
	SignalControlCollectorImage                 string
	SignalControlCollectorImagePullPolicy       corev1.PullPolicy
	TargetAllocatorImage                        string
	TargetAllocatorPullPolicy                   corev1.PullPolicy
	ConfigurationReloaderImage                  string
	ConfigurationReloaderImagePullPolicy        corev1.PullPolicy
	FilelogOffsetSyncImage                      string
	FilelogOffsetSyncImagePullPolicy            corev1.PullPolicy
	FilelogOffsetVolumeOwnershipImage           string
	FilelogOffsetVolumeOwnershipImagePullPolicy corev1.PullPolicy
	EdgeProxyImage                              string
	EdgeProxyImagePullPolicy                    corev1.PullPolicy
	Agent0ConnectorImage                        string
	Agent0ConnectorImagePullPolicy              corev1.PullPolicy
}

func (i Images) GetOperatorVersion() string {
	return getImageVersion(i.OperatorImage)
}

func getImageVersion(image string) string {
	idx := strings.LastIndex(image, "@")
	if idx >= 0 {
		return image[idx+1:]
	}
	idx = strings.LastIndex(image, ":")
	if idx >= 0 {
		return image[idx+1:]
	}
	return ""
}

// PossibleCollectorUrls holds the two possible base URLs for routing telemetry from instrumented workloads to the
// OpenTelemetry collector daemonset: the service URL of the collector DaemonSet and the node-local URL (node IP plus
// host port). The actual URL used for instrumentation is selected from these two values depending on the cluster setup.
type PossibleCollectorUrls struct {
	NodeLocalBaseUrl string
	ServiceBaseUrl   string
}

// ClusterInstrumentationConfig holds configuration values relevant for instrumenting workloads which apply to the whole
// cluster, e.g. settings from the helm chart or the operator configuration resource.
type ClusterInstrumentationConfig struct {
	Images
	PossibleCollectorUrls PossibleCollectorUrls
	OTelCollectorBaseUrl  string
	ExtraConfig           atomic.Pointer[ExtraConfig]

	// kubernetesApiServerVersion holds the Kubernetes API server version, detected at operator manager startup. Its
	// Detected flag indicates whether that detection succeeded.
	kubernetesApiServerVersion cluster.KubernetesVersionInfo

	// minimumKubeletVersionDetector provides the minimum kubelet version among the cluster's nodes, which is determined
	// asynchronously after the operator manager has started.
	minimumKubeletVersionDetector *cluster.MinimumKubeletVersionDetector

	// requestedInstrumentationDelivery is the unresolved value of the operator configuration's
	// spec.instrumentWorkloads.instrumentationDelivery setting. It is initialized at operator manager startup from the
	// --operator-configuration-instrumentation-delivery flag (if available) and may be updated at runtime by the
	// operator configuration reconciler when spec.instrumentWorkloads.instrumentationDelivery is changed. The decision
	// whether Kubernetes image volumes or the legacy init container plus emptyDir volume approach is used is derived
	// from this value on demand, see ResolveInstrumentationDelivery.
	requestedInstrumentationDelivery atomic.Pointer[dash0v1alpha1.InstrumentationDelivery]

	InstrumentationDelays           *DelayConfig
	InstrumentationDebug            bool
	EnablePythonAutoInstrumentation bool
	EnableRubyAutoInstrumentation   bool
}

func NewClusterInstrumentationConfig(
	images Images,
	possibleCollectorUrls PossibleCollectorUrls,
	oTelCollectorBaseUrl string,
	extraConfig ExtraConfig,
	requestedInstrumentationDelivery dash0v1alpha1.InstrumentationDelivery,
	instrumentationDelays *DelayConfig,
	instrumentationDebug bool,
	enablePythonAutoInstrumentation bool,
	enableRubyAutoInstrumentation bool,
) *ClusterInstrumentationConfig {
	c := &ClusterInstrumentationConfig{
		Images:                          images,
		PossibleCollectorUrls:           possibleCollectorUrls,
		OTelCollectorBaseUrl:            oTelCollectorBaseUrl,
		InstrumentationDelays:           instrumentationDelays,
		InstrumentationDebug:            instrumentationDebug,
		EnablePythonAutoInstrumentation: enablePythonAutoInstrumentation,
		EnableRubyAutoInstrumentation:   enableRubyAutoInstrumentation,
	}
	c.ExtraConfig.Store(&extraConfig)
	c.requestedInstrumentationDelivery.Store(&requestedInstrumentationDelivery)
	return c
}

// SetKubernetesVersions stores the detected Kubernetes API server version and the detector for the minimum kubelet
// version in the ClusterInstrumentationConfig. Intended to be called once at operator manager startup, before the
// config is shared with reconcilers and webhooks.
func (c *ClusterInstrumentationConfig) SetKubernetesVersions(
	apiServerVersion cluster.KubernetesVersionInfo,
	minimumKubeletVersionDetector *cluster.MinimumKubeletVersionDetector,
) {
	c.kubernetesApiServerVersion = apiServerVersion
	c.minimumKubeletVersionDetector = minimumKubeletVersionDetector
}

// WaitForInstrumentationDeliveryAutoToBeResolved returns the instrumentation delivery mechanism to use, waiting for the
// minimum kubelet version detection first if the outcome depends on it, e.g. if the requested mode is "auto". Use it
// before instrumenting workloads in bulk: while the detection is still in progress, "auto" resolves to the
// init-container fallback, and workloads that have been instrumented already are not re-instrumented when the resolved
// mechanism changes later. It gives up waiting after the given timeout and then returns the mechanism that is effective
// at that point.
//
// If the requested delivery is not "auto", the method will not wait.
func (c *ClusterInstrumentationConfig) WaitForInstrumentationDeliveryAutoToBeResolved(
	ctx context.Context,
	timeout time.Duration,
) cluster.ResolvedInstrumentationDelivery {
	requestedInstrumentationDelivery := c.requestedInstrumentationDelivery.Load()
	if requestedInstrumentationDelivery != nil &&
		*requestedInstrumentationDelivery == dash0v1alpha1.InstrumentationDeliveryAuto {
		c.minimumKubeletVersionDetector.WaitForDetection(ctx, timeout)
	}
	return c.ResolveInstrumentationDelivery()
}

// UpdateRequestedInstrumentationDelivery stores the requested (unresolved) instrumentation delivery setting that has
// been read from the Dash0OperatorConfiguration resource. It returns the resolved delivery mechanism that has been
// effective before the update and the new resolved mechanism that is effective now, after updating it.
func (c *ClusterInstrumentationConfig) UpdateRequestedInstrumentationDelivery(
	requestedInstrumentationDelivery dash0v1alpha1.InstrumentationDelivery,
	logWarningForEmptyValue bool,
	logger logd.Logger,
) (cluster.ResolvedInstrumentationDelivery, cluster.ResolvedInstrumentationDelivery) {
	previous := c.ResolveInstrumentationDelivery()
	c.requestedInstrumentationDelivery.Store(&requestedInstrumentationDelivery)
	return previous, c.resolveInstrumentationDelivery(logWarningForEmptyValue, logger)
}

// ResolveInstrumentationDelivery resolves the stored requested instrumentation delivery setting to the effective
// delivery mechanism depending on the current knowledge about the Kubernetes API server version and the minimum kubelet
// version among all nodes of the cluster.
//
// This is derived on demand, because the minimum kubelet version is detected asynchronously after the operator manager
// has started. The mode "auto" resolves to init-container until all nodes have been inspected, and potentially to
// image-volumes afterwards (if all kubelets are recent enough).
func (c *ClusterInstrumentationConfig) ResolveInstrumentationDelivery() cluster.ResolvedInstrumentationDelivery {
	return c.resolveInstrumentationDelivery(false, logd.Discard())
}

func (c *ClusterInstrumentationConfig) resolveInstrumentationDelivery(
	logWarningForEmptyValue bool,
	logger logd.Logger,
) cluster.ResolvedInstrumentationDelivery {
	requestedInstrumentationDelivery := c.requestedInstrumentationDelivery.Load()
	if requestedInstrumentationDelivery == nil {
		return cluster.ResolvedInstrumentationDeliveryInitContainer
	}
	return cluster.ResolveInstrumentationDelivery(
		*requestedInstrumentationDelivery,
		c.kubernetesApiServerVersion,
		c.minimumKubeletVersionDetector.Get(),
		logWarningForEmptyValue,
		logger,
	)
}

type DelayConfig struct {
	// AfterEachWorkloadMillis determines the delay to wait after updating a single workload, when instrumenting
	// workloads in a namespace either when running InstrumentAtStartup or when instrumentation is enabled for a new
	// workspace via a monitoring resource.
	AfterEachWorkloadMillis uint64

	// AfterEachNamespace determines the delay to wait after updating the instrumentation in one namespace when running
	// InstrumentAtStartup.
	AfterEachNamespaceMillis uint64
}

type ModificationMode string

const (
	ModificationModeInstrumentation   ModificationMode = "instrumentation"
	ModificationModeUninstrumentation ModificationMode = "uninstrumentation"
)

type DanglingEventsTimeouts struct {
	InitialTimeout time.Duration
	Backoff        wait.Backoff
}
