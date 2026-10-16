// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

// OperatorConfigurationComponents contains the optional settings for the managed workloads.
type OperatorConfigurationComponents struct {
	// Settings for the OpenTelemetry collector DaemonSet.
	//
	// +kubebuilder:validation:Optional
	DaemonSetCollector *DaemonSetCollectorSettings `json:"daemonSetCollector,omitempty"`

	// Settings for the OpenTelemetry collector Deployment (cluster-metrics-collector).
	//
	// +kubebuilder:validation:Optional
	DeploymentCollector *DeploymentCollectorSettings `json:"deploymentCollector,omitempty"`

	// Settings for the OpenTelemetry target-allocator.
	//
	// +kubebuilder:validation:Optional
	TargetAllocator *TargetAllocatorSettings `json:"targetAllocator,omitempty"`

	// Settings for the agent0-connector. Its cluster role rules can only be configured via the Helm chart, since the
	// operator can only grant permissions it holds itself. Its allowed kubectl commands can also only be configured via
	// the Helm chart (operator.agent0Connector.allowedKubectlCommands).
	//
	// +kubebuilder:validation:Optional
	Agent0Connector *Agent0ConnectorSettings `json:"agent0Connector,omitempty"`
}

// SignalControlComponents contains the optional settings for the managed Signal Control workloads.
type SignalControlComponents struct {
	// Settings for the Signal Control collector Deployment.
	//
	// +kubebuilder:validation:Optional
	Collector *SignalControlCollectorSettings `json:"collector,omitempty"`

	// Settings for the Edge Proxy Deployment.
	//
	// +kubebuilder:validation:Optional
	EdgeProxy *EdgeProxySettings `json:"edgeProxy,omitempty"`
}

// WorkloadSettings are the metadata settings of a workload that the operator manages.
type WorkloadSettings struct {
	// Additional labels for the workload. They replace the labels provided via the Helm chart, empty labels keep them.
	//
	// +kubebuilder:validation:Optional
	Labels map[string]string `json:"labels,omitempty"`

	// Additional annotations for the workload. They replace the annotations provided via the Helm chart, empty
	// annotations keep them.
	//
	// +kubebuilder:validation:Optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Additional labels for the pods of the workload. They replace the pod labels provided via the Helm chart, empty
	// pod labels keep them.
	//
	// +kubebuilder:validation:Optional
	PodLabels map[string]string `json:"podLabels,omitempty"`

	// Additional annotations for the pods of the workload. They replace the pod annotations provided via the Helm
	// chart, empty pod annotations keep them.
	//
	// +kubebuilder:validation:Optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
}

// DaemonSetCollectorSettings are the settings for the OpenTelemetry collector DaemonSet.
type DaemonSetCollectorSettings struct {
	WorkloadSettings `json:",inline"`
}

// DeploymentCollectorSettings are the settings for the OpenTelemetry collector Deployment (cluster-metrics-collector).
type DeploymentCollectorSettings struct {
	WorkloadSettings `json:",inline"`
}

// TargetAllocatorSettings are the settings for the OpenTelemetry target-allocator.
type TargetAllocatorSettings struct {
	WorkloadSettings `json:",inline"`
}

// Agent0ConnectorSettings are the settings for the agent0-connector Deployment.
type Agent0ConnectorSettings struct {
	WorkloadSettings `json:",inline"`
}

// SignalControlCollectorSettings are the settings for the Signal Control collector Deployment.
type SignalControlCollectorSettings struct {
	// The number of replicas of the Signal Control collector Deployment (Helm value
	// operator.collectors.signalControlCollectorReplicas, default 2).
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	WorkloadSettings `json:",inline"`
}

// EdgeProxySettings are the settings for the Edge Proxy Deployment.
type EdgeProxySettings struct {
	// The number of Edge Proxy pods to run (Helm value operator.signalControl.edgeProxy.replicas, default 2).
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Whether to enable pprof for the Edge Proxy pods (Helm value operator.signalControl.edgeProxy.enablePprof, default
	// false). Only enable this if instructed by Dash0 support.
	//
	// +kubebuilder:validation:Optional
	EnablePprof *bool `json:"enablePprof,omitempty"`

	WorkloadSettings `json:",inline"`
}
