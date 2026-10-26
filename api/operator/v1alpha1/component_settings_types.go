// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// OperatorConfigurationComponents contains the optional settings for the managed workloads.
type OperatorConfigurationComponents struct {
	// Settings for the init container that the operator adds to instrumented workloads.
	//
	// +kubebuilder:validation:Optional
	InstrumentationInitContainer *InstrumentationInitContainerSettings `json:"instrumentationInitContainer,omitempty"`

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
	// operator can only grant permissions it holds itself.
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

// ContainerResources are the resource requests and limits of a container.
type ContainerResources struct {
	// The resource limits. Only cpu, memory and ephemeral-storage are supported. A resource that is not set here keeps
	// its default limit, and -1 removes it. A default limit below the request set in requests is raised to that request.
	//
	// +kubebuilder:validation:Optional
	Limits corev1.ResourceList `json:"limits,omitempty"`

	// The resource requests. Only cpu, memory and ephemeral-storage are supported. A resource that is not set here keeps
	// its default request, and -1 removes it. A default request above the limit set in limits is lowered to that limit.
	// A request above the limit set in limits is rejected.
	//
	// +kubebuilder:validation:Optional
	Requests corev1.ResourceList `json:"requests,omitempty"`
}

// ContainerResourcesWithGoMemLimit are the resource requests and limits of a container that runs a Go process.
type ContainerResourcesWithGoMemLimit struct {
	ContainerResources `json:",inline"`

	// The GOMEMLIMIT environment variable, in the format of the Go runtime (e.g. 400MiB, not 400Mi), or off. If it is
	// empty (recommended), the operator derives it from the memory limit. It must be greater than 0 and below the
	// effective memory limit. In collectorContainerResources, it should also be below the memory_limiter soft limit.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^(off|[0-9]+(([KMGT]i)?B)?)$`
	GoMemLimit string `json:"gomemlimit,omitempty"`
}

// ProbeSettings are the timing settings of a probe.
type ProbeSettings struct {
	// The number of seconds after the container has started before the probe is initiated.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	InitialDelaySeconds *int32 `json:"initialDelaySeconds,omitempty"`

	// The number of seconds after which the probe times out.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`

	// How often (in seconds) to perform the probe.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	PeriodSeconds *int32 `json:"periodSeconds,omitempty"`

	// The minimum number of consecutive successes for the probe to be considered successful after having failed. Must
	// be 1 for the liveness and the startup probe.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	SuccessThreshold *int32 `json:"successThreshold,omitempty"`

	// The minimum number of consecutive failures for the probe to be considered failed after having succeeded.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	FailureThreshold *int32 `json:"failureThreshold,omitempty"`

	// The duration in seconds the pod needs to terminate gracefully upon probe failure, overriding the
	// terminationGracePeriodSeconds of the pod. Must not be set for the readiness probe.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
}

// CollectorProbeSettings are the timing settings of the probes of an OpenTelemetry collector container.
//
// +kubebuilder:validation:XValidation:rule="!has(self.liveness) || !has(self.liveness.successThreshold) || self.liveness.successThreshold == 1",message="liveness.successThreshold must be 1"
// +kubebuilder:validation:XValidation:rule="!has(self.startup) || !has(self.startup.successThreshold) || self.startup.successThreshold == 1",message="startup.successThreshold must be 1"
// +kubebuilder:validation:XValidation:rule="!has(self.readiness) || !has(self.readiness.terminationGracePeriodSeconds)",message="readiness.terminationGracePeriodSeconds must not be set"
type CollectorProbeSettings struct {
	// The timing settings of the liveness probe.
	//
	// +kubebuilder:validation:Optional
	Liveness *ProbeSettings `json:"liveness,omitempty"`

	// The timing settings of the readiness probe.
	//
	// +kubebuilder:validation:Optional
	Readiness *ProbeSettings `json:"readiness,omitempty"`

	// The timing settings of the startup probe.
	//
	// +kubebuilder:validation:Optional
	Startup *ProbeSettings `json:"startup,omitempty"`
}

// WorkloadSettings are the metadata and scheduling settings of a workload that the operator manages.
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

	// Tolerations for the pods of the workload. They replace the tolerations provided via the Helm chart, empty
	// tolerations keep them. Only the operators Equal and Exists are supported.
	//
	// +kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// The node affinity for the pods of the workload. It is merged with the default node affinity of the Helm chart,
	// not with the one provided via Helm: requiredDuringSchedulingIgnoredDuringExecution and
	// preferredDuringSchedulingIgnoredDuringExecution each replace their default, unless they are absent. The default
	// requires nodes that are not labelled with dash0.com/enable=false and run Linux, and has no preferred node
	// affinity. requiredDuringSchedulingIgnoredDuringExecution with an empty list of nodeSelectorTerms removes the
	// required node affinity.
	//
	// +kubebuilder:validation:Optional
	NodeAffinity *corev1.NodeAffinity `json:"nodeAffinity,omitempty"`
}

