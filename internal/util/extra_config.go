// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"time"

	"github.com/bep/debounce"
	"github.com/fsnotify/fsnotify"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type ResourceRequirementsWithGoMemLimit struct {
	Limits     corev1.ResourceList `json:"limits,omitempty"`
	Requests   corev1.ResourceList `json:"requests,omitempty"`
	GoMemLimit string              `json:"gomemlimit,omitempty"`
}

type CollectorProbes struct {
	Liveness  corev1.Probe `json:"liveness"`
	Readiness corev1.Probe `json:"readiness"`
	Startup   corev1.Probe `json:"startup"`
}

// ExtraConfig holds the additional configuration values for the operator, which the operator reads from
// /etc/config/extra.yaml at startup, mostly Kubernetes-related settings like resource requests, resource limits, the
// filelog offset volume and tolerations for the collector daemonset.
type ExtraConfig struct {
	InstrumentationInitContainerResources ResourceRequirementsWithGoMemLimit `json:"initContainerResources"`

	CollectorFilelogOffsetStorageVolume *corev1.Volume `json:"collectorFilelogOffsetStorageVolume,omitempty"`

	CollectorDaemonSetCollectorContainerResources             ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetCollectorContainerResources"`
	CollectorDaemonSetConfigurationReloaderContainerResources ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetConfigurationReloaderContainerResources"`
	CollectorDaemonSetFileLogOffsetSyncContainerResources     ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetFileLogOffsetSyncContainerResources"`

	CollectorDaemonSetLabels         map[string]string `json:"collectorDaemonSetLabels,omitempty"`
	CollectorDaemonSetAnnotations    map[string]string `json:"collectorDaemonSetAnnotations,omitempty"`
	CollectorDaemonSetPodLabels      map[string]string `json:"collectorDaemonSetPodLabels,omitempty"`
	CollectorDaemonSetPodAnnotations map[string]string `json:"collectorDaemonSetPodAnnotations,omitempty"`

	DaemonSetTolerations  []corev1.Toleration  `json:"daemonSetTolerations,omitempty"`
	DaemonSetNodeAffinity *corev1.NodeAffinity `json:"daemonSetNodeAffinity,omitempty"`

	DaemonSetSysctls []corev1.Sysctl `json:"daemonSetSysctls,omitempty"`

	DaemonSetSELinuxOptions *corev1.SELinuxOptions `json:"daemonSetSeLinuxOptions,omitempty"`

	CollectorDaemonSetPriorityClassName string `json:"collectorDaemonSetPriorityClassName,omitempty"`

	DaemonSetProbes CollectorProbes `json:"daemonSetProbes"`

	CollectorDeploymentCollectorContainerResources             ResourceRequirementsWithGoMemLimit `json:"collectorDeploymentCollectorContainerResources"`
	CollectorDeploymentConfigurationReloaderContainerResources ResourceRequirementsWithGoMemLimit `json:"collectorDeploymentConfigurationReloaderContainerResources"`

	CollectorDeploymentLabels         map[string]string `json:"collectorDeploymentLabels,omitempty"`
	CollectorDeploymentAnnotations    map[string]string `json:"collectorDeploymentAnnotations,omitempty"`
	CollectorDeploymentPodLabels      map[string]string `json:"collectorDeploymentPodLabels,omitempty"`
	CollectorDeploymentPodAnnotations map[string]string `json:"collectorDeploymentPodAnnotations,omitempty"`

	DeploymentTolerations  []corev1.Toleration  `json:"deploymentTolerations,omitempty"`
	DeploymentNodeAffinity *corev1.NodeAffinity `json:"deploymentNodeAffinity,omitempty"`

	DeploymentSysctls []corev1.Sysctl `json:"deploymentSysctls,omitempty"`

	CollectorDeploymentPriorityClassName string `json:"collectorDeploymentPriorityClassName,omitempty"`

	DeploymentProbes CollectorProbes `json:"deploymentProbes"`

	SignalControlCollectorReplicas                                int32                              `json:"signalControlCollectorReplicas,omitempty"`
	SignalControlCollectorContainerResources                      ResourceRequirementsWithGoMemLimit `json:"signalControlCollectorContainerResources"`
	SignalControlCollectorConfigurationReloaderContainerResources ResourceRequirementsWithGoMemLimit `json:"signalControlCollectorConfigurationReloaderContainerResources"`

	SignalControlCollectorLabels         map[string]string `json:"signalControlCollectorLabels,omitempty"`
	SignalControlCollectorAnnotations    map[string]string `json:"signalControlCollectorAnnotations,omitempty"`
	SignalControlCollectorPodLabels      map[string]string `json:"signalControlCollectorPodLabels,omitempty"`
	SignalControlCollectorPodAnnotations map[string]string `json:"signalControlCollectorPodAnnotations,omitempty"`

	SignalControlCollectorTolerations  []corev1.Toleration  `json:"signalControlCollectorTolerations,omitempty"`
	SignalControlCollectorNodeAffinity *corev1.NodeAffinity `json:"signalControlCollectorNodeAffinity,omitempty"`

	SignalControlCollectorSysctls []corev1.Sysctl `json:"signalControlCollectorSysctls,omitempty"`

	SignalControlCollectorPriorityClassName string `json:"signalControlCollectorPriorityClassName,omitempty"`

	SignalControlCollectorProbes CollectorProbes `json:"signalControlCollectorProbes"`

	TargetAllocatorMtlsEnabled              bool                               `json:"targetAllocatorMtlsEnabled,omitempty"`
	TargetAllocatorMtlsServerCertSecretName string                             `json:"targetAllocatorMtlsServerCertSecretName,omitempty"`
	TargetAllocatorMtlsClientCertSecretName string                             `json:"targetAllocatorMtlsClientCertSecretName,omitempty"`
	TargetAllocatorAllowInsecureAuthSecrets bool                               `json:"targetAllocatorAllowInsecureAuthSecrets,omitempty"`
	TargetAllocatorContainerResources       ResourceRequirementsWithGoMemLimit `json:"targetAllocatorContainerResources"`
	TargetAllocatorLabels                   map[string]string                  `json:"targetAllocatorLabels,omitempty"`
	TargetAllocatorAnnotations              map[string]string                  `json:"targetAllocatorAnnotations,omitempty"`
	TargetAllocatorPodLabels                map[string]string                  `json:"targetAllocatorPodLabels,omitempty"`
	TargetAllocatorPodAnnotations           map[string]string                  `json:"targetAllocatorPodAnnotations,omitempty"`
	TargetAllocatorTolerations              []corev1.Toleration                `json:"targetAllocatorTolerations,omitempty"`
	TargetAllocatorNodeAffinity             *corev1.NodeAffinity               `json:"targetAllocatorNodeAffinity,omitempty"`

	EdgeProxyReplicas           int32                              `json:"edgeProxyReplicas,omitempty"`
	EdgeProxyEnablePprof        bool                               `json:"edgeProxyEnablePprof,omitempty"`
	EdgeProxyContainerResources ResourceRequirementsWithGoMemLimit `json:"edgeProxyContainerResources"`
	EdgeProxyTolerations        []corev1.Toleration                `json:"edgeProxyTolerations,omitempty"`
	EdgeProxyNodeAffinity       *corev1.NodeAffinity               `json:"edgeProxyNodeAffinity,omitempty"`

	EdgeProxyLabels         map[string]string `json:"edgeProxyLabels,omitempty"`
	EdgeProxyAnnotations    map[string]string `json:"edgeProxyAnnotations,omitempty"`
	EdgeProxyPodLabels      map[string]string `json:"edgeProxyPodLabels,omitempty"`
	EdgeProxyPodAnnotations map[string]string `json:"edgeProxyPodAnnotations,omitempty"`

	Agent0ConnectorMaxConcurrentCommands  int32                              `json:"agent0ConnectorMaxConcurrentCommands,omitempty"`
	Agent0ConnectorClusterRoleRules       []rbacv1.PolicyRule                `json:"agent0ConnectorClusterRoleRules,omitempty"`
	Agent0ConnectorAllowedKubectlCommands map[string]bool                    `json:"agent0ConnectorAllowedKubectlCommands,omitempty"`
	Agent0ConnectorContainerResources     ResourceRequirementsWithGoMemLimit `json:"agent0ConnectorContainerResources"`
	Agent0ConnectorLabels                 map[string]string                  `json:"agent0ConnectorLabels,omitempty"`
	Agent0ConnectorAnnotations            map[string]string                  `json:"agent0ConnectorAnnotations,omitempty"`
	Agent0ConnectorPodLabels              map[string]string                  `json:"agent0ConnectorPodLabels,omitempty"`
	Agent0ConnectorPodAnnotations         map[string]string                  `json:"agent0ConnectorPodAnnotations,omitempty"`
	Agent0ConnectorTolerations            []corev1.Toleration                `json:"agent0ConnectorTolerations,omitempty"`
	Agent0ConnectorNodeAffinity           *corev1.NodeAffinity               `json:"agent0ConnectorNodeAffinity,omitempty"`

	// Actually we would like to use the types *dash0v1alpha1.MonitoringTemplate, *dash0common.Filter and
	// *dash0common.Transform here, but that leads to circular package dependencies. We should revisit how to untangle
	// this.
	MonitoringTemplateRaw *json.RawMessage `json:"monitoringTemplate,omitempty"`
	FilterRaw             *json.RawMessage `json:"filter,omitempty"`
	TransformRaw          *json.RawMessage `json:"transform,omitempty"`

	// Exports are the exports for the automatically created operator configuration resource, provided via the Helm
	// value operator.exports. They are appended to the Dash0 export that is derived from the operator.dash0Export.*
	// Helm values (if any).
	Exports []dash0common.Export `json:"exports,omitempty"`
}

