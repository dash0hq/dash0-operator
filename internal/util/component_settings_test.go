// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"maps"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

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

	memoryLimit := func(limit string) dash0v1alpha1.ContainerResources {
		return dash0v1alpha1.ContainerResources{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(limit)},
		}
	}

	daemonSetCollectorResources := func(
		settings *dash0v1alpha1.ContainerResourcesWithGoMemLimit,
	) *dash0v1alpha1.OperatorConfigurationComponents {
		return &dash0v1alpha1.OperatorConfigurationComponents{
			DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{CollectorContainerResources: settings},
		}
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
					InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{},
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
		It("should apply empty container resources as the default resources", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDaemonSetCollectorContainerResources = ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			}
			components := daemonSetCollectorResources(&dash0v1alpha1.ContainerResourcesWithGoMemLimit{})
			effective, conflicts := resolveOperatorConfiguration(fileConfig, components)
			Expect(toJson(effective.CollectorDaemonSetCollectorContainerResources)).To(
				MatchJSON(toJson(ExtraConfigDefaults.CollectorDaemonSetCollectorContainerResources)))
			Expect(conflicts).To(HaveLen(1))
			Expect(invalidSettings(components, nil)).To(BeEmpty())
		})

		It("should apply empty resources of the instrumentation init container as the default resources", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.InstrumentationInitContainerResources = ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
			}
			components := &dash0v1alpha1.OperatorConfigurationComponents{
				InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{
					Resources: &dash0v1alpha1.ContainerResources{},
				},
			}
			effective, conflicts := resolveOperatorConfiguration(fileConfig, components)
			Expect(effective.InstrumentationInitContainerResources).To(Equal(ResourceRequirementsWithGoMemLimit{}))
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].HelmValue).To(Equal("operator.initContainerResources"))

			effective, _ = resolve(fileConfig, ExtraConfigDefaultsFor(true, false), components, nil)
			Expect(toJson(effective.InstrumentationInitContainerResources)).To(
				MatchJSON(toJson(ExtraConfigDefaultsFor(true, false).InstrumentationInitContainerResources)))
		})

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
		components := &dash0v1alpha1.OperatorConfigurationComponents{
			Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{MaxConcurrentCommands: new(int32(4))},
		}

		It("should apply the value without a conflict if the Helm chart uses the default", func() {
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults, components)
			Expect(effective.Agent0ConnectorMaxConcurrentCommands).To(Equal(int32(4)))
			Expect(conflicts).To(BeEmpty())
		})

		It("should apply the value without a conflict if the Helm chart provides the same value", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.Agent0ConnectorMaxConcurrentCommands = 4
			effective, conflicts := resolveOperatorConfiguration(fileConfig, components)
			Expect(effective.Agent0ConnectorMaxConcurrentCommands).To(Equal(int32(4)))
			Expect(conflicts).To(BeEmpty())
		})

		It("should apply the value and report a conflict if the Helm chart provides a different value", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.Agent0ConnectorMaxConcurrentCommands = 3
			effective, conflicts := resolveOperatorConfiguration(fileConfig, components)
			Expect(effective.Agent0ConnectorMaxConcurrentCommands).To(Equal(int32(4)))
			Expect(conflicts).To(ConsistOf(ComponentSettingConflict{
				Resource:        "Dash0OperatorConfiguration",
				Path:            "spec.components.agent0Connector.maxConcurrentCommands",
				HelmValue:       "operator.agent0Connector.maxConcurrentCommands",
				HelmSetting:     "3",
				ResourceSetting: "4",
			}))
			Expect(conflicts[0].String()).To(Equal(
				"spec.components.agent0Connector.maxConcurrentCommands overrides the custom Helm value " +
					"operator.agent0Connector.maxConcurrentCommands: 4 instead of 3"))
		})

		It("should keep the value from the Helm chart if the setting is not provided", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.Agent0ConnectorMaxConcurrentCommands = 3
			effective, conflicts := resolveOperatorConfiguration(fileConfig, &dash0v1alpha1.OperatorConfigurationComponents{
				Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{},
			})
			Expect(effective.Agent0ConnectorMaxConcurrentCommands).To(Equal(int32(3)))
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

	Describe("container resources", func() {
		It("should merge the resources with the defaults, not with the resources of the Helm chart", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDaemonSetCollectorContainerResources = ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("750Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("750Mi"),
				},
				GoMemLimit: "600MiB",
			}
			effective, conflicts := resolveOperatorConfiguration(fileConfig, daemonSetCollectorResources(
				&dash0v1alpha1.ContainerResourcesWithGoMemLimit{ContainerResources: memoryLimit("1Gi")}))
			Expect(toJson(effective.CollectorDaemonSetCollectorContainerResources)).To(
				MatchJSON(`{"limits":{"memory":"1Gi"},"requests":{"memory":"500Mi"}}`))
			Expect(conflicts).To(ConsistOf(ComponentSettingConflict{
				Resource:            "Dash0OperatorConfiguration",
				Path:                "spec.components.daemonSetCollector.collectorContainerResources",
				HelmValue:           "operator.collectors.daemonSetCollectorContainerResources",
				DeprecatedHelmValue: "operator.collectorDaemonSetCollectorContainerResources",
				HelmSetting: `{"limits":{"cpu":"1","memory":"750Mi"},"requests":{"memory":"750Mi"},` +
					`"gomemlimit":"600MiB"}`,
				ResourceSetting: `{"limits":{"memory":"1Gi"},"requests":{"memory":"500Mi"}}`,
			}))
			Expect(conflicts[0].String()).To(Equal(
				"spec.components.daemonSetCollector.collectorContainerResources overrides the custom Helm value " +
					"operator.collectors.daemonSetCollectorContainerResources (or " +
					"operator.collectorDaemonSetCollectorContainerResources)"))
			Expect(toJson(fileConfig.CollectorDaemonSetCollectorContainerResources.Limits)).To(
				MatchJSON(`{"cpu":"1","memory":"750Mi"}`))
		})

		It("should keep the defaults of the resources that the setting leaves out", func() {
			effective, conflicts := resolveSignalControl(ExtraConfigDefaults, &dash0v1alpha1.SignalControlComponents{
				EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
					ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
						ContainerResources: dash0v1alpha1.ContainerResources{
							Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
						},
					},
				},
			})
			Expect(toJson(effective.EdgeProxyContainerResources)).To(MatchJSON(
				`{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"50m","memory":"128Mi"}}`))
			Expect(conflicts).To(BeEmpty())
		})

		It("should lower a default request above a limit of the setting to that limit", func() {
			components := daemonSetCollectorResources(
				&dash0v1alpha1.ContainerResourcesWithGoMemLimit{ContainerResources: memoryLimit("300Mi")})
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults, components)
			Expect(toJson(effective.CollectorDaemonSetCollectorContainerResources)).To(
				MatchJSON(`{"limits":{"memory":"300Mi"},"requests":{"memory":"300Mi"}}`))
			Expect(conflicts).To(BeEmpty())
			Expect(invalidSettings(components, nil)).To(BeEmpty())
		})

		It("should raise a default limit below a request of the setting to that request", func() {
			components := daemonSetCollectorResources(&dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: dash0v1alpha1.ContainerResources{
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				},
			})
			effective, _ := resolveOperatorConfiguration(ExtraConfigDefaults, components)
			Expect(toJson(effective.CollectorDaemonSetCollectorContainerResources)).To(
				MatchJSON(`{"limits":{"memory":"1Gi"},"requests":{"memory":"1Gi"}}`))
			Expect(invalidSettings(components, nil)).To(BeEmpty())
		})

		DescribeTable("should remove a resource including its default with -1",
			func(settings dash0v1alpha1.ContainerResources, expected string) {
				components := &dash0v1alpha1.SignalControlComponents{
					EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
						ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{ContainerResources: settings},
					},
				}
				effective, _ := resolveSignalControl(ExtraConfigDefaults, components)
				Expect(toJson(effective.EdgeProxyContainerResources)).To(MatchJSON(expected))
				Expect(invalidSettings(nil, components)).To(BeEmpty())
			},
			Entry("a limit", dash0v1alpha1.ContainerResources{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("-1")},
			}, `{"limits":{"cpu":"500m"},"requests":{"cpu":"50m","memory":"128Mi"}}`),
			Entry("a request, written as millicores", dash0v1alpha1.ContainerResources{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("-1000m")},
			}, `{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"memory":"128Mi"}}`),
			Entry("all resources", dash0v1alpha1.ContainerResources{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("-1"),
					corev1.ResourceMemory: resource.MustParse("-1"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("-1"),
					corev1.ResourceMemory: resource.MustParse("-1"),
				},
			}, `{}`),
			Entry("a limit, together with a request above the default limit", dash0v1alpha1.ContainerResources{
				Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("-1")},
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
			}, `{"limits":{"memory":"512Mi"},"requests":{"cpu":"2","memory":"128Mi"}}`),
		)

		It("should report negative quantities other than -1 as invalid", func() {
			Expect(invalidSettings(daemonSetCollectorResources(&dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: dash0v1alpha1.ContainerResources{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("-2")},
				},
			}), nil)).To(ConsistOf(
				`spec.components.daemonSetCollector.collectorContainerResources.limits[memory]: Invalid value: "-2": ` +
					`must be greater than 0, or -1 to remove the resource`,
			))
		})

		It("should merge the resources of the instrumentation init container with the defaults", func() {
			components := &dash0v1alpha1.OperatorConfigurationComponents{
				InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{
					Resources: &dash0v1alpha1.ContainerResources{
						Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
					},
				},
			}
			Expect(toJson(EffectiveInstrumentationInitContainerResources(ExtraConfigDefaultsFor(true, false), components))).To(
				MatchJSON(`{"limits":{"ephemeral-storage":"500Mi","memory":"256Mi"},` +
					`"requests":{"cpu":"100m","ephemeral-storage":"500Mi","memory":"128Mi"}}`))
			Expect(toJson(EffectiveInstrumentationInitContainerResources(ExtraConfigDefaults, components))).To(
				MatchJSON(`{"limits":{"memory":"256Mi"}}`))
			Expect(EffectiveInstrumentationInitContainerResources(ExtraConfigDefaults,
				&dash0v1alpha1.OperatorConfigurationComponents{
					InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{},
				})).To(BeNil())
			Expect(EffectiveInstrumentationInitContainerResources(ExtraConfigDefaults, nil)).To(BeNil())
		})

		It("should report a limit of 0 as invalid, but not a request of 0", func() {
			Expect(invalidSettings(daemonSetCollectorResources(&dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: dash0v1alpha1.ContainerResources{
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("0")},
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("0")},
				},
				GoMemLimit: "1TiB",
			}), nil)).To(ConsistOf(
				`spec.components.daemonSetCollector.collectorContainerResources.limits[memory]: Invalid value: "0": ` +
					`must be greater than 0, or -1 to remove the resource`,
			))
		})

		DescribeTable("should apply and validate the resources of every container",
			func(
				components func(
					*dash0v1alpha1.ContainerResourcesWithGoMemLimit,
				) (*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents),
				effectiveResources func(*ExtraConfig) *ResourceRequirementsWithGoMemLimit,
				path string,
			) {
				operatorConfigurationComponents, signalControlComponents := components(
					&dash0v1alpha1.ContainerResourcesWithGoMemLimit{ContainerResources: memoryLimit("123Mi")})
				effective, _ := resolve(
					ExtraConfigDefaults, ExtraConfigDefaults, operatorConfigurationComponents, signalControlComponents)
				Expect(effectiveResources(&effective).Limits.Memory().String()).To(Equal("123Mi"))

				operatorConfigurationComponents, signalControlComponents = components(
					&dash0v1alpha1.ContainerResourcesWithGoMemLimit{
						ContainerResources: dash0v1alpha1.ContainerResources{
							Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("100Mi")},
							Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("200Mi")},
						},
					})
				Expect(invalidSettings(operatorConfigurationComponents, signalControlComponents)).To(ConsistOf(
					path + `.requests[memory]: Invalid value: "200Mi": must be less than or equal to the memory limit ` +
						`of 100Mi`))

				defaults := ExtraConfigDefaults
				operatorConfigurationComponents, signalControlComponents = components(
					&dash0v1alpha1.ContainerResourcesWithGoMemLimit{GoMemLimit: "100GiB"})
				Expect(invalidSettings(operatorConfigurationComponents, signalControlComponents)).To(ConsistOf(
					path + `.gomemlimit: Invalid value: "100GiB": must be less than the memory limit of ` +
						effectiveResources(&defaults).Limits.Memory().String()))
			},
			Entry("DaemonSet collector",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return &dash0v1alpha1.OperatorConfigurationComponents{
						DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{CollectorContainerResources: r},
					}, nil
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit {
					return &c.CollectorDaemonSetCollectorContainerResources
				},
				"spec.components.daemonSetCollector.collectorContainerResources"),
			Entry("DaemonSet configuration reloader",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return &dash0v1alpha1.OperatorConfigurationComponents{
						DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{
							ConfigurationReloaderContainerResources: r,
						},
					}, nil
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit {
					return &c.CollectorDaemonSetConfigurationReloaderContainerResources
				},
				"spec.components.daemonSetCollector.configurationReloaderContainerResources"),
			Entry("DaemonSet filelog offset sync",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return &dash0v1alpha1.OperatorConfigurationComponents{
						DaemonSetCollector: &dash0v1alpha1.DaemonSetCollectorSettings{FilelogOffsetSyncContainerResources: r},
					}, nil
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit {
					return &c.CollectorDaemonSetFileLogOffsetSyncContainerResources
				},
				"spec.components.daemonSetCollector.filelogOffsetSyncContainerResources"),
			Entry("deployment collector",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return &dash0v1alpha1.OperatorConfigurationComponents{
						DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{CollectorContainerResources: r},
					}, nil
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit {
					return &c.CollectorDeploymentCollectorContainerResources
				},
				"spec.components.deploymentCollector.collectorContainerResources"),
			Entry("deployment configuration reloader",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return &dash0v1alpha1.OperatorConfigurationComponents{
						DeploymentCollector: &dash0v1alpha1.DeploymentCollectorSettings{
							ConfigurationReloaderContainerResources: r,
						},
					}, nil
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit {
					return &c.CollectorDeploymentConfigurationReloaderContainerResources
				},
				"spec.components.deploymentCollector.configurationReloaderContainerResources"),
			Entry("target-allocator",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return &dash0v1alpha1.OperatorConfigurationComponents{
						TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{ContainerResources: r},
					}, nil
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit { return &c.TargetAllocatorContainerResources },
				"spec.components.targetAllocator.containerResources"),
			Entry("agent0-connector",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return &dash0v1alpha1.OperatorConfigurationComponents{
						Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{ContainerResources: r},
					}, nil
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit { return &c.Agent0ConnectorContainerResources },
				"spec.components.agent0Connector.containerResources"),
			Entry("Signal Control collector",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return nil, &dash0v1alpha1.SignalControlComponents{
						Collector: &dash0v1alpha1.SignalControlCollectorSettings{CollectorContainerResources: r},
					}
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit {
					return &c.SignalControlCollectorContainerResources
				},
				"spec.components.collector.collectorContainerResources"),
			Entry("Signal Control configuration reloader",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return nil, &dash0v1alpha1.SignalControlComponents{
						Collector: &dash0v1alpha1.SignalControlCollectorSettings{
							ConfigurationReloaderContainerResources: r,
						},
					}
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit {
					return &c.SignalControlCollectorConfigurationReloaderContainerResources
				},
				"spec.components.collector.configurationReloaderContainerResources"),
			Entry("Edge Proxy",
				func(r *dash0v1alpha1.ContainerResourcesWithGoMemLimit) (
					*dash0v1alpha1.OperatorConfigurationComponents, *dash0v1alpha1.SignalControlComponents,
				) {
					return nil, &dash0v1alpha1.SignalControlComponents{
						EdgeProxy: &dash0v1alpha1.EdgeProxySettings{ContainerResources: r},
					}
				},
				func(c *ExtraConfig) *ResourceRequirementsWithGoMemLimit { return &c.EdgeProxyContainerResources },
				"spec.components.edgeProxy.containerResources"),
		)

		It("should not report resources that are only written differently", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDaemonSetCollectorContainerResources = ResourceRequirementsWithGoMemLimit{
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1024Mi")},
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
			}
			_, conflicts := resolveOperatorConfiguration(fileConfig, daemonSetCollectorResources(
				&dash0v1alpha1.ContainerResourcesWithGoMemLimit{ContainerResources: memoryLimit("1Gi")}))
			Expect(conflicts).To(BeEmpty())
		})

		It("should apply the resources of the instrumentation init container", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.InstrumentationInitContainerResources = ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
			}
			effective, conflicts := resolveOperatorConfiguration(
				fileConfig,
				&dash0v1alpha1.OperatorConfigurationComponents{
					InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{
						Resources: &dash0v1alpha1.ContainerResources{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m")},
						},
					},
				})
			Expect(toJson(effective.InstrumentationInitContainerResources)).To(MatchJSON(`{"requests":{"cpu":"50m"}}`))
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].Path).To(Equal("spec.components.instrumentationInitContainer.resources"))
			Expect(conflicts[0].HelmValue).To(Equal("operator.initContainerResources"))
		})

		It("should use the GKE Autopilot defaults of the init container as the baseline for conflicts", func() {
			fileConfig := ExtraConfigDefaultsFor(true, false)
			components := &dash0v1alpha1.OperatorConfigurationComponents{
				InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{
					Resources: &dash0v1alpha1.ContainerResources{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
					},
				},
			}

			effective, conflicts := resolve(fileConfig, ExtraConfigDefaultsFor(true, false), components, nil)
			Expect(conflicts).To(BeEmpty())
			Expect(toJson(effective.InstrumentationInitContainerResources)).To(MatchJSON(
				`{"limits":{"ephemeral-storage":"500Mi"},` +
					`"requests":{"cpu":"200m","ephemeral-storage":"500Mi","memory":"128Mi"}}`))

			_, conflicts = resolve(fileConfig, ExtraConfigDefaultsFor(false, false), components, nil)
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].HelmValue).To(Equal("operator.initContainerResources"))
		})

		It("should report resources with a request that is greater than the limit as invalid", func() {
			Expect(invalidSettings(&dash0v1alpha1.OperatorConfigurationComponents{
				TargetAllocator: &dash0v1alpha1.TargetAllocatorSettings{
					ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
						ContainerResources: dash0v1alpha1.ContainerResources{
							Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("300Mi")},
							Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("400Mi")},
						},
					},
				},
			}, nil)).To(ConsistOf(
				`spec.components.targetAllocator.containerResources.requests[memory]: Invalid value: "400Mi": ` +
					`must be less than or equal to the memory limit of 300Mi`,
			))
		})

		It("should not report valid resources as invalid", func() {
			Expect(invalidSettings(
				&dash0v1alpha1.OperatorConfigurationComponents{
					InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{
						Resources: &dash0v1alpha1.ContainerResources{
							Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
							Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1024Mi")},
						},
					},
				},
				&dash0v1alpha1.SignalControlComponents{
					EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
						ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
							ContainerResources: dash0v1alpha1.ContainerResources{
								Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
							},
						},
					},
				},
			)).To(BeEmpty())
		})

		It("should report invalid resources of the Signal Control resource", func() {
			Expect(invalidSettings(nil, &dash0v1alpha1.SignalControlComponents{
				EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
					ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
						ContainerResources: dash0v1alpha1.ContainerResources{
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
						},
					},
				},
			})).To(ConsistOf(
				`spec.components.edgeProxy.containerResources.requests[cpu]: Invalid value: "200m": ` +
					`must be less than or equal to the cpu limit of 100m`,
			))
		})

		It("should report an explicit request above the limit of the same resource as invalid", func() {
			Expect(invalidSettings(&dash0v1alpha1.OperatorConfigurationComponents{
				InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{
					Resources: &dash0v1alpha1.ContainerResources{
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
					},
				},
			}, nil)).To(ConsistOf(
				`spec.components.instrumentationInitContainer.resources.requests[cpu]: Invalid value: "200m": ` +
					`must be less than or equal to the cpu limit of 100m`,
			))
		})
	})

	Describe("GOMEMLIMIT", func() {
		It("should apply a GOMEMLIMIT together with the limits of the setting", func() {
			components := daemonSetCollectorResources(&dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: memoryLimit("1Gi"),
				GoMemLimit:         "800MiB",
			})
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults, components)
			Expect(toJson(effective.CollectorDaemonSetCollectorContainerResources)).To(
				MatchJSON(`{"limits":{"memory":"1Gi"},"requests":{"memory":"500Mi"},"gomemlimit":"800MiB"}`))
			Expect(conflicts).To(BeEmpty())
			Expect(invalidSettings(components, nil)).To(BeEmpty())
		})

		It("should not keep the GOMEMLIMIT of the Helm chart if the setting does not provide one", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.CollectorDaemonSetCollectorContainerResources.GoMemLimit = "400MiB"
			effective, conflicts := resolveOperatorConfiguration(fileConfig, daemonSetCollectorResources(
				&dash0v1alpha1.ContainerResourcesWithGoMemLimit{ContainerResources: memoryLimit("300Mi")}))
			Expect(effective.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(BeEmpty())
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].Path).To(Equal("spec.components.daemonSetCollector.collectorContainerResources"))
		})

		DescribeTable("should check the GOMEMLIMIT against the memory limit the container gets",
			func(settings *dash0v1alpha1.ContainerResourcesWithGoMemLimit, expectedInvalid []string) {
				Expect(invalidSettings(daemonSetCollectorResources(settings), nil)).To(Equal(expectedInvalid))
			},
			Entry("above the default memory limit", &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				GoMemLimit: "600MiB",
			}, []string{`spec.components.daemonSetCollector.collectorContainerResources.gomemlimit: Invalid value: ` +
				`"600MiB": must be less than the memory limit of 500Mi`}),
			Entry("below the default memory limit", &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				GoMemLimit: "400MiB",
			}, nil),
			Entry("below a default memory limit that is raised to the request", &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: dash0v1alpha1.ContainerResources{
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				},
				GoMemLimit: "800MiB",
			}, nil),
			Entry("without a memory limit", &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: memoryLimit("-1"),
				GoMemLimit:         "600MiB",
			}, nil),
		)

		It("should accept off as GOMEMLIMIT", func() {
			Expect(invalidSettings(daemonSetCollectorResources(&dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: memoryLimit("300Mi"),
				GoMemLimit:         "off",
			}), nil)).To(BeEmpty())
		})

		DescribeTable("should report an invalid GOMEMLIMIT",
			func(settings *dash0v1alpha1.ContainerResourcesWithGoMemLimit, expectedReason string) {
				invalid := invalidSettings(daemonSetCollectorResources(settings), nil)
				Expect(invalid).To(HaveLen(1))
				Expect(invalid[0]).To(ContainSubstring(
					"spec.components.daemonSetCollector.collectorContainerResources." + expectedReason))
			},
			Entry("a decimal unit", &dash0v1alpha1.ContainerResourcesWithGoMemLimit{GoMemLimit: "400MB"},
				`gomemlimit: Invalid value: "400MB": must be off or a positive number of bytes`),
			Entry("zero", &dash0v1alpha1.ContainerResourcesWithGoMemLimit{GoMemLimit: "0"},
				`gomemlimit: Invalid value: "0": must be off or a positive number of bytes`),
			Entry("the memory limit", &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: memoryLimit("300Mi"),
				GoMemLimit:         "300MiB",
			}, `gomemlimit: Invalid value: "300MiB": must be less than the memory limit of 300Mi`),
			Entry("above the memory limit", &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
				ContainerResources: memoryLimit("300Mi"),
				GoMemLimit:         "400MiB",
			}, `gomemlimit: Invalid value: "400MiB": must be less than the memory limit of 300Mi`),
		)
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
			Entry("an unsupported resource", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				CollectorContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
					ContainerResources: dash0v1alpha1.ContainerResources{
						Limits: corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")},
					},
				},
			}), `spec.components.daemonSetCollector.collectorContainerResources.limits[nvidia.com/gpu]: `+
				`Unsupported value: "nvidia.com/gpu"`),
			Entry("a negative quantity", daemonSetCollector(dash0v1alpha1.DaemonSetCollectorSettings{
				CollectorContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
					ContainerResources: dash0v1alpha1.ContainerResources{
						Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("-1Gi")},
					},
				},
			}), `spec.components.daemonSetCollector.collectorContainerResources.requests[memory]: Invalid value: "-1Gi"`),
		)

		It("should report the invalid settings of all components of both resources", func() {
			Expect(invalidSettings(
				&dash0v1alpha1.OperatorConfigurationComponents{
					InstrumentationInitContainer: &dash0v1alpha1.InstrumentationInitContainerSettings{
						Resources: &dash0v1alpha1.ContainerResources{
							Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("-64Mi")},
						},
					},
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
				ContainSubstring(`spec.components.instrumentationInitContainer.resources.limits[memory]: `+
					`Invalid value: "-64Mi"`),
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

	Describe("agent0-connector pprof", func() {
		It("should enable pprof for the agent0-connector", func() {
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults,
				&dash0v1alpha1.OperatorConfigurationComponents{
					Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{EnablePprof: new(true)},
				})
			Expect(effective.Agent0ConnectorEnablePprof).To(BeTrue())
			Expect(conflicts).To(BeEmpty())
		})

		It("should disable pprof and report a conflict if the Helm chart enables it", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.Agent0ConnectorEnablePprof = true
			effective, conflicts := resolveOperatorConfiguration(fileConfig,
				&dash0v1alpha1.OperatorConfigurationComponents{
					Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{EnablePprof: new(false)},
				})
			Expect(effective.Agent0ConnectorEnablePprof).To(BeFalse())
			Expect(conflicts).To(ConsistOf(ComponentSettingConflict{
				Resource:        "Dash0OperatorConfiguration",
				Path:            "spec.components.agent0Connector.enablePprof",
				HelmValue:       "operator.agent0Connector.enablePprof",
				HelmSetting:     "true",
				ResourceSetting: "false",
			}))
		})

		It("should keep the Helm value if the setting is not provided", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.Agent0ConnectorEnablePprof = true
			effective, conflicts := resolveOperatorConfiguration(fileConfig,
				&dash0v1alpha1.OperatorConfigurationComponents{
					Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{},
				})
			Expect(effective.Agent0ConnectorEnablePprof).To(BeTrue())
			Expect(conflicts).To(BeEmpty())
		})
	})

	Describe("allowed kubectl commands", func() {
		allowedKubectlCommands := func(
			settings dash0v1alpha1.Agent0ConnectorAllowedKubectlCommands,
		) *dash0v1alpha1.OperatorConfigurationComponents {
			return &dash0v1alpha1.OperatorConfigurationComponents{
				Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{AllowedKubectlCommands: &settings},
			}
		}

		It("should merge the commands with the defaults per command", func() {
			effective, conflicts := resolveOperatorConfiguration(ExtraConfigDefaults, allowedKubectlCommands(
				dash0v1alpha1.Agent0ConnectorAllowedKubectlCommands{Logs: new(true), Top: new(false)}))
			expected := maps.Clone(ExtraConfigDefaults.Agent0ConnectorAllowedKubectlCommands)
			expected["logs"] = true
			expected["top"] = false
			Expect(effective.Agent0ConnectorAllowedKubectlCommands).To(Equal(expected))
			Expect(conflicts).To(BeEmpty())
		})

		It("should use the defaults for an empty object", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.Agent0ConnectorAllowedKubectlCommands = map[string]bool{"get": true}
			effective, conflicts := resolveOperatorConfiguration(
				fileConfig, allowedKubectlCommands(dash0v1alpha1.Agent0ConnectorAllowedKubectlCommands{}))
			Expect(effective.Agent0ConnectorAllowedKubectlCommands).To(
				Equal(ExtraConfigDefaults.Agent0ConnectorAllowedKubectlCommands))
			Expect(conflicts).To(HaveLen(1))
		})

		It("should merge with the defaults, not with the Helm value, and report a conflict", func() {
			fileConfig := ExtraConfigDefaults
			fileConfig.Agent0ConnectorAllowedKubectlCommands = maps.Clone(
				ExtraConfigDefaults.Agent0ConnectorAllowedKubectlCommands)
			fileConfig.Agent0ConnectorAllowedKubectlCommands["top"] = false
			effective, conflicts := resolveOperatorConfiguration(fileConfig, allowedKubectlCommands(
				dash0v1alpha1.Agent0ConnectorAllowedKubectlCommands{Explain: new(false)}))
			Expect(effective.Agent0ConnectorAllowedKubectlCommands).To(HaveKeyWithValue("top", true))
			Expect(effective.Agent0ConnectorAllowedKubectlCommands).To(HaveKeyWithValue("explain", false))
			Expect(conflicts).To(HaveLen(1))
			Expect(conflicts[0].Path).To(Equal("spec.components.agent0Connector.allowedKubectlCommands"))
			Expect(conflicts[0].HelmValue).To(Equal("operator.agent0Connector.allowedKubectlCommands"))
		})

		It("should have a field for every kubectl command of the defaults", func() {
			merged := mergeAllowedKubectlCommands(ExtraConfigDefaults.Agent0ConnectorAllowedKubectlCommands,
				&dash0v1alpha1.Agent0ConnectorAllowedKubectlCommands{
					ApiResources: new(false),
					ApiVersions:  new(false),
					Auth:         new(false),
					ClusterInfo:  new(false),
					Events:       new(true),
					Explain:      new(false),
					Get:          new(false),
					Logs:         new(true),
					Top:          new(false),
					Version:      new(false),
				})
			Expect(merged).To(HaveLen(len(ExtraConfigDefaults.Agent0ConnectorAllowedKubectlCommands)))
			for kubectlCommand, allowed := range ExtraConfigDefaults.Agent0ConnectorAllowedKubectlCommands {
				Expect(merged).To(HaveKeyWithValue(kubectlCommand, !allowed))
			}
		})

		It("should report settings that disable every kubectl command as invalid", func() {
			Expect(invalidSettings(allowedKubectlCommands(dash0v1alpha1.Agent0ConnectorAllowedKubectlCommands{
				ApiResources: new(false),
				ApiVersions:  new(false),
				Auth:         new(false),
				ClusterInfo:  new(false),
				Explain:      new(false),
				Get:          new(false),
				Top:          new(false),
				Version:      new(false),
			}), nil)).To(ConsistOf(
				"spec.components.agent0Connector.allowedKubectlCommands: Forbidden: at least one kubectl command needs " +
					"to be enabled, set spec.agent0Connector.enabled to false instead if Agent0 should not execute any " +
					"kubectl command",
			))
		})

		It("should accept settings that keep one kubectl command enabled", func() {
			Expect(invalidSettings(allowedKubectlCommands(dash0v1alpha1.Agent0ConnectorAllowedKubectlCommands{
				ApiResources: new(false),
				ApiVersions:  new(false),
				Auth:         new(false),
				ClusterInfo:  new(false),
				Explain:      new(false),
				Get:          new(false),
				Top:          new(false),
				Version:      new(false),
				Logs:         new(true),
			}), nil)).To(BeEmpty())
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

		DescribeTable("should not report replacing the OpenShift default SELinux options as a conflict",
			func(setting *corev1.SELinuxOptions, expected *corev1.SELinuxOptions) {
				openShiftDefaults := ExtraConfigDefaultsFor(false, true)
				effective, conflicts := resolve(openShiftDefaults, openShiftDefaults, withSeLinuxOptions(setting), nil)
				Expect(effective.DaemonSetSELinuxOptions).To(Equal(expected))
				Expect(conflicts).To(BeEmpty())
			},
			Entry("other type", &corev1.SELinuxOptions{Type: "resource_t"}, &corev1.SELinuxOptions{Type: "resource_t"}),
			Entry("empty object", &corev1.SELinuxOptions{}, nil),
		)

		DescribeTable("should report replacing SELinux options of the Helm chart on OpenShift as a conflict",
			func(helmSetting *corev1.SELinuxOptions) {
				openShiftDefaults := ExtraConfigDefaultsFor(false, true)
				fileConfig := openShiftDefaults
				fileConfig.DaemonSetSELinuxOptions = helmSetting
				effective, conflicts := resolve(
					fileConfig, openShiftDefaults, withSeLinuxOptions(&corev1.SELinuxOptions{Type: "resource_t"}), nil)
				Expect(effective.DaemonSetSELinuxOptions).To(Equal(&corev1.SELinuxOptions{Type: "resource_t"}))
				Expect(conflicts).To(HaveLen(1))
				Expect(conflicts[0].HelmValue).To(Equal("operator.collectors.daemonSetSeLinuxOptions"))
			},
			Entry("options removed via the Helm chart", nil),
			Entry("other type set via the Helm chart", &corev1.SELinuxOptions{Type: "helm_t"}),
		)
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
					ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
						ContainerResources: dash0v1alpha1.ContainerResources{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
						},
					},
				},
			})
			Expect(effective.SignalControlCollectorReplicas).To(Equal(int32(3)))
			Expect(effective.SignalControlCollectorPodLabels).To(Equal(map[string]string{"team": "observability"}))
			Expect(effective.EdgeProxyReplicas).To(Equal(int32(3)))
			Expect(effective.EdgeProxyEnablePprof).To(BeTrue())
			Expect(toJson(effective.EdgeProxyContainerResources)).To(MatchJSON(
				`{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}`))
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
					Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{MaxConcurrentCommands: new(int32(4))},
				},
				&dash0v1alpha1.SignalControlComponents{
					EdgeProxy: &dash0v1alpha1.EdgeProxySettings{Replicas: new(int32(3))},
				},
			)
			Expect(effective.Agent0ConnectorMaxConcurrentCommands).To(Equal(int32(4)))
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
					CollectorContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
						ContainerResources: dash0v1alpha1.ContainerResources{
							Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("100Mi")},
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
						},
					},
					ConfigurationReloaderContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
						ContainerResources: memoryLimit("-1"),
					},
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
				Agent0Connector: &dash0v1alpha1.Agent0ConnectorSettings{
					AllowedKubectlCommands: &dash0v1alpha1.Agent0ConnectorAllowedKubectlCommands{
						Get:  new(false),
						Logs: new(true),
					},
				},
			},
			&dash0v1alpha1.SignalControlComponents{
				EdgeProxy: &dash0v1alpha1.EdgeProxySettings{
					ContainerResources: &dash0v1alpha1.ContainerResourcesWithGoMemLimit{
						ContainerResources: dash0v1alpha1.ContainerResources{
							Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
						},
					},
				},
			},
		)
		Expect(toJson(ExtraConfigDefaults)).To(MatchJSON(defaultsBefore))
	})
})