// CollectorPodSettings are the pod settings that all OpenTelemetry collector workloads have in common.
type CollectorPodSettings struct {
	// Pod-level sysctls (spec.securityContext.sysctls). They replace the sysctls provided via the Helm chart, empty
	// sysctls keep them.
	//
	// +kubebuilder:validation:Optional
	Sysctls []corev1.Sysctl `json:"sysctls,omitempty"`

	// The timing settings of the liveness, readiness and startup probes. The operator always sets the probe handler
	// itself. The settings are merged with the default probes of the Helm chart per field, not with the probes provided
	// via Helm: a probe or a field that is not set here keeps its default. The defaults are initialDelaySeconds 0,
	// periodSeconds 2, timeoutSeconds 1, and failureThreshold 20 for the liveness, 3 for the readiness and 45 for the
	// startup probe.
	//
	// +kubebuilder:validation:Optional
	Probes *CollectorProbeSettings `json:"probes,omitempty"`

	// The priority class name for the pods. It replaces the one provided via the Helm chart, an empty value keeps it.
	//
	// +kubebuilder:validation:Optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
}

// InstrumentationInitContainerSettings are the settings for the instrumentation init container.
type InstrumentationInitContainerSettings struct {
	// The resources of the init container (Helm value operator.initContainerResources). Instrumented workloads get
	// changed resources on their next update, e.g. via kubectl rollout restart.
	//
	// +kubebuilder:validation:Optional
	Resources *ContainerResources `json:"resources,omitempty"`
}

// DaemonSetCollectorSettings are the settings for the OpenTelemetry collector DaemonSet.
type DaemonSetCollectorSettings struct {
	// The resources of the OpenTelemetry collector container (Helm value
	// operator.collectors.daemonSetCollectorContainerResources).
	//
	// +kubebuilder:validation:Optional
	CollectorContainerResources *ContainerResourcesWithGoMemLimit `json:"collectorContainerResources,omitempty"`

	// The resources of the configuration reloader container (Helm value
	// operator.collectors.daemonSetConfigurationReloaderContainerResources).
	//
	// +kubebuilder:validation:Optional
	ConfigurationReloaderContainerResources *ContainerResourcesWithGoMemLimit `json:"configurationReloaderContainerResources,omitempty"`

	// The resources of the filelog offset sync container and of the filelog offset sync init container (Helm value
	// operator.collectors.daemonSetFileLogOffsetSyncContainerResources).
	//
	// +kubebuilder:validation:Optional
	FilelogOffsetSyncContainerResources *ContainerResourcesWithGoMemLimit `json:"filelogOffsetSyncContainerResources,omitempty"`

	WorkloadSettings     `json:",inline"`
	CollectorPodSettings `json:",inline"`

	// Pod-level SELinux options (spec.securityContext.seLinuxOptions, Helm value
	// operator.collectors.daemonSetSeLinuxOptions). They replace the SELinux options provided via the Helm chart as a
	// whole, an empty object removes them. By default, the pods have no SELinux options.
	//
	// +kubebuilder:validation:Optional
	SELinuxOptions *corev1.SELinuxOptions `json:"seLinuxOptions,omitempty"`

	// A volume for the offsets of the filelog receiver (Helm value operator.collectors.filelogOffsetSyncStorageVolume),
	// as an alternative to the default ConfigMap-based storage, which is limited to 1 MB. It replaces the volume provided
	// via the Helm chart.
	//
	// +kubebuilder:validation:Optional
	FilelogOffsetStorageVolume *FilelogOffsetStorageVolume `json:"filelogOffsetStorageVolume,omitempty"`
}

// FilelogOffsetStorageVolume is the volume for the offsets of the filelog receiver. Exactly one of
// persistentVolumeClaim and hostPath must be set.
//
// +kubebuilder:validation:XValidation:rule="has(self.persistentVolumeClaim) != has(self.hostPath)",message="exactly one of persistentVolumeClaim and hostPath must be set"
type FilelogOffsetStorageVolume struct {
	// A persistent volume claim for the offsets. All pods of the DaemonSet mount the same claim, each one writes to its
	// own subdirectory, hence the claim needs to support the ReadWriteMany access mode when the cluster has more than one
	// node.
	//
	// +kubebuilder:validation:Optional
	PersistentVolumeClaim *FilelogOffsetStoragePersistentVolumeClaim `json:"persistentVolumeClaim,omitempty"`

	// A directory on the nodes for the offsets, mounted as a hostPath volume of type DirectoryOrCreate. Each pod of the
	// DaemonSet writes to a subdirectory named after its node.
	//
	// +kubebuilder:validation:Optional
	HostPath *FilelogOffsetStorageHostPath `json:"hostPath,omitempty"`
}