type ExtraConfigClient interface {
	UpdateExtraConfig(context.Context, ExtraConfig, logd.Logger)
}

const (
	extraConfigDir  = "/etc/config"
	extraConfigFile = "/etc/config/extra.yaml"
)

var (
	ExtraConfigDefaults = ExtraConfig{
		CollectorDaemonSetCollectorContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		CollectorDaemonSetConfigurationReloaderContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("26Mi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
		CollectorDaemonSetFileLogOffsetSyncContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
		},
		CollectorDeploymentCollectorContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		CollectorDeploymentConfigurationReloaderContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("26Mi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
		// The Signal Control collector aggregates the Dash0-bound telemetry of the whole cluster (tail-sampling
		// reservoir, RED metrics, signal-to-metrics), so it needs considerably more memory than the per-node collector.
		SignalControlCollectorContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		SignalControlCollectorConfigurationReloaderContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("26Mi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
		Agent0ConnectorContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},

		// The following defaults are only applied when the respective key is missing from the extra config map (see
		// readExtraConfigurationFromFile), since the Helm chart renders them whenever the respective component is
		// enabled.
		InstrumentationInitContainerResources: ResourceRequirementsWithGoMemLimit{},
		DaemonSetNodeAffinity:                 defaultNodeAffinity(),
		DaemonSetProbes:                       defaultCollectorProbes,
		DeploymentNodeAffinity:                defaultNodeAffinity(),
		DeploymentProbes:                      defaultCollectorProbes,
		SignalControlCollectorReplicas:        2,
		SignalControlCollectorNodeAffinity:    defaultNodeAffinity(),
		SignalControlCollectorProbes:          defaultCollectorProbes,
		TargetAllocatorContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
		TargetAllocatorNodeAffinity: defaultNodeAffinity(),
		EdgeProxyReplicas:           2,
		EdgeProxyContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			GoMemLimit: "",
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
		EdgeProxyNodeAffinity:                defaultNodeAffinity(),
		Agent0ConnectorMaxConcurrentCommands: 2,
		Agent0ConnectorAllowedKubectlCommands: map[string]bool{
			"api-resources": true,
			"api-versions":  true,
			"auth":          true,
			"cluster-info":  true,
			"events":        false,
			"explain":       true,
			"get":           true,
			"logs":          false,
			"top":           true,
			"version":       true,
		},
		Agent0ConnectorNodeAffinity: defaultNodeAffinity(),
	}

	// gkeAutopilotInstrumentationInitContainerResources mirrors the Helm chart's fallback for GKE Autopilot when
	// operator.initContainerResources is not set, see
	// helm-chart/dash0-operator/files/gke-autopilot-init-container-resources.yaml.
	gkeAutopilotInstrumentationInitContainerResources = ResourceRequirementsWithGoMemLimit{
		Limits: corev1.ResourceList{
			corev1.ResourceEphemeralStorage: resource.MustParse("500Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("100m"),
			corev1.ResourceMemory:           resource.MustParse("128Mi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("500Mi"),
		},
	}

	defaultCollectorProbes = CollectorProbes{
		Liveness:  defaultProbe(20),
		Readiness: defaultProbe(3),
		Startup:   defaultProbe(45),
	}
)

func defaultProbe(failureThreshold int32) corev1.Probe {
	return corev1.Probe{
		InitialDelaySeconds: 0,
		PeriodSeconds:       2,
		TimeoutSeconds:      1,
		FailureThreshold:    failureThreshold,
	}
}

// defaultNodeAffinity keeps the pods of the managed workloads off nodes labelled with dash0.com/enable=false and off
// non-Linux nodes.
func defaultNodeAffinity() *corev1.NodeAffinity {
	return &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "dash0.com/enable",
							Operator: corev1.NodeSelectorOpNotIn,
							Values:   []string{"false"},
						},
						{
							Key:      "kubernetes.io/os",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"linux"},
						},
					},
				},
			},
		},
	}
}

