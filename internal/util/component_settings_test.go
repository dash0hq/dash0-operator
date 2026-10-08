// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("component settings", func() {

	resolve := func(
		fileConfig ExtraConfig,
		defaults ExtraConfig,
		signalControlComponents *dash0v1alpha1.SignalControlComponents,
	) (ExtraConfig, []ComponentSettingConflict) {
		return resolveComponentSettings(fileConfig, defaults, signalControlComponents, true)
	}

	resolveSignalControl := func(
		fileConfig ExtraConfig,
		components *dash0v1alpha1.SignalControlComponents,
	) (ExtraConfig, []ComponentSettingConflict) {
		return resolve(fileConfig, ExtraConfigDefaults, components)
	}

	toJson := func(value any) string {
		serialized, err := json.Marshal(value)
		Expect(err).ToNot(HaveOccurred())
		return string(serialized)
	}

	toleration := func(key string) corev1.Toleration {
		return corev1.Toleration{Key: key, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule}
	}

	Describe("without component settings", func() {
		It("should return the file configuration unchanged", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.DaemonSetTolerations = []corev1.Toleration{toleration("helm")}
			effective, conflicts := resolve(fileConfig, ExtraConfigDefaults, nil)
			Expect(conflicts).To(BeEmpty())
			Expect(toJson(effective)).To(MatchJSON(toJson(fileConfig)))
		})

		It("should return the file configuration unchanged from EffectiveExtraConfig without resources", func() {
			Expect(toJson(EffectiveExtraConfig(ExtraConfigDefaults, ExtraConfigDefaults, nil))).To(
				MatchJSON(toJson(ExtraConfigDefaults)))
		})
	})

	Describe("scalar values", func() {
		components := &dash0v1alpha1.SignalControlComponents{
			EdgeProxy: &dash0v1alpha1.EdgeProxySettings{Replicas: new(int32(3))},
		}

		It("should apply the value without a conflict if the Helm chart uses the default", func() {
			effective, conflicts := resolveSignalControl(ExtraConfigDefaults, components)
			Expect(effective.EdgeProxyReplicas).To(Equal(int32(3)))
			Expect(conflicts).To(BeEmpty())
		})

		It("should apply the value without a conflict if the Helm chart provides the same value", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.EdgeProxyReplicas = 3
			effective, conflicts := resolveSignalControl(fileConfig, components)
			Expect(effective.EdgeProxyReplicas).To(Equal(int32(3)))
			Expect(conflicts).To(BeEmpty())
		})

		It("should apply the value and report a conflict if the Helm chart provides a different value", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.EdgeProxyReplicas = 4
			effective, conflicts := resolveSignalControl(fileConfig, components)
			Expect(effective.EdgeProxyReplicas).To(Equal(int32(3)))
			Expect(conflicts).To(ConsistOf(ComponentSettingConflict{
				Resource:        "Dash0SignalControl",
				Path:            "spec.components.edgeProxy.replicas",
				HelmValue:       "operator.signalControl.edgeProxy.replicas",
				HelmSetting:     "4",
				ResourceSetting: "3",
			}))
			Expect(conflicts[0].String()).To(Equal(
				"spec.components.edgeProxy.replicas overrides the custom Helm value " +
					"operator.signalControl.edgeProxy.replicas: 3 instead of 4"))
		})

		It("should keep the value from the Helm chart if the setting is not provided", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.EdgeProxyReplicas = 4
			effective, conflicts := resolveSignalControl(fileConfig, &dash0v1alpha1.SignalControlComponents{
				EdgeProxy: &dash0v1alpha1.EdgeProxySettings{},
			})
			Expect(effective.EdgeProxyReplicas).To(Equal(int32(4)))
			Expect(conflicts).To(BeEmpty())
		})
	})

	Describe("Signal Control components", func() {
		It("should apply the settings of the Signal Control collector and the edge proxy", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.EdgeProxyReplicas = 4
			effective, conflicts := resolveSignalControl(fileConfig, &dash0v1alpha1.SignalControlComponents{
				Collector: &dash0v1alpha1.SignalControlCollectorSettings{
					Replicas: new(int32(3)),
				},
				EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
					Replicas:    new(int32(3)),
					EnablePprof: new(true),
				},
			})
			Expect(effective.SignalControlCollectorReplicas).To(Equal(int32(3)))
			Expect(effective.EdgeProxyReplicas).To(Equal(int32(3)))
			Expect(effective.EdgeProxyEnablePprof).To(BeTrue())
			Expect(conflicts).To(ConsistOf(ComponentSettingConflict{
				Resource:        "Dash0SignalControl",
				Path:            "spec.components.edgeProxy.replicas",
				HelmValue:       "operator.signalControl.edgeProxy.replicas",
				HelmSetting:     "4",
				ResourceSetting: "3",
			}))
		})
	})
})