// FilelogOffsetStorageHostPath is a directory on the nodes.
type FilelogOffsetStorageHostPath struct {
	// The path of the directory on the nodes.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="!self.matches('(^|/)[.][.](/|$)')",message="path must not contain '..'"
	Path string `json:"path"`
}

// FilelogOffsetStoragePersistentVolumeClaim references a persistent volume claim in the operator's namespace.
type FilelogOffsetStoragePersistentVolumeClaim struct {
	// The name of the persistent volume claim in the operator's namespace.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ClaimName string `json:"claimName"`
}

// DeploymentCollectorSettings are the settings for the OpenTelemetry collector Deployment (cluster-metrics-collector).
type DeploymentCollectorSettings struct {
	// The resources of the OpenTelemetry collector container (Helm value
	// operator.collectors.deploymentCollectorContainerResources).
	//
	// +kubebuilder:validation:Optional
	CollectorContainerResources *ContainerResourcesWithGoMemLimit `json:"collectorContainerResources,omitempty"`

	// The resources of the configuration reloader container (Helm value
	// operator.collectors.deploymentConfigurationReloaderContainerResources).
	//
	// +kubebuilder:validation:Optional
	ConfigurationReloaderContainerResources *ContainerResourcesWithGoMemLimit `json:"configurationReloaderContainerResources,omitempty"`

	WorkloadSettings     `json:",inline"`
	CollectorPodSettings `json:",inline"`
}

// TargetAllocatorSettings are the settings for the OpenTelemetry target-allocator.
type TargetAllocatorSettings struct {
	// The resources of the target-allocator container (Helm value operator.targetAllocator.containerResources).
	//
	// +kubebuilder:validation:Optional
	ContainerResources *ContainerResourcesWithGoMemLimit `json:"containerResources,omitempty"`

	WorkloadSettings `json:",inline"`

	// Settings for mTLS between the target-allocator and the collectors (Helm value operator.targetAllocator.mTls). They
	// replace the mTLS settings provided via the Helm chart as a whole, an empty object disables mTLS.
	//
	// +kubebuilder:validation:Optional
	MTls *TargetAllocatorMTls `json:"mTls,omitempty"`

	// Whether the target-allocator serves the credentials for metrics endpoints which require authorization over plain
	// HTTP instead of mTLS. Only enable this if the transport security between the target-allocator and the collectors
	// is guaranteed by other means, for example by a service mesh (Helm value
	// operator.targetAllocator.allowInsecureAuthSecrets, default false).
	//
	// +kubebuilder:validation:Optional
	AllowInsecureAuthSecrets *bool `json:"allowInsecureAuthSecrets,omitempty"`
}

