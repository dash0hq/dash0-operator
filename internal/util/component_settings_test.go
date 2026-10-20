// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"strings"

	corev1 "k8s.io/api/core/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("component settings", func() {

	resolve := func(
		fileConfig ExtraConfig,
		defaults ExtraConfig,
		operatorConfigurationComponents *dash0v1alpha1.OperatorConfigurationComponents,
		signalControlComponents *dash0v1alpha1.SignalControlComponents,
	) (ExtraConfig, []ComponentSettingConflict) {
		return resolveComponentSettings(
			fileConfig, defaults, operatorConfigurationComponents, signalControlComponents, true)
	}

	resolveOperatorConfiguration := func(
		fileConfig ExtraConfig,
		components *dash0v1alpha1.OperatorConfigurationComponents,
	) (ExtraConfig, []ComponentSettingConflict) {
		return resolve(fileConfig, ExtraConfigDefaults, components, nil)
	}

	resolveSignalControl := func(
		fileConfig ExtraConfig,
		components *dash0v1alpha1.SignalControlComponents,
	) (ExtraConfig, []ComponentSettingConflict) {
		return resolve(fileConfig, ExtraConfigDefaults, nil, components)
	}

	invalidSettings := func(
		operatorConfigurationComponents *dash0v1alpha1.OperatorConfigurationComponents,
		signalControlComponents *dash0v1alpha1.SignalControlComponents,
	) []string {
		return CheckComponentSettings(
			ExtraConfigDefaults, ExtraConfigDefaults, operatorConfigurationComponents, signalControlComponents).Invalid
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
			effective, conflicts := resolve(fileConfig, ExtraConfigDefaults, nil, nil)
			Expect(conflicts).To(BeEmpty())
			Expect(toJson(effective)).To(MatchJSON(toJson(fileConfig)))
		})

		It("should return the file configuration unchanged if spec.components only has empty maps", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDaemonSetLabels = map[string]string{"a": "helm"}
			fileConfig.EdgeProxyPodAnnotations = map[string]string{"a": "helm"}
			effective, conflicts := resolve(
				fileConfig,
				ExtraConfigDefaults,
				&dash0v1alpha1.OperatorConfigurationComponents{
					DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
						WorkloadSettings: dash0v1alpha1.WorkloadSettings{Labels: map[string]string{}},
					},
					DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{},
					TargetAllocator:     &dash0v1alpha1.TargetAllocatorSettings{},
					Agent0Connector:     &dash0v1alpha1.Agent0ConnectorSettings{},
				},
				&dash0v1alpha1.SignalControlComponents{
					Collector: &dash0v1alpha1.SignalControlCollectorSettings{},
					EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
						WorkloadSettings: dash0v1alpha1.WorkloadSettings{PodAnnotations: map[string]string{}},
					},
				},
			)
			Expect(conflicts).To(BeEmpty())
			Expect(toJson(effective)).To(MatchJSON(toJson(fileConfig)))
		})

		It("should return the file configuration unchanged from EffectiveExtraConfig without resources", func() {
			Expect(toJson(EffectiveExtraConfig(ExtraConfigDefaults, ExtraConfigDefaults, nil, nil))).To(
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

	Describe("maps", func() {
		It("should replace the labels and report a conflict", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDaemonSetLabels = map[string]string{"a": "helm", "b": "helm"}
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{
						Labels: map[string]string{"b": "resource", "c": "resource"},
					},
				},
			})
			Expect(effective.CollectorDaemonSetLabels).To(Equal(map[string]string{"b": "resource", "c": "resource"}))
			Expect(conflicts).To(ConsistOf(ComponentSettingConflict{
				Resource:        "Dash0OperatorConfiguration",
				Path:            "spec.components.daemonSetCollector.labels",
				HelmValue:       "operator.collectors.daemonSetLabels",
				HelmSetting:     `{"a":"helm","b":"helm"}`,
				ResourceSetting: `{"b":"resource","c":"resource"}`,
			}))
			Expect(fileConfig.CollectorDaemonSetLabels).To(Equal(map[string]string{"a": "helm", "b": "helm"}))
		})

		It("should not report the same labels as a conflict", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDeploymentPodLabels = map[string]string{"a": "b"}
			_, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{PodLabels: map[string]string{"a": "b"}},
				},
			})
			Expect(conflicts).To(BeEmpty())
		})

		It("should use the nested Helm value names for the target-allocator", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.TargetAllocatorPodAnnotations = map[string]string{"a": "helm"}
			_, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{
						PodAnnotations: map[string]string{"a": "resource"},
					},
				},
			})
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].Path).To(Equal("spec.components.targetAllocator.podAnnotations"))
			Expect(conflicts[0].HelmValue).To(Equal("operator.targetAllocator.podAnnotations"))
		})
	})

	Describe("validation", func() {
		daemonSetCollector := func(
			settings dash0v1alpha1.DaemonSetCollectorSettings,
		) *dash0v1alpha1.OperatorConfigurationComponents {
			return &dash0v1alpha1.OperatorConfigurationComponents{DaemonSetCollector: &settings}
		}

		DescribeTable("should report an invalid setting",
			func(components *dash0v1alpha1.OperatorConfigurationComponents, expectedReason string) {
				invalid := invalidSettings(components, nil)
				Expect(invalid).ToNot(BeEmpty())
				Expect(invalid[0]).To(ContainSubstring(expectedReason))
			},
			Entry("a label with an invalid key", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{Labels: map[string]string{"in valid": "value"}},
			}), `spec.components.daemonSetCollector.labels: Invalid value: "in valid"`),
			Entry("a pod label with an invalid value", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{PodLabels: map[string]string{"key": "in valid"}},
			}), `spec.components.daemonSetCollector.podLabels: Invalid value: "in valid"`),
			Entry("an annotation with an invalid key", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{PodAnnotations: map[string]string{"in valid": "value"}},
			}), `spec.components.daemonSetCollector.podAnnotations: Invalid value: "in valid"`),
		)

		It("should report the invalid settings of all components of both resources", func() {
			Expect(invalidSettings(
				&dash0v1alpha1.OperatorConfigurationComponents{
					DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{
						WorkloadSettings: dash0v1alpha1.WorkloadSettings{PodLabels: map[string]string{"key": "in valid"}},
					},
					TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
						WorkloadSettings: dash0v1alpha1.WorkloadSettings{Labels: map[string]string{"in valid": "value"}},
					},
					Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{
						WorkloadSettings: dash0v1alpha1.WorkloadSettings{
							Annotations: map[string]string{"in valid": "value"},
						},
					},
				},
				&dash0v1alpha1.SignalControlComponents{
					Collector: &dash0v1alpha1.SignalControlCollectorSettings{
						WorkloadSettings: dash0v1alpha1.WorkloadSettings{
							PodAnnotations: map[string]string{"in valid": "value"},
						},
					},
				},
			)).To(ConsistOf(
				ContainSubstring(`spec.components.deploymentCollector.podLabels: Invalid value: "in valid"`),
				ContainSubstring(`spec.components.targetAllocator.labels: Invalid value: "in valid"`),
				ContainSubstring(`spec.components.agent0Connector.annotations: Invalid value: "in valid"`),
				ContainSubstring(`spec.components.collector.podAnnotations: Invalid value: "in valid"`),
			))
		})

		It("should apply invalid settings as they are, since the validating webhooks reject them", func() {
			operatorConfigurationResource := &dash0v1alpha1.Dash0OperatorConfiguration{
				Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Components: daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
						WorkloadSettings: dash0v1alpha1.WorkloadSettings{Labels: map[string]string{"in valid": "value"}},
					}),
				},
			}
			effective := EffectiveExtraConfig(ExtraConfigDefaults, ExtraConfigDefaults, operatorConfigurationResource, nil)
			Expect(effective.CollectorDaemonSetLabels).To(Equal(map[string]string{"in valid": "value"}))
		})

		It("should report the conflicts of invalid settings without validating them", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDaemonSetLabels = map[string]string{"a": "helm"}
			components := daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{Labels: map[string]string{"in valid": "value"}},
			})
			Expect(invalidSettings(components, nil)).ToNot(BeEmpty())
			Expect(ComponentSettingConflicts(fileConfig, ExtraConfigDefaults, components, nil)).To(ConsistOf(
				ComponentSettingConflict{
					Resource:        "Dash0OperatorConfiguration",
					Path:            "spec.components.daemonSetCollector.labels",
					HelmValue:       "operator.collectors.daemonSetLabels",
					HelmSetting:     `{"a":"helm"}`,
					ResourceSetting: `{"in valid":"value"}`,
				}))
		})
	})

	Describe("ComponentSettingConflict.String", func() {
		It("should leave out values that are too long for an admission warning", func() {
			conflict := ComponentSettingConflict{
				Path:            "spec.components.daemonSetCollector.labels",
				HelmValue:       "operator.collectors.daemonSetLabels",
				HelmSetting:     strings.Repeat("a", 65),
				ResourceSetting: `{"a":"b"}`,
			}
			Expect(conflict.String()).To(Equal("spec.components.daemonSetCollector.labels overrides the custom " +
				"Helm value operator.collectors.daemonSetLabels"))
		})

		It("should leave out values with control characters", func() {
			conflict := ComponentSettingConflict{
				Path:            "spec.components.daemonSetCollector.annotations",
				HelmValue:       "operator.collectors.daemonSetAnnotations",
				HelmSetting:     "{\"a\":\"x\u0085y\"}",
				ResourceSetting: "value",
			}
			Expect(conflict.String()).To(Equal("spec.components.daemonSetCollector.annotations overrides the custom " +
				"Helm value operator.collectors.daemonSetAnnotations"))
		})
	})

	Describe("Signal Control components", func() {
		It("should apply the settings of the Signal Control collector and the edge proxy", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.EdgeProxyReplicas = 4
			effective, conflicts := resolveSignalControl(fileConfig, &dash0v1alpha1.SignalControlComponents{
				Collector: &dash0v1alpha1.SignalControlCollectorSettings{
					Replicas: new(int32(3)),
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{
						PodLabels: map[string]string{"team": "observability"},
					},
				},
				EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
					Replicas:    new(int32(3)),
					EnablePprof: new(true),
				},
			})
			Expect(effective.SignalControlCollectorReplicas).To(Equal(int32(3)))
			Expect(effective.SignalControlCollectorPodLabels).To(Equal(map[string]string{"team": "observability"}))
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

		It("should apply the settings of both resources", func() {
			effective, _ := resolve(
				ExtraConfigDefaults,
				ExtraConfigDefaults,
				&dash0v1alpha1.OperatorConfigurationComponents{
					Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{
						WorkloadSettings: dash0v1alpha1.WorkloadSettings{Labels: map[string]string{"team": "observability"}},
					},
				},
				&dash0v1alpha1.SignalControlComponents{
					EdgeProxy: &dash0v1alpha1.EdgeProxySettings{Replicas: new(int32(3))},
				},
			)
			Expect(effective.Agent0ConnectorLabels).To(Equal(map[string]string{"team": "observability"}))
			Expect(effective.EdgeProxyReplicas).To(Equal(int32(3)))
		})
	})
})
