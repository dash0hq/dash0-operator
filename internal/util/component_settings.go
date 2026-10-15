// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/ptr"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

const (
	operatorConfigurationKind = "Dash0OperatorConfiguration"
	signalControlKind         = "Dash0SignalControl"

	filelogOffsetStorageVolumeName = "filelogreceiver-offsets"

	// maxSettingLengthInWarning is the maximum length of a value that ComponentSettingConflict.String includes. The API
	// server truncates every admission warning to 256 characters once the warnings of a request exceed 4096 characters.
	maxSettingLengthInWarning = 64

	componentSettingsPath = "spec.components"
)

// The names of the components in spec.components of the custom resources.
const (
	componentDaemonSetCollector     = "daemonSetCollector"
	componentDeploymentCollector    = "deploymentCollector"
	componentTargetAllocator        = "targetAllocator"
	componentAgent0Connector        = "agent0Connector"
	componentSignalControlCollector = "collector"
	componentEdgeProxy              = "edgeProxy"
)

// ComponentSettingConflict is a setting that is configured both via the Helm chart and via spec.components of a custom
// resource, with different values. The value of the custom resource takes effect.
type ComponentSettingConflict struct {
	// Resource is the kind of the custom resource, e.g. Dash0OperatorConfiguration.
	Resource string
	// Path is the path of the setting in the custom resource.
	Path string
	// HelmValue is the name of the Helm value.
	HelmValue string
	// HelmSetting is the value provided via the Helm chart.
	HelmSetting string
	// ResourceSetting is the value provided via the custom resource, which takes effect.
	ResourceSetting string
}

// String describes the conflict, it is used as an admission warning. The values are left out if one of them is long or
// contains control characters, since the API server drops warnings with control characters.
func (c ComponentSettingConflict) String() string {
	if !fitsIntoWarning(c.HelmSetting) || !fitsIntoWarning(c.ResourceSetting) {
		return fmt.Sprintf("%s overrides the custom Helm value %s", c.Path, c.HelmValue)
	}
	return fmt.Sprintf(
		"%s overrides the custom Helm value %s: %s instead of %s",
		c.Path, c.HelmValue, c.ResourceSetting, c.HelmSetting,
	)
}

// IsSameSetting reports whether both conflicts concern the same setting, regardless of the values.
func (c ComponentSettingConflict) IsSameSetting(other ComponentSettingConflict) bool {
	return c.Resource == other.Resource && c.Path == other.Path && c.HelmValue == other.HelmValue
}

func fitsIntoWarning(setting string) bool {
	return utf8.RuneCountInString(setting) <= maxSettingLengthInWarning &&
		!strings.ContainsFunc(setting, unicode.IsControl)
}

// ComponentSettingsCheck is the result of checking spec.components of the custom resources, see CheckComponentSettings.
type ComponentSettingsCheck struct {
	// Conflicts are the settings that override a different value provided via the Helm chart.
	Conflicts []ComponentSettingConflict
	// Invalid describes the invalid settings, one entry per violation.
	Invalid []string
}

// EffectiveExtraConfig applies spec.components of the given resources (either may be nil) to the extra config. A
// setting replaces the value provided via the Helm chart, the fields it leaves out are taken from the given defaults
// (see mergeNodeAffinity and mergeProbe). Invalid settings are rejected by the validating webhooks (see
// CheckComponentSettings). Neither the file configuration nor the resources are modified.
func EffectiveExtraConfig(
	fileConfig ExtraConfig,
	defaults ExtraConfig,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
) ExtraConfig {
	effective, _ := resolveComponentSettings(
		fileConfig,
		defaults,
		OperatorConfigurationComponentsOf(operatorConfigurationResource),
		SignalControlComponentsOf(signalControlResource),
		false,
	)
	return effective
}

// OperatorConfigurationComponentsOf returns spec.components of the resource, or nil if the resource is nil.
func OperatorConfigurationComponentsOf(
	resource *dash0v1alpha1.Dash0OperatorConfiguration,
) *dash0v1alpha1.OperatorConfigurationComponents {
	if resource == nil {
		return nil
	}
	return resource.Spec.Components
}

// SignalControlComponentsOf returns spec.components of the resource, or nil if the resource is nil.
func SignalControlComponentsOf(resource *dash0v1alpha1.Dash0SignalControl) *dash0v1alpha1.SignalControlComponents {
	if resource == nil {
		return nil
	}
	return resource.Spec.Components
}