// TargetAllocatorMTls are the settings for mTLS between the target-allocator and the collectors.
//
// +kubebuilder:validation:XValidation:rule="!has(self.enabled) || !self.enabled || (has(self.serverCertSecretName) && size(self.serverCertSecretName) > 0 && has(self.clientCertSecretName) && size(self.clientCertSecretName) > 0)",message="serverCertSecretName and clientCertSecretName are required when mTLS is enabled"
type TargetAllocatorMTls struct {
	// Whether to enable mTLS between the target-allocator and the collectors. Defaults to false, also when mTLS is
	// enabled via Helm. Enabling mTLS requires serverCertSecretName and clientCertSecretName.
	//
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`

	// The name of the secret in the operator's namespace that contains the server certificate.
	//
	// +kubebuilder:validation:Optional
	ServerCertSecretName string `json:"serverCertSecretName,omitempty"`

	// The name of the secret in the operator's namespace that contains the client (collector) certificate.
	//
	// +kubebuilder:validation:Optional
	ClientCertSecretName string `json:"clientCertSecretName,omitempty"`
}

// Agent0ConnectorSettings are the settings for the agent0-connector Deployment.
type Agent0ConnectorSettings struct {
	// The number of command requests the agent0-connector executes at the same time (Helm value
	// operator.agent0Connector.maxConcurrentCommands, default 2). Raising this value requires raising
	// containerResources.limits.memory correspondingly, and containerResources.gomemlimit, if it is set.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	MaxConcurrentCommands *int32 `json:"maxConcurrentCommands,omitempty"`

	// The kubectl commands the agent0-connector may execute (Helm value operator.agent0Connector.allowedKubectlCommands).
	// The settings are merged with the defaults of the Helm chart per command, not with the commands provided via Helm:
	// a command that is not set here keeps its default. At least one command needs to stay enabled. Allowing a command
	// does not grant the RBAC permissions it needs (Helm value operator.agent0Connector.clusterRole.additionalRules or
	// operator.agent0Connector.clusterRole.rules).
	//
	// +kubebuilder:validation:Optional
	AllowedKubectlCommands *Agent0ConnectorAllowedKubectlCommands `json:"allowedKubectlCommands,omitempty"`

	// Whether to enable pprof for the agent0-connector pod (Helm value operator.agent0Connector.enablePprof, default
	// false). Only enable this if instructed by Dash0 support.
	//
	// +kubebuilder:validation:Optional
	EnablePprof *bool `json:"enablePprof,omitempty"`

	// The resources of the agent0-connector container (Helm value operator.agent0Connector.containerResources).
	//
	// +kubebuilder:validation:Optional
	ContainerResources *ContainerResourcesWithGoMemLimit `json:"containerResources,omitempty"`

	WorkloadSettings `json:",inline"`
}

// Agent0ConnectorAllowedKubectlCommands enables or disables the kubectl commands the agent0-connector may execute.
type Agent0ConnectorAllowedKubectlCommands struct {
	// kubectl api-resources, enabled by default.
	//
	// +kubebuilder:validation:Optional
	ApiResources *bool `json:"apiResources,omitempty"`

	// kubectl api-versions, enabled by default.
	//
	// +kubebuilder:validation:Optional
	ApiVersions *bool `json:"apiVersions,omitempty"`

	// kubectl auth, enabled by default.
	//
	// +kubebuilder:validation:Optional
	Auth *bool `json:"auth,omitempty"`

	// kubectl cluster-info, enabled by default.
	//
	// +kubebuilder:validation:Optional
	ClusterInfo *bool `json:"clusterInfo,omitempty"`

	// kubectl events, disabled by default. Events are not redacted, only enable this if no sensitive information like
	// credentials, API tokens, secrets or customer PII is leaked into event bodies. It needs RBAC access to events (core
	// API group) and events.events.k8s.io. While it is disabled, reading events via kubectl get is rejected as well.
	//
	// +kubebuilder:validation:Optional
	Events *bool `json:"events,omitempty"`

	// kubectl explain, enabled by default.
	//
	// +kubebuilder:validation:Optional
	Explain *bool `json:"explain,omitempty"`

	// kubectl get, enabled by default. It is the main kubectl command that Agent0 uses.
	//
	// +kubebuilder:validation:Optional
	Get *bool `json:"get,omitempty"`

	// kubectl logs, disabled by default. Logs cannot be redacted reliably, only enable this if no sensitive information is
	// leaked into logs. It needs RBAC access to pods/log, and also lifts the redaction of container termination messages
	// (which can hold log output) in the output of kubectl get.
	//
	// +kubebuilder:validation:Optional
	Logs *bool `json:"logs,omitempty"`

	// kubectl top, enabled by default.
	//
	// +kubebuilder:validation:Optional
	Top *bool `json:"top,omitempty"`

	// kubectl version, enabled by default.
	//
	// +kubebuilder:validation:Optional
	Version *bool `json:"version,omitempty"`
}

// SignalControlCollectorSettings are the settings for the Signal Control collector Deployment.
type SignalControlCollectorSettings struct {
	// The number of replicas of the Signal Control collector Deployment (Helm value
	// operator.collectors.signalControlCollectorReplicas, default 2).
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// The resources of the OpenTelemetry collector container (Helm value
	// operator.collectors.signalControlCollectorContainerResources). With the disk-backed trace reservoir, the operator
	// derives an ephemeral-storage request from spec.sampling.reservoir.maxDiskBytes and adds it if no request other
	// than 0 is set. A lower ephemeral-storage limit is rejected, since the reservoir counts towards the limit.
	//
	// +kubebuilder:validation:Optional
	CollectorContainerResources *ContainerResourcesWithGoMemLimit `json:"collectorContainerResources,omitempty"`

	// The resources of the configuration reloader container (Helm value
	// operator.collectors.signalControlCollectorConfigurationReloaderContainerResources).
	//
	// +kubebuilder:validation:Optional
	ConfigurationReloaderContainerResources *ContainerResourcesWithGoMemLimit `json:"configurationReloaderContainerResources,omitempty"`

	WorkloadSettings     `json:",inline"`
	CollectorPodSettings `json:",inline"`
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

	// The resources of the Edge Proxy container (Helm value operator.signalControl.edgeProxy.containerResources).
	//
	// +kubebuilder:validation:Optional
	ContainerResources *ContainerResourcesWithGoMemLimit `json:"containerResources,omitempty"`

	WorkloadSettings `json:",inline"`
}