// ExtraConfigDefaultsFor returns the defaults that apply when a value is missing from the extra config map. They match
// the defaults of the Helm chart.
func ExtraConfigDefaultsFor(isGkeAutopilot bool) ExtraConfig {
	defaults := ExtraConfigDefaults
	if isGkeAutopilot {
		defaults.InstrumentationInitContainerResources = gkeAutopilotInstrumentationInitContainerResources
	}
	return defaults
}

// ReadExtraConfigMap reads the config map content from the default location. If the file does not exist, the defaults
// are used.
func ReadExtraConfigMap(defaults ExtraConfig, logger logd.Logger) (ExtraConfig, error) {
	return readExtraConfigurationFromFile(extraConfigFile, defaults, true, logger)
}

// readExtraConfigurationFromFile reads the config map content from the given file path. A missing file is an error,
// unless missingFileUsesDefaults is set.
func readExtraConfigurationFromFile(
	configurationFile string,
	defaults ExtraConfig,
	missingFileUsesDefaults bool,
	logger logd.Logger,
) (ExtraConfig, error) {
	if len(configurationFile) == 0 {
		return ExtraConfig{}, fmt.Errorf("filename is empty")
	}
	content, err := os.ReadFile(configurationFile)
	if errors.Is(err, fs.ErrNotExist) && missingFileUsesDefaults {
		logger.Info("The extra configuration file does not exist, using the default values.", "file", configurationFile)
		content = nil
	} else if err != nil {
		return ExtraConfig{}, fmt.Errorf("the configuration file (%s) is missing or cannot be opened %w", configurationFile, err)
	}

	extraConfig := &ExtraConfig{}
	if err = yaml.Unmarshal(content, extraConfig); err != nil {
		return ExtraConfig{}, fmt.Errorf("cannot unmarshal the configuration file %w", err)
	}
	presentKeys := map[string]any{}
	if err = yaml.Unmarshal(content, &presentKeys); err != nil {
		return ExtraConfig{}, fmt.Errorf("cannot unmarshal the configuration file %w", err)
	}
	applyDefaultsForMissingKeys(extraConfig, &defaults, presentKeys)

	applyDefaults(
		&extraConfig.CollectorDaemonSetCollectorContainerResources,
		&defaults.CollectorDaemonSetCollectorContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources,
		&defaults.CollectorDaemonSetConfigurationReloaderContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources,
		&defaults.CollectorDaemonSetFileLogOffsetSyncContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDeploymentCollectorContainerResources,
		&defaults.CollectorDeploymentCollectorContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDeploymentConfigurationReloaderContainerResources,
		&defaults.CollectorDeploymentConfigurationReloaderContainerResources,
	)
	applyDefaults(
		&extraConfig.SignalControlCollectorContainerResources,
		&defaults.SignalControlCollectorContainerResources,
	)
	applyDefaults(
		&extraConfig.SignalControlCollectorConfigurationReloaderContainerResources,
		&defaults.SignalControlCollectorConfigurationReloaderContainerResources,
	)
	applyDefaults(
		&extraConfig.Agent0ConnectorContainerResources,
		&defaults.Agent0ConnectorContainerResources,
	)

	return *extraConfig, nil
}