// ComponentSettingConflicts returns the settings in spec.components that override a different value provided via Helm,
// without validating the settings. Either one of the components may be nil. A file value only counts as provided via
// Helm if it differs from the given defaults, which need to match the defaults of the Helm chart.
func ComponentSettingConflicts(
	fileConfig ExtraConfig,
	defaults ExtraConfig,
	operatorConfigurationComponents *dash0v1alpha1.OperatorConfigurationComponents,
	signalControlComponents *dash0v1alpha1.SignalControlComponents,
) []ComponentSettingConflict {
	_, conflicts := resolveComponentSettings(
		fileConfig, defaults, operatorConfigurationComponents, signalControlComponents, true)
	return conflicts
}

// CheckComponentSettings returns the conflicts (see ComponentSettingConflicts) and the invalid settings in
// spec.components (rejected by the API server). Either one of the components may be nil.
func CheckComponentSettings(
	fileConfig ExtraConfig,
	defaults ExtraConfig,
	operatorConfigurationComponents *dash0v1alpha1.OperatorConfigurationComponents,
	signalControlComponents *dash0v1alpha1.SignalControlComponents,
) ComponentSettingsCheck {
	check := ComponentSettingsCheck{
		Conflicts: ComponentSettingConflicts(
			fileConfig, defaults, operatorConfigurationComponents, signalControlComponents),
	}
	errs := validateOperatorConfigurationComponents(operatorConfigurationComponents)
	errs = append(errs, validateSignalControlComponents(signalControlComponents)...)
	for _, err := range errs {
		check.Invalid = append(check.Invalid, err.Error())
	}
	return check
}

// resolveComponentSettings returns the effective extra configuration (see EffectiveExtraConfig), and, if report is set,
// the conflicts of the settings. It does not validate the settings.
func resolveComponentSettings(
	fileConfig ExtraConfig,
	defaults ExtraConfig,
	operatorConfigurationComponents *dash0v1alpha1.OperatorConfigurationComponents,
	signalControlComponents *dash0v1alpha1.SignalControlComponents,
	report bool,
) (ExtraConfig, []ComponentSettingConflict) {
	effective := fileConfig
	var conflicts []ComponentSettingConflict
	if operatorConfigurationComponents != nil {
		m := &componentSettingsApplier{resource: operatorConfigurationKind, report: report}
		m.applyOperatorConfigurationComponents(&effective, &defaults, operatorConfigurationComponents)
		conflicts = append(conflicts, m.conflicts...)
	}
	if signalControlComponents != nil {
		m := &componentSettingsApplier{resource: signalControlKind, report: report}
		m.applySignalControlComponents(&effective, &defaults, signalControlComponents)
		conflicts = append(conflicts, m.conflicts...)
	}
	return effective, conflicts
}

// componentSettingsApplier applies the settings of one custom resource. Unless report is set, it does not record
// conflicts.
type componentSettingsApplier struct {
	resource  string
	report    bool
	conflicts []ComponentSettingConflict
}

// workloadFields points to the fields of the extra configuration that hold the settings of one workload.
type workloadFields struct {
	labels         *map[string]string
	annotations    *map[string]string
	podLabels      *map[string]string
	podAnnotations *map[string]string
	tolerations    *[]corev1.Toleration
	nodeAffinity   **corev1.NodeAffinity
}

// workloadHelmValues are the names of the Helm values for the settings of one workload.
type workloadHelmValues struct {
	labels         string
	annotations    string
	podLabels      string
	podAnnotations string
	tolerations    string
	nodeAffinity   string
}

// collectorPodFields points to the fields of the extra configuration that hold the pod settings of one collector.
type collectorPodFields struct {
	sysctls           *[]corev1.Sysctl
	probes            *CollectorProbes
	priorityClassName *string
}

type collectorPodHelmValues struct {
	sysctls           string
	probes            string
	priorityClassName string
}

// targetAllocatorMTls holds the fields of the extra configuration for the target-allocator mTLS settings, which are
// replaced together.
type targetAllocatorMTls struct {
	Enabled              bool   `json:"enabled"`
	ServerCertSecretName string `json:"serverCertSecretName"`
	ClientCertSecretName string `json:"clientCertSecretName"`
}

// withoutUnusedSecretNames leaves out the secret names if mTLS is disabled, since they are only used with mTLS.
func (t targetAllocatorMTls) withoutUnusedSecretNames() targetAllocatorMTls {
	if !t.Enabled {
		return targetAllocatorMTls{}
	}
	return t
}

