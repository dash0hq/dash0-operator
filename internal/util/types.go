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
	TargetAllocatorNamePrefix string
	SendBatchMaxSize          *uint32
	DisableReplicasetInformer bool
	NodeIp                    string
	NodeName                  string
	PseudoClusterUid          types.UID
	IsIPv6Cluster             bool
	IsDocker                  bool
	DisableHostPorts          bool
	IsGkeAutopilot            bool
	DevelopmentMode           bool
	DebugVerbosityDetailed    bool
	EnableProfExtension       bool
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
	DevelopmentMode    bool
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

// ClusterInstrumentationConfig holds configuration values relevant for instrumenting workloads which apply to the whole
// cluster, e.g. settings from the helm chart or the operator configuration resource.
type ClusterInstrumentationConfig struct {
	Images
	OTelCollectorBaseUrl            string
	ExtraConfig                     atomic.Pointer[ExtraConfig]
	InstrumentationDelays           *DelayConfig
	InstrumentationDebug            bool
	EnablePythonAutoInstrumentation bool
}

func NewClusterInstrumentationConfig(
	images Images,
	oTelCollectorBaseUrl string,
	extraConfig ExtraConfig,
	instrumentationDelays *DelayConfig,
	instrumentationDebug bool,
	enablePythonAutoInstrumentation bool,
) *ClusterInstrumentationConfig {
	c := &ClusterInstrumentationConfig{
		Images:                          images,
		OTelCollectorBaseUrl:            oTelCollectorBaseUrl,
		InstrumentationDelays:           instrumentationDelays,
		InstrumentationDebug:            instrumentationDebug,
		EnablePythonAutoInstrumentation: enablePythonAutoInstrumentation,
	}
	c.ExtraConfig.Store(&extraConfig)
	return c
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

// NamespaceInstrumentationConfig holds configuration values relevant for instrumenting workloads which apply to one
// namespace, e.g. settings from the monitoring resource.
type NamespaceInstrumentationConfig struct {
	InstrumentationLabelSelector    string
	TraceContextPropagators         *string
	PreviousTraceContextPropagators *string
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