// applyDefaultsForMissingKeys sets the defaults for the values that the Helm chart renders whenever the respective
// component is enabled, when their key is missing. A value that is present is kept as it is, including an explicit
// null.
func applyDefaultsForMissingKeys(extraConfig *ExtraConfig, defaults *ExtraConfig, presentKeys map[string]any) {
	missing := func(key string) bool {
		_, present := presentKeys[key]
		return !present
	}
	if missing("initContainerResources") {
		extraConfig.InstrumentationInitContainerResources = defaults.InstrumentationInitContainerResources.DeepCopy()
	}
	if missing("daemonSetNodeAffinity") {
		extraConfig.DaemonSetNodeAffinity = defaults.DaemonSetNodeAffinity.DeepCopy()
	}
	if missing("daemonSetProbes") {
		extraConfig.DaemonSetProbes = defaults.DaemonSetProbes.DeepCopy()
	}
	if missing("deploymentNodeAffinity") {
		extraConfig.DeploymentNodeAffinity = defaults.DeploymentNodeAffinity.DeepCopy()
	}
	if missing("deploymentProbes") {
		extraConfig.DeploymentProbes = defaults.DeploymentProbes.DeepCopy()
	}
	if missing("signalControlCollectorReplicas") {
		extraConfig.SignalControlCollectorReplicas = defaults.SignalControlCollectorReplicas
	}
	if missing("signalControlCollectorNodeAffinity") {
		extraConfig.SignalControlCollectorNodeAffinity = defaults.SignalControlCollectorNodeAffinity.DeepCopy()
	}
	if missing("signalControlCollectorProbes") {
		extraConfig.SignalControlCollectorProbes = defaults.SignalControlCollectorProbes.DeepCopy()
	}
	if missing("targetAllocatorContainerResources") {
		extraConfig.TargetAllocatorContainerResources = defaults.TargetAllocatorContainerResources.DeepCopy()
	}
	if missing("targetAllocatorNodeAffinity") {
		extraConfig.TargetAllocatorNodeAffinity = defaults.TargetAllocatorNodeAffinity.DeepCopy()
	}
	if missing("edgeProxyReplicas") {
		extraConfig.EdgeProxyReplicas = defaults.EdgeProxyReplicas
	}
	if missing("edgeProxyContainerResources") {
		extraConfig.EdgeProxyContainerResources = defaults.EdgeProxyContainerResources.DeepCopy()
	}
	if missing("edgeProxyNodeAffinity") {
		extraConfig.EdgeProxyNodeAffinity = defaults.EdgeProxyNodeAffinity.DeepCopy()
	}
	if missing("agent0ConnectorMaxConcurrentCommands") {
		extraConfig.Agent0ConnectorMaxConcurrentCommands = defaults.Agent0ConnectorMaxConcurrentCommands
	}
	if missing("agent0ConnectorAllowedKubectlCommands") {
		extraConfig.Agent0ConnectorAllowedKubectlCommands = maps.Clone(defaults.Agent0ConnectorAllowedKubectlCommands)
	}
	if missing("agent0ConnectorNodeAffinity") {
		extraConfig.Agent0ConnectorNodeAffinity = defaults.Agent0ConnectorNodeAffinity.DeepCopy()
	}
}