// collectorHelmValues returns the names of the Helm values for a collector, which are all derived from one prefix, e.g.
// operator.collectors.daemonSet -> operator.collectors.daemonSetLabels.
func collectorHelmValues(prefix string) (workloadHelmValues, collectorPodHelmValues) {
	return workloadHelmValues{
		labels:         prefix + "Labels",
		annotations:    prefix + "Annotations",
		podLabels:      prefix + "PodLabels",
		podAnnotations: prefix + "PodAnnotations",
		tolerations:    prefix + "Tolerations",
		nodeAffinity:   prefix + "NodeAffinity",
	}, collectorPodHelmValues{
		sysctls:           prefix + "Sysctls",
		probes:            prefix + "Probes",
		priorityClassName: prefix + "PriorityClassName",
	}
}

// nestedHelmValues returns the names of the Helm values for a workload whose settings are grouped in one object, e.g.
// operator.targetAllocator -> operator.targetAllocator.labels.
func nestedHelmValues(prefix string) workloadHelmValues {
	return workloadHelmValues{
		labels:         prefix + ".labels",
		annotations:    prefix + ".annotations",
		podLabels:      prefix + ".podLabels",
		podAnnotations: prefix + ".podAnnotations",
		tolerations:    prefix + ".tolerations",
		nodeAffinity:   prefix + ".nodeAffinity",
	}
}

