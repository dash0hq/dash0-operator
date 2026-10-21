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

		It("should return the file configuration unchanged if spec.components only has empty maps and lists", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDaemonSetLabels = map[string]string{"a": "helm"}
			fileConfig.DaemonSetTolerations = []corev1.Toleration{toleration("helm")}
			fileConfig.DeploymentSysctls = []corev1.Sysctl{{Name: "net.core.somaxconn", Value: "1024"}}
			fileConfig.EdgeProxyPodAnnotations = map[string]string{"a": "helm"}
			effective, conflicts := resolve(
				fileConfig,
				ExtraConfigDefaults,
				&dash0v1alpha1.OperatorConfigurationComponents{
					DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
						WorkloadSettings: dash0v1alpha1.WorkloadSettings{
							Labels:      map[string]string{},
							Tolerations: []corev1.Toleration{},
						},
					},
					DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{
						CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{Sysctls: []corev1.Sysctl{}},
					},
					TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{},
					Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{},
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

	Describe("empty objects", func() {
		DescribeTable("should apply empty probe settings as the default probes",
			func(probes *dash0v1alpha1.CollectorProbeSettings) {
				fileConfig := ExtraConfigDefaults
				fileConfig.SignalControlCollectorProbes = CollectorProbes{}
				effective, conflicts := resolveSignalControl(fileConfig, &dash0v1alpha1.SignalControlComponents{
					Collector: &dash0v1alpha1.SignalControlCollectorSettings{
						CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{Probes: probes},
					},
				})
				Expect(effective.SignalControlCollectorProbes).To(Equal(ExtraConfigDefaults.SignalControlCollectorProbes))
				Expect(conflicts).To(HaveLen(1))
			},
			Entry("without any probe", &dash0v1alpha1.CollectorProbeSettings{}),
			Entry("with an empty probe", &dash0v1alpha1.CollectorProbeSettings{Liveness: &dash0v1alpha1.ProbeSettings{}}),
		)

		It("should apply an empty node affinity as the default node affinity", func() {
			components := &dash0v1alpha1.OperatorConfigurationComponents{
				TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{NodeAffinity: &corev1.NodeAffinity{}},
				},
			}
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults, components)
			Expect(effective.TargetAllocatorNodeAffinity).To(Equal(ExtraConfigDefaults.TargetAllocatorNodeAffinity))
			Expect(conflicts).To(BeEmpty())
			Expect(invalidSettings(components, nil)).To(BeEmpty())

			By("reporting a conflict with a Helm value of null")
			fileConfig := ExtraConfigDefaults
			fileConfig.TargetAllocatorNodeAffinity = nil
			_, conflicts = resolveOperatorConfiguration(fileConfig, components)
			Expect(conflicts).To(HaveLen(1))
		})

		It("should apply empty mTLS settings as mTLS disabled", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.TargetAllocatorMtlsEnabled = true
			fileConfig.TargetAllocatorMtlsServerCertSecretName = "helm-server-cert"
			fileConfig.TargetAllocatorMtlsClientCertSecretName = "helm-client-cert"
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{MTls: &dash0v1alpha1.TargetAllocatorMTls{}},
			})
			Expect(effective.TargetAllocatorMtlsEnabled).To(BeFalse())
			Expect(effective.TargetAllocatorMtlsServerCertSecretName).To(BeEmpty())
			Expect(effective.TargetAllocatorMtlsClientCertSecretName).To(BeEmpty())
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].Path).To(Equal("spec.components.targetAllocator.mTls"))
		})

		DescribeTable("should not report mTLS settings that are disabled in both as a conflict",
			func(mTls *dash0v1alpha1.TargetAllocatorMTls) {
				fileConfig := ExtraConfigDefaults
				fileConfig.TargetAllocatorMtlsServerCertSecretName = "helm-server-cert"
				fileConfig.TargetAllocatorMtlsClientCertSecretName = "helm-client-cert"
				effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
					TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{MTls: mTls},
				})
				Expect(effective.TargetAllocatorMtlsEnabled).To(BeFalse())
				Expect(effective.TargetAllocatorMtlsServerCertSecretName).To(BeEmpty())
				Expect(effective.TargetAllocatorMtlsClientCertSecretName).To(BeEmpty())
				Expect(conflicts).To(BeEmpty())
			},
			Entry("empty mTLS settings", &dash0v1alpha1.TargetAllocatorMTls{}),
			Entry("disabled mTLS with secret names", &dash0v1alpha1.TargetAllocatorMTls{
				Enabled:              new(false),
				ServerCertSecretName: "server-cert",
				ClientCertSecretName: "client-cert",
			}),
		)
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

		It("should apply an explicit false", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.TargetAllocatorAllowInsecureAuthSecrets = true
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{AllowInsecureAuthSecrets: new(false)},
			})
			Expect(effective.TargetAllocatorAllowInsecureAuthSecrets).To(BeFalse())
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].HelmValue).To(Equal("operator.targetAllocator.allowInsecureAuthSecrets"))
		})

		It("should treat an empty string as not set", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDaemonSetPriorityClassName = "helm-priority"
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
					CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{PriorityClassName: ""},
				},
			})
			Expect(effective.CollectorDaemonSetPriorityClassName).To(Equal("helm-priority"))
			Expect(conflicts).To(BeEmpty())
		})

		It("should apply the target-allocator mTLS settings", func() {
			effective, conflicts := resolveOperatorConfiguration(
				ExtraConfigDefaults,
				&dash0v1alpha1.OperatorConfigurationComponents{
					TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
						MTls: &dash0v1alpha1.TargetAllocatorMTls{
							Enabled:              new(true),
							ServerCertSecretName: "server-cert",
							ClientCertSecretName: "client-cert",
						},
					},
				})
			Expect(conflicts).To(BeEmpty())
			Expect(effective.TargetAllocatorMtlsEnabled).To(BeTrue())
			Expect(effective.TargetAllocatorMtlsServerCertSecretName).To(Equal("server-cert"))
			Expect(effective.TargetAllocatorMtlsClientCertSecretName).To(Equal("client-cert"))
		})

		It("should replace the target-allocator mTLS settings of the Helm chart as a whole", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.TargetAllocatorMtlsEnabled = true
			fileConfig.TargetAllocatorMtlsServerCertSecretName = "helm-server-cert"
			fileConfig.TargetAllocatorMtlsClientCertSecretName = "helm-client-cert"
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
					MTls: &dash0v1alpha1.TargetAllocatorMTls{Enabled: new(false)},
				},
			})
			Expect(effective.TargetAllocatorMtlsEnabled).To(BeFalse())
			Expect(effective.TargetAllocatorMtlsServerCertSecretName).To(BeEmpty())
			Expect(effective.TargetAllocatorMtlsClientCertSecretName).To(BeEmpty())
			Expect(conflicts).To(ConsistOf(ComponentSettingConflict{
				Resource:  "Dash0OperatorConfiguration",
				Path:      "spec.components.targetAllocator.mTls",
				HelmValue: "operator.targetAllocator.mTls",
				HelmSetting: `{"enabled":true,"serverCertSecretName":"helm-server-cert",` +
					`"clientCertSecretName":"helm-client-cert"}`,
				ResourceSetting: `{"enabled":false,"serverCertSecretName":"","clientCertSecretName":""}`,
			}))
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
			Entry("a toleration with an unsupported operator", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{Tolerations: []corev1.Toleration{{
					Key:      "key",
					Operator: corev1.TolerationOpLt,
					Value:    "5",
				}}},
			}), `spec.components.daemonSetCollector.tolerations[0].operator: Unsupported value: "Lt"`),
			Entry("a toleration without a key and without operator Exists",
				daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{Tolerations: []corev1.Toleration{{
						Operator: corev1.TolerationOpEqual,
						Value:    "value",
					}}},
				}), `spec.components.daemonSetCollector.tolerations[0].operator: Invalid value: "Equal"`),
			Entry("a node selector requirement without values", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{NodeAffinity: &corev1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{{
						Weight: 1,
						Preference: corev1.NodeSelectorTerm{MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      "node-type",
							Operator: corev1.NodeSelectorOpIn,
						}}},
					}},
				}},
			}), "spec.components.daemonSetCollector.nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution[0]."+
				"preference.matchExpressions[0].values: Required value"),
			Entry("a preferred scheduling term with weight 0", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				WorkloadSettings: dash0v1alpha1.WorkloadSettings{NodeAffinity: &corev1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{{
						Preference: corev1.NodeSelectorTerm{MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      "node-type",
							Operator: corev1.NodeSelectorOpExists,
						}}},
					}},
				}},
			}), "spec.components.daemonSetCollector.nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution[0]."+
				"weight: Invalid value: 0"),
			Entry("a sysctl with an invalid name", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{
					Sysctls: []corev1.Sysctl{{Name: "net.core.Somaxconn", Value: "1024"}},
				},
			}), `spec.components.daemonSetCollector.sysctls[0].name: Invalid value: "net.core.Somaxconn"`),
			Entry("a duplicate sysctl", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{
					Sysctls: []corev1.Sysctl{
						{Name: "net.core.somaxconn", Value: "1024"},
						{Name: "net.core.somaxconn", Value: "2048"},
					},
				},
			}), `spec.components.daemonSetCollector.sysctls[1].name: Duplicate value: "net.core.somaxconn"`),
			Entry("an invalid priority class name", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{PriorityClassName: "High_Priority"},
			}), `spec.components.daemonSetCollector.priorityClassName: Invalid value: "High_Priority"`),
		)

		It("should report the invalid settings of all components of both resources", func() {
			Expect(invalidSettings(
				&dash0v1alpha1.OperatorConfigurationComponents{
					DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{
						CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{PriorityClassName: "High_Priority"},
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
						CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{
							Sysctls: []corev1.Sysctl{{Name: "net.core.Somaxconn", Value: "1024"}},
						},
					},
				},
			)).To(ConsistOf(
				ContainSubstring(`spec.components.deploymentCollector.priorityClassName: Invalid value: "High_Priority"`),
				ContainSubstring(`spec.components.targetAllocator.labels: Invalid value: "in valid"`),
				ContainSubstring(`spec.components.agent0Connector.annotations: Invalid value: "in valid"`),
				ContainSubstring(`spec.components.collector.sysctls[0].name: Invalid value: "net.core.Somaxconn"`),
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

	Describe("lists", func() {
		It("should replace the tolerations", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.DaemonSetTolerations = []corev1.Toleration{toleration("helm-1"), toleration("helm-2")}
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{
						Tolerations: []corev1.Toleration{toleration("resource")},
					},
				},
			})
			Expect(effective.DaemonSetTolerations).To(Equal([]corev1.Toleration{toleration("resource")}))
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].Path).To(Equal("spec.components.daemonSetCollector.tolerations"))
			Expect(conflicts[0].HelmValue).To(Equal("operator.collectors.daemonSetTolerations"))
			Expect(conflicts[0].HelmSetting).To(MatchJSON(toJson(fileConfig.DaemonSetTolerations)))
			Expect(fileConfig.DaemonSetTolerations).To(HaveLen(2))
		})

		It("should treat an empty list as not set", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.DaemonSetTolerations = []corev1.Toleration{toleration("helm")}
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{Tolerations: []corev1.Toleration{}},
				},
			})
			Expect(effective.DaemonSetTolerations).To(Equal([]corev1.Toleration{toleration("helm")}))
			Expect(conflicts).To(BeEmpty())
		})

		It("should not report the same list as a conflict", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.DeploymentSysctls = []corev1.Sysctl{{Name: "net.core.somaxconn", Value: "1024"}}
			_, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{
					CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{
						Sysctls: []corev1.Sysctl{{Name: "net.core.somaxconn", Value: "1024"}},
					},
				},
			})
			Expect(conflicts).To(BeEmpty())
		})
	})

	Describe("node affinity", func() {
		preferred := []corev1.PreferredSchedulingTerm{{
			Weight: 1,
			Preference: corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{{
					Key:      "node-type",
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"monitoring"},
				}},
			},
		}}
		defaultTermWith := func(requirements ...corev1.NodeSelectorRequirement) corev1.NodeSelectorTerm {
			term := ExtraConfigDefaults.DaemonSetNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.
				NodeSelectorTerms[0].DeepCopy()
			term.MatchExpressions = append(term.MatchExpressions, requirements...)
			return *term
		}

		daemonSetNodeAffinity := func(nodeAffinity *corev1.NodeAffinity) *dash0v1alpha1.OperatorConfigurationComponents {
			return &dash0v1alpha1.OperatorConfigurationComponents{
				DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{NodeAffinity: nodeAffinity},
				},
			}
		}

		It("should keep the default required node affinity if the setting only sets the preferred one", func() {
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults, daemonSetNodeAffinity(
				&corev1.NodeAffinity{PreferredDuringSchedulingIgnoredDuringExecution: preferred}))
			Expect(conflicts).To(BeEmpty())
			Expect(effective.DaemonSetNodeAffinity).To(Equal(&corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: ExtraConfigDefaults.DaemonSetNodeAffinity.
					RequiredDuringSchedulingIgnoredDuringExecution,
				PreferredDuringSchedulingIgnoredDuringExecution: preferred,
			}))
			Expect(ExtraConfigDefaults.DaemonSetNodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(BeEmpty())
		})

		It("should replace the default required node affinity with the one of the setting", func() {
			required := &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{preferred[0].Preference}}
			components := daemonSetNodeAffinity(&corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: required})
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults, components)
			Expect(conflicts).To(BeEmpty())
			Expect(effective.DaemonSetNodeAffinity).To(Equal(
				&corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: required}))
			Expect(CheckComponentSettings(ExtraConfigDefaults, ExtraConfigDefaults, components, nil)).To(
				Equal(ComponentSettingsCheck{}))
		})

		It("should remove the required node affinity with an empty list of node selector terms", func() {
			emptyRequired := &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{}}
			components := daemonSetNodeAffinity(
				&corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: emptyRequired})
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults, components)
			Expect(effective.DaemonSetNodeAffinity).To(BeNil())
			Expect(conflicts).To(BeEmpty())
			Expect(invalidSettings(components, nil)).To(BeEmpty())

			effective, _ = resolveOperatorConfiguration(ExtraConfigDefaults, daemonSetNodeAffinity(&corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution:  emptyRequired,
				PreferredDuringSchedulingIgnoredDuringExecution: preferred,
			}))
			Expect(effective.DaemonSetNodeAffinity).To(Equal(
				&corev1.NodeAffinity{PreferredDuringSchedulingIgnoredDuringExecution: preferred}))
		})

		It("should report a node affinity from the Helm chart that differs from the setting", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.TargetAllocatorNodeAffinity = &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: ExtraConfigDefaults.TargetAllocatorNodeAffinity.
					RequiredDuringSchedulingIgnoredDuringExecution,
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{{
					Weight:     5,
					Preference: preferred[0].Preference,
				}},
			}
			_, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{defaultTermWith()},
							},
							PreferredDuringSchedulingIgnoredDuringExecution: preferred,
						},
					},
				},
			})
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].Path).To(Equal("spec.components.targetAllocator.nodeAffinity"))
			Expect(conflicts[0].HelmValue).To(Equal("operator.targetAllocator.nodeAffinity"))
			Expect(fileConfig.TargetAllocatorNodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight).To(
				Equal(int32(5)))
		})

	})

	Describe("probes", func() {
		It("should merge the probes with the default probes, not with the probes of the Helm chart", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.DaemonSetProbes.Liveness.TimeoutSeconds = 5
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
					CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{
						Probes: &dash0v1alpha1.CollectorProbeSettings{
							Liveness: &dash0v1alpha1.ProbeSettings{
								PeriodSeconds:                 new(int32(10)),
								TerminationGracePeriodSeconds: new(int64(30)),
							},
						},
					},
				},
			})
			Expect(effective.DaemonSetProbes).To(Equal(CollectorProbes{
				Liveness: corev1.Probe{
					PeriodSeconds:                 10,
					TimeoutSeconds:                1,
					FailureThreshold:              20,
					TerminationGracePeriodSeconds: new(int64(30)),
				},
				Readiness: ExtraConfigDefaults.DaemonSetProbes.Readiness,
				Startup:   ExtraConfigDefaults.DaemonSetProbes.Startup,
			}))
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].Path).To(Equal("spec.components.daemonSetCollector.probes"))
			Expect(conflicts[0].HelmValue).To(Equal("operator.collectors.daemonSetProbes"))
			Expect(fileConfig.DaemonSetProbes.Liveness.PeriodSeconds).To(Equal(int32(2)))
		})

		It("should not report a conflict if the Helm chart uses the default probes", func() {
			_, conflicts := resolveSignalControl(ExtraConfigDefaults, &dash0v1alpha1.SignalControlComponents{
				Collector: &dash0v1alpha1.SignalControlCollectorSettings{
					CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{
						Probes: &dash0v1alpha1.CollectorProbeSettings{
							Startup: &dash0v1alpha1.ProbeSettings{FailureThreshold: new(int32(90))},
						},
					},
				},
			})
			Expect(conflicts).To(BeEmpty())
		})

		It("should apply every timing setting of a probe", func() {
			effective, _ := resolveSignalControl(ExtraConfigDefaults, &dash0v1alpha1.SignalControlComponents{
				Collector: &dash0v1alpha1.SignalControlCollectorSettings{
					CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{
						Probes: &dash0v1alpha1.CollectorProbeSettings{
							Readiness: &dash0v1alpha1.ProbeSettings{
								InitialDelaySeconds: new(int32(1)),
								TimeoutSeconds:      new(int32(2)),
								PeriodSeconds:       new(int32(3)),
								SuccessThreshold:    new(int32(4)),
								FailureThreshold:    new(int32(5)),
							},
						},
					},
				},
			})
			Expect(effective.SignalControlCollectorProbes.Readiness).To(Equal(corev1.Probe{
				InitialDelaySeconds: 1,
				TimeoutSeconds:      2,
				PeriodSeconds:       3,
				SuccessThreshold:    4,
				FailureThreshold:    5,
			}))
		})
	})

	Describe("filelog offset storage volume", func() {
		It("should use the persistent volume claim of the setting", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorFilelogOffsetStorageVolume = &corev1.Volume{
				Name: "filelogreceiver-offsets",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/dash0"},
				},
			}
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
					FilelogOffsetStorageVolume: &dash0v1alpha1.FilelogOffsetStorageVolume{
						PersistentVolumeClaim: &dash0v1alpha1.FilelogOffsetStoragePersistentVolumeClaim{
							ClaimName: "offsets",
						},
					},
				},
			})
			Expect(effective.CollectorFilelogOffsetStorageVolume).To(Equal(&corev1.Volume{
				Name: "filelogreceiver-offsets",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "offsets"},
				},
			}))
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].HelmValue).To(Equal("operator.collectors.filelogOffsetSyncStorageVolume"))
			Expect(fileConfig.CollectorFilelogOffsetStorageVolume.HostPath).ToNot(BeNil())
		})

		It("should use the host path of the setting", func() {
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults,
				&dash0v1alpha1.OperatorConfigurationComponents{
					DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
						FilelogOffsetStorageVolume: &dash0v1alpha1.FilelogOffsetStorageVolume{
							HostPath: &dash0v1alpha1.FilelogOffsetStorageHostPath{Path: "/var/lib/dash0"},
						},
					},
				})
			Expect(effective.CollectorFilelogOffsetStorageVolume).To(Equal(&corev1.Volume{
				Name: "filelogreceiver-offsets",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/var/lib/dash0",
						Type: new(corev1.HostPathDirectoryOrCreate),
					},
				},
			}))
			Expect(conflicts).To(BeEmpty())
		})
	})

	Describe("SELinux options", func() {
		withSeLinuxOptions := func(seLinuxOptions *corev1.SELinuxOptions) *dash0v1alpha1.OperatorConfigurationComponents {
			return &dash0v1alpha1.OperatorConfigurationComponents{
				DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{SELinuxOptions: seLinuxOptions},
			}
		}
		withHelmSeLinuxOptions := func() ExtraConfig {
			fileConfig := ExtraConfigDefaults
			fileConfig.DaemonSetSELinuxOptions = &corev1.SELinuxOptions{Type: "helm_t", Level: "s0:c1,c2"}
			return fileConfig
		}

		It("should replace the SELinux options of the Helm chart as a whole", func() {
			components := withSeLinuxOptions(&corev1.SELinuxOptions{Type: "resource_t"})
			effective, conflicts := resolveOperatorConfiguration(withHelmSeLinuxOptions(), components)
			Expect(effective.DaemonSetSELinuxOptions).To(Equal(&corev1.SELinuxOptions{Type: "resource_t"}))
			Expect(effective.DaemonSetSELinuxOptions).ToNot(BeIdenticalTo(components.DaemonSetCollector.SELinuxOptions))
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].Path).To(Equal("spec.components.daemonSetCollector.seLinuxOptions"))
			Expect(conflicts[0].HelmValue).To(Equal("operator.collectors.daemonSetSeLinuxOptions"))
			Expect(invalidSettings(components, nil)).To(BeEmpty())
		})

		It("should apply the SELinux options without a conflict if the Helm chart does not set any", func() {
			effective, conflicts := resolveOperatorConfiguration(
				ExtraConfigDefaults, withSeLinuxOptions(&corev1.SELinuxOptions{Type: "resource_t"}))
			Expect(effective.DaemonSetSELinuxOptions).To(Equal(&corev1.SELinuxOptions{Type: "resource_t"}))
			Expect(conflicts).To(BeEmpty())
		})

		It("should not report the same SELinux options as a conflict", func() {
			_, conflicts := resolveOperatorConfiguration(
				withHelmSeLinuxOptions(), withSeLinuxOptions(&corev1.SELinuxOptions{Type: "helm_t", Level: "s0:c1,c2"}))
			Expect(conflicts).To(BeEmpty())
		})

		It("should remove the SELinux options of the Helm chart with an empty object", func() {
			effective, conflicts := resolveOperatorConfiguration(
				withHelmSeLinuxOptions(), withSeLinuxOptions(&corev1.SELinuxOptions{}))
			Expect(effective.DaemonSetSELinuxOptions).To(BeNil())
			Expect(conflicts).To(HaveLen(1))
		})

		It("should keep the SELinux options of the Helm chart if the setting is absent", func() {
			effective, conflicts := resolveOperatorConfiguration(withHelmSeLinuxOptions(), withSeLinuxOptions(nil))
			Expect(effective.DaemonSetSELinuxOptions).To(Equal(&corev1.SELinuxOptions{Type: "helm_t", Level: "s0:c1,c2"}))
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

	It("should not modify the defaults", func() {
		defaultsBefore := toJson(ExtraConfigDefaults)
		_, _ = resolve(
			ExtraConfigDefaults,
			ExtraConfigDefaults,
			&dash0v1alpha1.OperatorConfigurationComponents{
				DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{
						Labels: map[string]string{"a": "b"},
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{{
									MatchExpressions: []corev1.NodeSelectorRequirement{{
										Key:      "node-type",
										Operator: corev1.NodeSelectorOpExists,
									}},
								}},
							},
						},
					},
					CollectorPodSettings: dash0v1alpha1.CollectorPodSettings{
						Probes: &dash0v1alpha1.CollectorProbeSettings{
							Readiness: &dash0v1alpha1.ProbeSettings{
								PeriodSeconds:                 new(int32(7)),
								TerminationGracePeriodSeconds: new(int64(5)),
							},
						},
					},
				},
				DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{
					WorkloadSettings: dash0v1alpha1.WorkloadSettings{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{},
							},
						},
					},
				},
			},
			nil,
		)
		Expect(toJson(ExtraConfigDefaults)).To(MatchJSON(defaultsBefore))
	})
})
