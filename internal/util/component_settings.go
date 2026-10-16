// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"unicode"
	"unicode/utf8"

	apiequality "k8s.io/apimachinery/pkg/api/equality"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

const (
	operatorConfigurationKind = "Dash0OperatorConfiguration"
	signalControlKind         = "Dash0SignalControl"

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
// setting replaces the value provided via the Helm chart. Invalid settings are rejected by the validating webhooks (see
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
		m.applyOperatorConfigurationComponents(&effective, operatorConfigurationComponents)
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
}

// workloadHelmValues are the names of the Helm values for the settings of one workload.
type workloadHelmValues struct {
	labels         string
	annotations    string
	podLabels      string
	podAnnotations string
}

// collectorHelmValues returns the names of the Helm values for a collector, which are all derived from one prefix, e.g.
// operator.collectors.daemonSet -> operator.collectors.daemonSetLabels.
func collectorHelmValues(prefix string) workloadHelmValues {
	return workloadHelmValues{
		labels:         prefix + "Labels",
		annotations:    prefix + "Annotations",
		podLabels:      prefix + "PodLabels",
		podAnnotations: prefix + "PodAnnotations",
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
	}
}

func (m *componentSettingsApplier) applyOperatorConfigurationComponents(
	effective *ExtraConfig,
	components *dash0v1alpha1.OperatorConfigurationComponents,
) {
	const path = componentSettingsPath

	if daemonSet := components.DaemonSetCollector; daemonSet != nil {
		p := path + "." + componentDaemonSetCollector
		m.applyWorkload(p, collectorHelmValues("operator.collectors.daemonSet"), workloadFields{
			labels:         &effective.CollectorDaemonSetLabels,
			annotations:    &effective.CollectorDaemonSetAnnotations,
			podLabels:      &effective.CollectorDaemonSetPodLabels,
			podAnnotations: &effective.CollectorDaemonSetPodAnnotations,
		}, daemonSet.WorkloadSettings)
	}

	if deployment := components.DeploymentCollector; deployment != nil {
		p := path + "." + componentDeploymentCollector
		m.applyWorkload(p, collectorHelmValues("operator.collectors.deployment"), workloadFields{
			labels:         &effective.CollectorDeploymentLabels,
			annotations:    &effective.CollectorDeploymentAnnotations,
			podLabels:      &effective.CollectorDeploymentPodLabels,
			podAnnotations: &effective.CollectorDeploymentPodAnnotations,
		}, deployment.WorkloadSettings)
	}

	if targetAllocator := components.TargetAllocator; targetAllocator != nil {
		p := path + "." + componentTargetAllocator
		m.applyWorkload(p, nestedHelmValues("operator.targetAllocator"), workloadFields{
			labels:         &effective.TargetAllocatorLabels,
			annotations:    &effective.TargetAllocatorAnnotations,
			podLabels:      &effective.TargetAllocatorPodLabels,
			podAnnotations: &effective.TargetAllocatorPodAnnotations,
		}, targetAllocator.WorkloadSettings)
	}

	if agent0Connector := components.Agent0Connector; agent0Connector != nil {
		p := path + "." + componentAgent0Connector
		m.applyWorkload(p, nestedHelmValues("operator.agent0Connector"), workloadFields{
			labels:         &effective.Agent0ConnectorLabels,
			annotations:    &effective.Agent0ConnectorAnnotations,
			podLabels:      &effective.Agent0ConnectorPodLabels,
			podAnnotations: &effective.Agent0ConnectorPodAnnotations,
		}, agent0Connector.WorkloadSettings)
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
		m.applyWorkload(p, collectorHelmValues("operator.collectors.signalControlCollector"), workloadFields{
			labels:         &effective.SignalControlCollectorLabels,
			annotations:    &effective.SignalControlCollectorAnnotations,
			podLabels:      &effective.SignalControlCollectorPodLabels,
			podAnnotations: &effective.SignalControlCollectorPodAnnotations,
		}, collector.WorkloadSettings)
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
		}, edgeProxy.WorkloadSettings)
	}
}

func (m *componentSettingsApplier) applyWorkload(
	path string,
	helm workloadHelmValues,
	fields workloadFields,
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
	serialized, err := json.Marshal(setting)
	if err != nil {
		return fmt.Sprintf("%v", setting)
	}
	return string(serialized)
}