func (m *componentSettingsApplier) applyOperatorConfigurationComponents(
	effective *ExtraConfig,
	defaults *ExtraConfig,
	components *dash0v1alpha1.OperatorConfigurationComponents,
) {
	const path = componentSettingsPath

	if daemonSet := components.DaemonSetCollector; daemonSet != nil {
		p := path + "." + componentDaemonSetCollector
		workloadHelm, podHelm := collectorHelmValues("operator.collectors.daemonSet")
		m.applyWorkload(p, workloadHelm, workloadFields{
			labels:         &effective.CollectorDaemonSetLabels,
			annotations:    &effective.CollectorDaemonSetAnnotations,
			podLabels:      &effective.CollectorDaemonSetPodLabels,
			podAnnotations: &effective.CollectorDaemonSetPodAnnotations,
			tolerations:    &effective.DaemonSetTolerations,
			nodeAffinity:   &effective.DaemonSetNodeAffinity,
		}, defaults.DaemonSetNodeAffinity, daemonSet.WorkloadSettings)
		m.applyCollectorPod(p, podHelm, collectorPodFields{
			sysctls:           &effective.DaemonSetSysctls,
			probes:            &effective.DaemonSetProbes,
			priorityClassName: &effective.CollectorDaemonSetPriorityClassName,
		}, defaults.DaemonSetProbes, daemonSet.CollectorPodSettings)
		if seLinuxOptions := daemonSet.SELinuxOptions; seLinuxOptions != nil {
			var value *corev1.SELinuxOptions
			if *seLinuxOptions != (corev1.SELinuxOptions{}) {
				value = seLinuxOptions.DeepCopy()
			}
			replaceValue(m, p+".seLinuxOptions", "operator.collectors.daemonSetSeLinuxOptions",
				&effective.DaemonSetSELinuxOptions, nil, value)
		}
		if volume := daemonSet.FilelogOffsetStorageVolume; volume != nil {
			var volumeSource corev1.VolumeSource
			if volume.HostPath != nil {
				volumeSource.HostPath = &corev1.HostPathVolumeSource{
					Path: volume.HostPath.Path,
					Type: new(corev1.HostPathDirectoryOrCreate),
				}
			} else if volume.PersistentVolumeClaim != nil {
				volumeSource.PersistentVolumeClaim = &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: volume.PersistentVolumeClaim.ClaimName,
				}
			}
			if volumeSource != (corev1.VolumeSource{}) {
				replaceValue(m, p+".filelogOffsetStorageVolume", "operator.collectors.filelogOffsetSyncStorageVolume",
					&effective.CollectorFilelogOffsetStorageVolume, nil, &corev1.Volume{
						Name:         filelogOffsetStorageVolumeName,
						VolumeSource: volumeSource,
					})
			}
		}
	}

	if deployment := components.DeploymentCollector; deployment != nil {
		p := path + "." + componentDeploymentCollector
		workloadHelm, podHelm := collectorHelmValues("operator.collectors.deployment")
		m.applyWorkload(p, workloadHelm, workloadFields{
			labels:         &effective.CollectorDeploymentLabels,
			annotations:    &effective.CollectorDeploymentAnnotations,
			podLabels:      &effective.CollectorDeploymentPodLabels,
			podAnnotations: &effective.CollectorDeploymentPodAnnotations,
			tolerations:    &effective.DeploymentTolerations,
			nodeAffinity:   &effective.DeploymentNodeAffinity,
		}, defaults.DeploymentNodeAffinity, deployment.WorkloadSettings)
		m.applyCollectorPod(p, podHelm, collectorPodFields{
			sysctls:           &effective.DeploymentSysctls,
			probes:            &effective.DeploymentProbes,
			priorityClassName: &effective.CollectorDeploymentPriorityClassName,
		}, defaults.DeploymentProbes, deployment.CollectorPodSettings)
	}

	if targetAllocator := components.TargetAllocator; targetAllocator != nil {
		p := path + "." + componentTargetAllocator
		m.applyWorkload(p, nestedHelmValues("operator.targetAllocator"), workloadFields{
			labels:         &effective.TargetAllocatorLabels,
			annotations:    &effective.TargetAllocatorAnnotations,
			podLabels:      &effective.TargetAllocatorPodLabels,
			podAnnotations: &effective.TargetAllocatorPodAnnotations,
			tolerations:    &effective.TargetAllocatorTolerations,
			nodeAffinity:   &effective.TargetAllocatorNodeAffinity,
		}, defaults.TargetAllocatorNodeAffinity, targetAllocator.WorkloadSettings)
		if mTls := targetAllocator.MTls; mTls != nil {
			current := targetAllocatorMTls{
				Enabled:              effective.TargetAllocatorMtlsEnabled,
				ServerCertSecretName: effective.TargetAllocatorMtlsServerCertSecretName,
				ClientCertSecretName: effective.TargetAllocatorMtlsClientCertSecretName,
			}.withoutUnusedSecretNames()
			replaceValue(m, p+".mTls", "operator.targetAllocator.mTls", &current, targetAllocatorMTls{
				Enabled:              defaults.TargetAllocatorMtlsEnabled,
				ServerCertSecretName: defaults.TargetAllocatorMtlsServerCertSecretName,
				ClientCertSecretName: defaults.TargetAllocatorMtlsClientCertSecretName,
			}.withoutUnusedSecretNames(), targetAllocatorMTls{
				Enabled:              ptr.Deref(mTls.Enabled, false),
				ServerCertSecretName: mTls.ServerCertSecretName,
				ClientCertSecretName: mTls.ClientCertSecretName,
			}.withoutUnusedSecretNames())
			effective.TargetAllocatorMtlsEnabled = current.Enabled
			effective.TargetAllocatorMtlsServerCertSecretName = current.ServerCertSecretName
			effective.TargetAllocatorMtlsClientCertSecretName = current.ClientCertSecretName
		}
		replaceValueIfSet(m, p+".allowInsecureAuthSecrets", "operator.targetAllocator.allowInsecureAuthSecrets",
			&effective.TargetAllocatorAllowInsecureAuthSecrets, defaults.TargetAllocatorAllowInsecureAuthSecrets,
			targetAllocator.AllowInsecureAuthSecrets)
	}

	if agent0Connector := components.Agent0Connector; agent0Connector != nil {
		p := path + "." + componentAgent0Connector
		m.applyWorkload(p, nestedHelmValues("operator.agent0Connector"), workloadFields{
			labels:         &effective.Agent0ConnectorLabels,
			annotations:    &effective.Agent0ConnectorAnnotations,
			podLabels:      &effective.Agent0ConnectorPodLabels,
			podAnnotations: &effective.Agent0ConnectorPodAnnotations,
			tolerations:    &effective.Agent0ConnectorTolerations,
			nodeAffinity:   &effective.Agent0ConnectorNodeAffinity,
		}, defaults.Agent0ConnectorNodeAffinity, agent0Connector.WorkloadSettings)
	}
}