// applyDefaults sets default values for CPU, Memory, and Ephemeral Storage on requests and limits.
func applyDefaults(spec *ResourceRequirementsWithGoMemLimit, defaults *ResourceRequirementsWithGoMemLimit) {
	if spec == nil || defaults == nil {
		// if spec is a nil pointer, we have no target object to update
		// if defaults is a nil pointer, we have no source object to copy values from
		return
	}
	if defaults.Requests != nil {
		if spec.Requests == nil {
			spec.Requests = make(corev1.ResourceList)
		}
		applyDefaultValues(spec.Requests, defaults.Requests)
	}
	if defaults.Limits != nil {
		if spec.Limits == nil {
			spec.Limits = make(corev1.ResourceList)
		}
		applyDefaultValues(spec.Limits, defaults.Limits)

	}
	if spec.GoMemLimit == "" && defaults.GoMemLimit != "" {
		spec.GoMemLimit = defaults.GoMemLimit
	}
}

// applyDefaultValues sets default values for CPU, Memory, and Ephemeral Storage on either requests or limits, expects
// both resourceList and defaults to not be nil.
func applyDefaultValues(resourceList corev1.ResourceList, defaults corev1.ResourceList) {
	if resourceList.Cpu().IsZero() && !defaults.Cpu().IsZero() {
		resourceList[corev1.ResourceCPU] = *defaults.Cpu()
	}
	if resourceList.Memory().IsZero() && !defaults.Memory().IsZero() {
		resourceList[corev1.ResourceMemory] = *defaults.Memory()
	}
	if resourceList.StorageEphemeral().IsZero() && !defaults.StorageEphemeral().IsZero() {
		resourceList[corev1.ResourceEphemeralStorage] = *defaults.StorageEphemeral()
	}

}

