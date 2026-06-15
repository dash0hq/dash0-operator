// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"strings"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
)

type Action string

const (
	ActionInstrumentation   Action = "Instrumentation"
	ActionUninstrumentation Action = "Uninstrumentation"
)

type Reason string

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

var AllEvents = []Reason{
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
	Agent0ConnectorEnabled                 bool
	SendBatchSize                          *uint32
	SendBatchMaxSize                       *uint32
	K8sAttributesDisableReplicasetInformer bool
	K8sAttributesWaitForMetadata           bool
	K8sAttributesWaitForMetadataTimeout    string
	NodeIp                                 string
	NodeName                               string
	PseudoClusterUid                       types.UID
	IsIPv6Cluster                          bool
	IsDocker                               bool
	DisableHostPorts                       bool
	IsGkeAutopilot                         bool
	DevelopmentMode                        bool
	DebugVerbosityDetailed                 bool
	EnableProfExtension                    bool
	CompressConfigMap                      bool
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
	// PseudoClusterUid is the UID of the default namespace (equal to the k8s.cluster.uid resource attribute). The
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
	TargetAllocatorImage                        string
	TargetAllocatorPullPolicy                   corev1.PullPolicy
	ConfigurationReloaderImage                  string
	ConfigurationReloaderImagePullPolicy        corev1.PullPolicy
	FilelogOffsetSyncImage                      string
	FilelogOffsetSyncImagePullPolicy            corev1.PullPolicy
	FilelogOffsetVolumeOwnershipImage           string
	FilelogOffsetVolumeOwnershipImagePullPolicy corev1.PullPolicy
	BarkerImage                                 string
	BarkerImagePullPolicy                       corev1.PullPolicy
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

// All returns all possible collector base URLs as a slice.
func (u PossibleCollectorUrls) All() []string {
	return []string{u.NodeLocalBaseUrl, u.ServiceBaseUrl}
}

// ClusterInstrumentationConfig holds configuration values relevant for instrumenting workloads which apply to the whole
// cluster, e.g. settings from the helm chart or the operator configuration resource.
type ClusterInstrumentationConfig struct {
	Images
	PossibleCollectorUrls PossibleCollectorUrls
	OTelCollectorBaseUrl  string
	ExtraConfig           atomic.Pointer[ExtraConfig]

	// KubernetesVersion holds the Kubernetes version of the cluster detected at operator manager startup.
	// KubernetesVersionDetected indicates whether detection succeeded; if false, KubernetesVersion is the zero value.
	KubernetesVersion         cluster.KubernetesVersionInfo
	KubernetesVersionDetected bool

	// ResolvedDelivery is the resolved decision (based on the operator configuration's
	// spec.instrumentWorkloads.instrumentationDelivery and the Kubernetes version) on whether Kubernetes image volumes
	// or the legacy init container plus emptyDir volume approach is used for instrumentation delivery. The value is
	// initialized at operator manager startup from the --operator-configuration-instrumentation-delivery flag (if
	// available) and may be updated at runtime by the operator configuration reconciler when
	// spec.instrumentWorkloads.instrumentationDelivery is changed.
	ResolvedDelivery atomic.Pointer[cluster.ResolvedInstrumentationDelivery]

	InstrumentationDelays           *DelayConfig
	InstrumentationDebug            bool
	EnablePythonAutoInstrumentation bool
}

func NewClusterInstrumentationConfig(
	images Images,
	possibleCollectorUrls PossibleCollectorUrls,
	oTelCollectorBaseUrl string,
	extraConfig ExtraConfig,
	instrumentationDelivery cluster.ResolvedInstrumentationDelivery,
	instrumentationDelays *DelayConfig,
	instrumentationDebug bool,
	enablePythonAutoInstrumentation bool,
) *ClusterInstrumentationConfig {
	c := &ClusterInstrumentationConfig{
		Images:                          images,
		PossibleCollectorUrls:           possibleCollectorUrls,
		OTelCollectorBaseUrl:            oTelCollectorBaseUrl,
		InstrumentationDelays:           instrumentationDelays,
		InstrumentationDebug:            instrumentationDebug,
		EnablePythonAutoInstrumentation: enablePythonAutoInstrumentation,
	}
	c.ExtraConfig.Store(&extraConfig)
	c.ResolvedDelivery.Store(&instrumentationDelivery)
	return c
}

// SetKubernetesVersion records the detected Kubernetes version on the config. Intended to be called once at
// operator manager startup, before the config is shared with reconcilers and webhooks.
func (c *ClusterInstrumentationConfig) SetKubernetesVersion(info cluster.KubernetesVersionInfo, detected bool) {
	c.KubernetesVersion = info
	c.KubernetesVersionDetected = detected
}

// SetInstrumentationDelivery sets the resolved instrumentation delivery mechanism. The previously stored value is
// returned for logging purposes.
func (c *ClusterInstrumentationConfig) SetInstrumentationDelivery(instrumentationDelivery cluster.ResolvedInstrumentationDelivery) cluster.ResolvedInstrumentationDelivery {
	previous := c.ResolvedDelivery.Swap(&instrumentationDelivery)
	if previous == nil {
		return ""
	}
	return *previous
}

func (c *ClusterInstrumentationConfig) IsInstrumentationDeliveryInitContainer() bool {
	return !c.IsInstrumentationDeliveryImageVolume()
}

func (c *ClusterInstrumentationConfig) IsInstrumentationDeliveryImageVolume() bool {
	delivery := c.ResolvedDelivery.Load()
	return delivery != nil && *delivery == cluster.ResolvedInstrumentationDeliveryImageVolume
}

func (c *ClusterInstrumentationConfig) GetInstrumentationDelivery() cluster.ResolvedInstrumentationDelivery {
	delivery := c.ResolvedDelivery.Load()
	if delivery == nil {
		return ""
	}
	return *delivery
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