func (m *componentSettingsApplier) applySignalControlComponents(
	effective *ExtraConfig,
	defaults *ExtraConfig,
	components *dash0v1alpha1.SignalControlComponents,
) {
	const path = componentSettingsPath

	if collector := components.Collector; collector != nil {
		p := path + "." + componentSignalControlCollector
		replaceValueIfSet(m, p+".replicas", "operator.collectors.signalControlCollectorReplicas",
			&effective.SignalControlCollectorReplicas, defaults.SignalControlCollectorReplicas, collector.Replicas)
		workloadHelm, podHelm := collectorHelmValues("operator.collectors.signalControlCollector")
		m.applyWorkload(p, workloadHelm, workloadFields{
			labels:         &effective.SignalControlCollectorLabels,
			annotations:    &effective.SignalControlCollectorAnnotations,
			podLabels:      &effective.SignalControlCollectorPodLabels,
			podAnnotations: &effective.SignalControlCollectorPodAnnotations,
			tolerations:    &effective.SignalControlCollectorTolerations,
			nodeAffinity:   &effective.SignalControlCollectorNodeAffinity,
		}, defaults.SignalControlCollectorNodeAffinity, collector.WorkloadSettings)
		m.applyCollectorPod(p, podHelm, collectorPodFields{
			sysctls:           &effective.SignalControlCollectorSysctls,
			probes:            &effective.SignalControlCollectorProbes,
			priorityClassName: &effective.SignalControlCollectorPriorityClassName,
		}, defaults.SignalControlCollectorProbes, collector.CollectorPodSettings)
	}

	if edgeProxy := components.EdgeProxy; edgeProxy != nil {
		p := path + "." + componentEdgeProxy
		replaceValueIfSet(m, p+".replicas", "operator.signalControl.edgeProxy.replicas",
			&effective.EdgeProxyReplicas, defaults.EdgeProxyReplicas, edgeProxy.Replicas)
		replaceValueIfSet(m, p+".enablePprof", "operator.signalControl.edgeProxy.enablePprof",
			&effective.EdgeProxyEnablePprof, defaults.EdgeProxyEnablePprof, edgeProxy.EnablePprof)
		m.applyWorkload(p, nestedHelmValues("operator.signalControl.edgeProxy"), workloadFields{
			labels:         &effective.EdgeProxyLabels,
			annotations:    &effective.EdgeProxyAnnotations,
			podLabels:      &effective.EdgeProxyPodLabels,
			podAnnotations: &effective.EdgeProxyPodAnnotations,
			tolerations:    &effective.EdgeProxyTolerations,
			nodeAffinity:   &effective.EdgeProxyNodeAffinity,
		}, defaults.EdgeProxyNodeAffinity, edgeProxy.WorkloadSettings)
	}
}

func (m *componentSettingsApplier) applyWorkload(
	path string,
	helm workloadHelmValues,
	fields workloadFields,
	defaultNodeAffinity *corev1.NodeAffinity,
	settings dash0v1alpha1.WorkloadSettings,
) {
	if len(settings.Labels) > 0 {
		replaceValue(m, path+".labels", helm.labels, fields.labels, nil, maps.Clone(settings.Labels))
	}
	if len(settings.Annotations) > 0 {
		replaceValue(m, path+".annotations", helm.annotations, fields.annotations, nil, maps.Clone(settings.Annotations))
	}
	if len(settings.PodLabels) > 0 {
		replaceValue(m, path+".podLabels", helm.podLabels, fields.podLabels, nil, maps.Clone(settings.PodLabels))
	}
	if len(settings.PodAnnotations) > 0 {
		replaceValue(m, path+".podAnnotations", helm.podAnnotations, fields.podAnnotations, nil,
			maps.Clone(settings.PodAnnotations))
	}
	if len(settings.Tolerations) > 0 {
		tolerations := make([]corev1.Toleration, 0, len(settings.Tolerations))
		for _, toleration := range settings.Tolerations {
			tolerations = append(tolerations, *toleration.DeepCopy())
		}
		replaceValue(m, path+".tolerations", helm.tolerations, fields.tolerations, nil, tolerations)
	}
	if nodeAffinity := settings.NodeAffinity; nodeAffinity != nil {
		replaceValue(m, path+".nodeAffinity", helm.nodeAffinity, fields.nodeAffinity, defaultNodeAffinity,
			mergeNodeAffinity(defaultNodeAffinity, nodeAffinity))
	}
}