func (rr ResourceRequirementsWithGoMemLimit) ToResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits:   rr.Limits,
		Requests: rr.Requests,
	}
}

// DeepCopy returns a copy that shares no maps with the original.
func (rr ResourceRequirementsWithGoMemLimit) DeepCopy() ResourceRequirementsWithGoMemLimit {
	return ResourceRequirementsWithGoMemLimit{
		Limits:     rr.Limits.DeepCopy(),
		Requests:   rr.Requests.DeepCopy(),
		GoMemLimit: rr.GoMemLimit,
	}
}

// DeepCopy returns a copy that shares no pointers with the original.
func (p CollectorProbes) DeepCopy() CollectorProbes {
	return CollectorProbes{
		Liveness:  *p.Liveness.DeepCopy(),
		Readiness: *p.Readiness.DeepCopy(),
		Startup:   *p.Startup.DeepCopy(),
	}
}

type ExtraConfigWatcher struct {
	watcher *fsnotify.Watcher
	clients []ExtraConfigClient
}

func NewExtraConfigWatcher() *ExtraConfigWatcher {
	return &ExtraConfigWatcher{
		clients: make([]ExtraConfigClient, 0),
	}
}

// StartWatch watches the extra config map file and updates all clients when it changes. The defaults are applied to
// the updated content in the same way as in ReadExtraConfigMap. A file that has been removed does not fall back to the
// defaults, the clients keep their current configuration until the operator restarts.
func (w *ExtraConfigWatcher) StartWatch(defaults ExtraConfig, logger logd.Logger) error {
	return w.watchConfigurationDirectory(extraConfigDir, extraConfigFile, defaults, logger)
}

func (w *ExtraConfigWatcher) AddClient(client ExtraConfigClient) {
	w.clients = append(w.clients, client)
}

func (w *ExtraConfigWatcher) watchConfigurationDirectory(
	configurationDir string,
	extraConfigFile string,
	defaults ExtraConfig,
	setupLogger logd.Logger,
) error {
	if _, err := os.Stat(configurationDir); errors.Is(err, fs.ErrNotExist) {
		setupLogger.Info(
			"The extra config directory does not exist, changes to the extra configuration will not be watched.",
			"directory", configurationDir,
		)
		return nil
	}

	var err error
	w.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		setupLogger.Error(err, "cannot establish file watcher")
		return err
	}

	// When the config map is changed, and Kubernetes finally updates it, there a couple of file system events
	// (copying to old file to a backup location, creating the new content as a temporary file, renaming that to
	// the actual name, etc.). Therefore, we debounce these events and only update clients after a debounce timeout of
	// 1 second.
	debouncedFileWatchEvents := debounce.New(1 * time.Second)
	go func() {
		for {
			select {
			case _, ok := <-w.watcher.Events:
				if !ok {
					return
				}
				ctx := context.Background()
				logger := logd.FromContext(ctx)
				debouncedFileWatchEvents(func() {
					logger.Info("the extra config map has been updated")
					// The defaults only stand in for a file that has never been mounted, not for one that has vanished.
					extraConfig, err := readExtraConfigurationFromFile(extraConfigFile, defaults, false, logger)
					if err != nil {
						logger.Error(err, "cannot read extra config map file after it has been updated")
						return
					}
					logger.Info("updating all clients with updated extra config map")
					for _, client := range w.clients {
						client.UpdateExtraConfig(ctx, extraConfig, logger)
					}
				})
			case fsnotifyErr, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
				ctx := context.Background()
				logger := logd.FromContext(ctx)
				logger.Error(fsnotifyErr, "extra config map file watcher error")
			}
		}
	}()

	// Watch the parent directory of the extra config map file. Watching the file directly only works once, since
	// updating the file include a remove operation on the file system level, and fsnotify removes the watch silently
	// once the path added via watcher.Add is removed.
	// Also, firing the fsnotify event can take a minute or a bit more after the config map has been changed.
	if err = w.watcher.Add(configurationDir); err != nil {
		setupLogger.Error(err, "cannot add watcher for extra config directory", "directory", configurationDir)
		return err
	}
	return nil
}

func (w *ExtraConfigWatcher) CloseWatch() {
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
}
