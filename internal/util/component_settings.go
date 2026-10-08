// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

const (
	signalControlKind = "Dash0SignalControl"

	componentSettingsPath = "spec.components"
)

// The names of the components in spec.components of the Dash0SignalControl resource.
const (
	componentSignalControlCollector = "collector"
	componentEdgeProxy              = "edgeProxy"
)

// ComponentSettingConflict is a setting that is configured both via the Helm chart and via spec.components of a custom
// resource, with different values. The value of the custom resource takes effect.
type ComponentSettingConflict struct {
	// Resource is the kind of the custom resource, e.g. Dash0SignalControl.
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

// String describes the conflict, it is used as an admission warning.
func (c ComponentSettingConflict) String() string {
	return fmt.Sprintf(
		"%s overrides the custom Helm value %s: %s instead of %s",
		c.Path, c.HelmValue, c.ResourceSetting, c.HelmSetting,
	)
}

// IsSameSetting reports whether both conflicts concern the same setting, regardless of the values.
func (c ComponentSettingConflict) IsSameSetting(other ComponentSettingConflict) bool {
	return c.Resource == other.Resource && c.Path == other.Path && c.HelmValue == other.HelmValue
}

// ComponentSettingsCheck is the result of checking spec.components of the Dash0SignalControl resource, see
// CheckComponentSettings.
type ComponentSettingsCheck struct {
	// Conflicts are the settings that override a different value provided via the Helm chart.
	Conflicts []ComponentSettingConflict
}

// EffectiveExtraConfig applies spec.components of the given resource (which may be nil) to the extra config. A setting
// replaces the value provided via the Helm chart. Neither the file configuration nor the resource are modified.
func EffectiveExtraConfig(
	fileConfig ExtraConfig,
	defaults ExtraConfig,
	signalControlResource *dash0v1alpha1.Dash0SignalControl,
) ExtraConfig {
	effective, _ := resolveComponentSettings(
		fileConfig,
		defaults,
		SignalControlComponentsOf(signalControlResource),
		false,
	)
	return effective
}

// SignalControlComponentsOf returns spec.components of the resource, or nil if the resource is nil.
func SignalControlComponentsOf(resource *dash0v1alpha1.Dash0SignalControl) *dash0v1alpha1.SignalControlComponents {
	if resource == nil {
		return nil
	}
	return resource.Spec.Components
}

// ComponentSettingConflicts returns the settings in spec.components that override a different value provided via Helm.
// The components may be nil. A file value only counts as provided via Helm if it differs from the given defaults, which
// need to match the defaults of the Helm chart.
func ComponentSettingConflicts(
	fileConfig ExtraConfig,
	defaults ExtraConfig,
	signalControlComponents *dash0v1alpha1.SignalControlComponents,
) []ComponentSettingConflict {
	_, conflicts := resolveComponentSettings(fileConfig, defaults, signalControlComponents, true)
	return conflicts
}

// CheckComponentSettings returns the conflicts (see ComponentSettingConflicts) of the settings in spec.components. The
// components may be nil.
func CheckComponentSettings(
	fileConfig ExtraConfig,
	defaults ExtraConfig,
	signalControlComponents *dash0v1alpha1.SignalControlComponents,
) ComponentSettingsCheck {
	return ComponentSettingsCheck{
		Conflicts: ComponentSettingConflicts(fileConfig, defaults, signalControlComponents),
	}
}

// resolveComponentSettings returns the effective extra configuration (see EffectiveExtraConfig), and, if report is set,
// the conflicts of the settings.
func resolveComponentSettings(
	fileConfig ExtraConfig,
	defaults ExtraConfig,
	signalControlComponents *dash0v1alpha1.SignalControlComponents,
	report bool,
) (ExtraConfig, []ComponentSettingConflict) {
	effective := fileConfig
	var conflicts []ComponentSettingConflict
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
	}

	if edgeProxy := components.EdgeProxy; edgeProxy != nil {
		p := path + "." + componentEdgeProxy
		replaceValueIfSet(m, p+".replicas", "operator.signalControl.edgeProxy.replicas",
			&effective.EdgeProxyReplicas, defaults.EdgeProxyReplicas, edgeProxy.Replicas)
		replaceValueIfSet(m, p+".enablePprof", "operator.signalControl.edgeProxy.enablePprof",
			&effective.EdgeProxyEnablePprof, defaults.EdgeProxyEnablePprof, edgeProxy.EnablePprof)
	}
}

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