// mergeNodeAffinity merges a node affinity setting with the default node affinity: the required and the preferred node
// affinity of the setting each replace the respective default unless they are absent. A required node affinity without
// node selector terms removes the default required node affinity.
func mergeNodeAffinity(defaults *corev1.NodeAffinity, settings *corev1.NodeAffinity) *corev1.NodeAffinity {
	merged := ptr.Deref(defaults.DeepCopy(), corev1.NodeAffinity{})
	settings = settings.DeepCopy()
	if required := settings.RequiredDuringSchedulingIgnoredDuringExecution; required != nil {
		merged.RequiredDuringSchedulingIgnoredDuringExecution = nil
		if len(required.NodeSelectorTerms) > 0 {
			merged.RequiredDuringSchedulingIgnoredDuringExecution = required
		}
	}
	if preferred := settings.PreferredDuringSchedulingIgnoredDuringExecution; len(preferred) > 0 {
		merged.PreferredDuringSchedulingIgnoredDuringExecution = preferred
	}
	if merged.RequiredDuringSchedulingIgnoredDuringExecution == nil &&
		len(merged.PreferredDuringSchedulingIgnoredDuringExecution) == 0 {
		return nil
	}
	return &merged
}

func (m *componentSettingsApplier) applyCollectorPod(
	path string,
	helm collectorPodHelmValues,
	fields collectorPodFields,
	defaultProbes CollectorProbes,
	settings dash0v1alpha1.CollectorPodSettings,
) {
	if len(settings.Sysctls) > 0 {
		replaceValue(m, path+".sysctls", helm.sysctls, fields.sysctls, nil, slices.Clone(settings.Sysctls))
	}
	if probes := settings.Probes; probes != nil {
		replaceValue(m, path+".probes", helm.probes, fields.probes, defaultProbes, CollectorProbes{
			Liveness:  mergeProbe(defaultProbes.Liveness, probes.Liveness),
			Readiness: mergeProbe(defaultProbes.Readiness, probes.Readiness),
			Startup:   mergeProbe(defaultProbes.Startup, probes.Startup),
		})
	}
	if settings.PriorityClassName != "" {
		replaceValue(m, path+".priorityClassName", helm.priorityClassName, fields.priorityClassName, "",
			settings.PriorityClassName)
	}
}

// mergeProbe merges the timing settings of a probe with the default probe per field.
func mergeProbe(defaultProbe corev1.Probe, settings *dash0v1alpha1.ProbeSettings) corev1.Probe {
	probe := *defaultProbe.DeepCopy()
	if settings == nil {
		return probe
	}
	setIfPresent := func(target *int32, value *int32) {
		if value != nil {
			*target = *value
		}
	}
	setIfPresent(&probe.InitialDelaySeconds, settings.InitialDelaySeconds)
	setIfPresent(&probe.TimeoutSeconds, settings.TimeoutSeconds)
	setIfPresent(&probe.PeriodSeconds, settings.PeriodSeconds)
	setIfPresent(&probe.SuccessThreshold, settings.SuccessThreshold)
	setIfPresent(&probe.FailureThreshold, settings.FailureThreshold)
	if settings.TerminationGracePeriodSeconds != nil {
		probe.TerminationGracePeriodSeconds = ptr.To(*settings.TerminationGracePeriodSeconds)
	}
	return probe
}

// replaceValueIfSet replaces the target with the value of the setting, unless the setting is nil, see replaceValue.
func replaceValueIfSet[T any](
	m *componentSettingsApplier,
	path string,
	helmValue string,
	target *T,
	defaultValue T,
	setting *T,
) {
	if setting != nil {
		replaceValue(m, path, helmValue, target, defaultValue, *setting)
	}
}

// replaceValue replaces the target with the value of a setting. A value from the extra config map that differs from the
// default and from the value of the setting is recorded as a conflict.
func replaceValue[T any](
	m *componentSettingsApplier,
	path string,
	helmValue string,
	target *T,
	defaultValue T,
	value T,
) {
	if current := *target; !apiequality.Semantic.DeepEqual(current, defaultValue) &&
		!apiequality.Semantic.DeepEqual(current, value) {
		m.conflict(path, helmValue, current, value)
	}
	*target = value
}

func (m *componentSettingsApplier) conflict(path string, helmValue string, helmSetting any, resourceSetting any) {
	if !m.report {
		return
	}
	m.conflicts = append(m.conflicts, ComponentSettingConflict{
		Resource:        m.resource,
		Path:            path,
		HelmValue:       helmValue,
		HelmSetting:     formatSetting(helmSetting),
		ResourceSetting: formatSetting(resourceSetting),
	})
}

func formatSetting(setting any) string {
	if s, isString := setting.(string); isString {
		return s
	}
	serialized, err := json.Marshal(setting)
	if err != nil {
		return fmt.Sprintf("%v", setting)
	}
	return string(serialized)
}
