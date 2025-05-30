// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("limits and requests for containers", func() {

	type applyDefaultsTest struct {
		input    *ResourceRequirementsWithGoMemLimit
		defaults *ResourceRequirementsWithGoMemLimit
		expected *ResourceRequirementsWithGoMemLimit
	}

	DescribeTable("should apply defaults resources to requirements", func(testConfig applyDefaultsTest) {
		applyDefaults(testConfig.input, testConfig.defaults)
		if testConfig.expected == nil {
			Expect(testConfig.input).To(BeNil())
		} else {
			Expect(testConfig.input).ToNot(BeNil())
			Expect(*testConfig.input).To(Equal(*testConfig.expected))
		}
	},
		Entry("spec and defaults are nil", applyDefaultsTest{
			input:    nil,
			defaults: nil,
			expected: nil,
		}),
		Entry("no limits&requests, no defaults", applyDefaultsTest{
			input:    &ResourceRequirementsWithGoMemLimit{},
			defaults: &ResourceRequirementsWithGoMemLimit{},
			expected: &ResourceRequirementsWithGoMemLimit{},
		}),
		Entry("no limits&requests, full defaults", applyDefaultsTest{
			input: &ResourceRequirementsWithGoMemLimit{},
			defaults: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
				GoMemLimit: "4MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("5m"),
					corev1.ResourceMemory:           resource.MustParse("6Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("7Mi"),
				},
			},
			expected: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
				GoMemLimit: "4MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("5m"),
					corev1.ResourceMemory:           resource.MustParse("6Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("7Mi"),
				},
			},
		}),
		Entry("no limits&requests, partial defaults", applyDefaultsTest{
			input: &ResourceRequirementsWithGoMemLimit{},
			defaults: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1m"),
				},
				GoMemLimit: "2MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
			},
			expected: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1m"),
				},
				GoMemLimit: "2MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
			},
		}),
		Entry("some limits&requests, nil defaults", applyDefaultsTest{
			input: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1m"),
				},
				GoMemLimit: "4MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Mi"),
				},
			},
			defaults: nil,
			expected: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1m"),
				},
				GoMemLimit: "4MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Mi"),
				},
			},
		}),
		Entry("only limits, no requests, empty defaults", applyDefaultsTest{
			input: &ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Mi"),
				},
			},
			defaults: &ResourceRequirementsWithGoMemLimit{},
			expected: &ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Mi"),
				},
			},
		}),
		Entry("only requests, no limits, empty defaults", applyDefaultsTest{
			input: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Mi"),
				},
			},
			defaults: &ResourceRequirementsWithGoMemLimit{},
			expected: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Mi"),
				},
			},
		}),
		Entry("some limits, some requests, empty defaults", applyDefaultsTest{
			input: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1m"),
				},
				GoMemLimit: "4MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Mi"),
				},
			},
			defaults: &ResourceRequirementsWithGoMemLimit{},
			expected: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1m"),
				},
				GoMemLimit: "4MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Mi"),
				},
			},
		}),
		Entry("only limits, no requests, request defaults", applyDefaultsTest{
			input: &ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Mi"),
				},
			},
			defaults: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2m"),
				},
			},
			expected: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Mi"),
				},
			},
		}),
		Entry("only requests, no limits, limit defaults", applyDefaultsTest{
			input: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Mi"),
				},
			},
			defaults: &ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2m"),
				},
			},
			expected: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2m"),
				},
			},
		}),
		Entry("some limits&requests, full defaults", applyDefaultsTest{
			input: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("10m"),
				},
				GoMemLimit: "14MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("12Mi"),
				},
			},
			defaults: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
				GoMemLimit: "4MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("5m"),
					corev1.ResourceMemory:           resource.MustParse("6Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("7Mi"),
				},
			},
			expected: &ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("10m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
				GoMemLimit: "14MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("5m"),
					corev1.ResourceMemory:           resource.MustParse("12Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("7Mi"),
				},
			},
		}),
	)

	type resourceToRequirementsTest struct {
		input    ResourceRequirementsWithGoMemLimit
		expected corev1.ResourceRequirements
	}

	DescribeTable("should convert resources to requirements", func(testConfig resourceToRequirementsTest) {
		actual := testConfig.input.ToResourceRequirements()
		Expect(actual).To(Equal(testConfig.expected))

	},
		Entry("all attributes set", resourceToRequirementsTest{
			input: ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
				GoMemLimit: "4MiB",
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("5m"),
					corev1.ResourceMemory:           resource.MustParse("6Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("7Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("5m"),
					corev1.ResourceMemory:           resource.MustParse("6Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("7Mi"),
				},
			},
		}),
		Entry("requests and limits with partial attributes", resourceToRequirementsTest{
			input: ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2m"),
				},
			},
			expected: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2m"),
				},
			},
		}),
		Entry("requests but no limits", resourceToRequirementsTest{
			input: ResourceRequirementsWithGoMemLimit{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
				Limits: nil,
			},
		}),
		Entry("limits but no requests", resourceToRequirementsTest{
			input: ResourceRequirementsWithGoMemLimit{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
			},
			expected: corev1.ResourceRequirements{
				Requests: nil,
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("1m"),
					corev1.ResourceMemory:           resource.MustParse("2Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("3Mi"),
				},
			},
		}),
	)

	Describe("with a config map", func() {

		var tmpFile *os.File

		BeforeEach(func() {
			var err error
			tmpFile, err = os.CreateTemp(os.TempDir(), "extra.yaml")
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			if tmpFile != nil {
				Expect(os.Remove(tmpFile.Name())).To(Succeed())
			}
		})

		Describe("parse the config map to extraConfig", func() {
			It("should apply defaults for empty config", func() {
				extraConfig, err := ReadExtraConfiguration(tmpFile.Name())
				Expect(err).ToNot(HaveOccurred())

				Expect(extraConfig.InstrumentationInitContainerResources.Limits).To(BeNil())
				Expect(extraConfig.InstrumentationInitContainerResources.GoMemLimit).To(BeEmpty())
				Expect(extraConfig.InstrumentationInitContainerResources.Requests).To(BeNil())

				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("12Mi"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.GoMemLimit).To(Equal("8MiB"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("12Mi"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Memory().String()).To(Equal("32Mi"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Storage().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.GoMemLimit).To(Equal("24MiB"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Memory().String()).To(Equal("32Mi"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("12Mi"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.GoMemLimit).To(Equal("8MiB"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("12Mi"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.DaemonSetTolerations).To(HaveLen(0))
			})

			It("should parse the config map content with all values set", func() {
				_, err := tmpFile.WriteString(`
  initContainerResources:
    limits:
      cpu: 200m
      memory: 300Mi
      ephemeral-storage: 1Gi
    requests:
      cpu: 100m
      memory: 400Mi
      ephemeral-storage: 2Gi
  collectorDaemonSetCollectorContainerResources:
    limits:
      cpu: 900m
      memory: 600Mi
      ephemeral-storage: 4Gi
    gomemlimit: 400MiB
    requests:
      cpu: 500m
      memory: 400Mi
      ephemeral-storage: 2Gi
  collectorDaemonSetConfigurationReloaderContainerResources:
    limits:
      cpu: 100m
      memory: 14Mi
      ephemeral-storage: 500Mi
    gomemlimit: 9MiB
    requests:
      cpu: 50m
      memory: 13Mi
      ephemeral-storage: 500Mi
  collectorDaemonSetFileLogOffsetSyncContainerResources:
    limits:
      cpu: 100m
      memory: 34Mi
      ephemeral-storage: 500Mi
    gomemlimit: 25MiB
    requests:
      cpu: 50m
      memory: 33Mi
      ephemeral-storage: 500Mi
  collectorDeploymentCollectorContainerResources:
    limits:
      cpu: 100m
      memory: 600Mi
      ephemeral-storage: 4Gi
    gomemlimit: 450MiB
    requests:
      cpu: 50m
      memory: 400Mi
      ephemeral-storage: 2Gi
  collectorDeploymentConfigurationReloaderContainerResources:
    limits:
      cpu: 100m
      memory: 14Mi
      ephemeral-storage: 500Mi
    gomemlimit: 9MiB
    requests:
      cpu: 50m
      memory: 13Mi
      ephemeral-storage: 500Mi
  daemonSetTolerations:
    - key: key1
      operator: Equal
      value: value1
      effect: NoSchedule
    - key: key2
      operator: Exists
      effect: NoSchedule
`)
				Expect(err).ToNot(HaveOccurred())

				extraConfig, err := ReadExtraConfiguration(tmpFile.Name())
				Expect(err).ToNot(HaveOccurred())

				Expect(extraConfig.InstrumentationInitContainerResources.Limits.Cpu().String()).To(Equal("200m"))
				Expect(extraConfig.InstrumentationInitContainerResources.Limits.Memory().String()).To(Equal("300Mi"))
				Expect(extraConfig.InstrumentationInitContainerResources.Limits.StorageEphemeral().String()).To(Equal("1Gi"))
				Expect(extraConfig.InstrumentationInitContainerResources.GoMemLimit).To(BeEmpty())
				Expect(extraConfig.InstrumentationInitContainerResources.Requests.Cpu().String()).To(Equal("100m"))
				Expect(extraConfig.InstrumentationInitContainerResources.Requests.Memory().String()).To(Equal("400Mi"))
				Expect(extraConfig.InstrumentationInitContainerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Cpu().String()).To(Equal("900m"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Memory().String()).To(Equal("600Mi"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.StorageEphemeral().String()).To(Equal("4Gi"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Cpu().String()).To(Equal("500m"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Memory().String()).To(Equal("400Mi"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Cpu().String()).To(Equal("100m"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("14Mi"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.GoMemLimit).To(Equal("9MiB"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Cpu().String()).To(Equal("50m"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("13Mi"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))

				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Cpu().String()).To(Equal("100m"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Memory().String()).To(Equal("34Mi"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.GoMemLimit).To(Equal("25MiB"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Cpu().String()).To(Equal("50m"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Memory().String()).To(Equal("33Mi"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))

				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Cpu().String()).To(Equal("100m"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Memory().String()).To(Equal("600Mi"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.StorageEphemeral().String()).To(Equal("4Gi"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.GoMemLimit).To(Equal("450MiB"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Cpu().String()).To(Equal("50m"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Memory().String()).To(Equal("400Mi"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Cpu().String()).To(Equal("100m"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("14Mi"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.GoMemLimit).To(Equal("9MiB"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Cpu().String()).To(Equal("50m"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("13Mi"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))

				Expect(extraConfig.DaemonSetTolerations).To(HaveLen(2))
				Expect(extraConfig.DaemonSetTolerations[0].Key).To(Equal("key1"))
				Expect(extraConfig.DaemonSetTolerations[0].Operator).To(Equal(corev1.TolerationOpEqual))
				Expect(extraConfig.DaemonSetTolerations[0].Value).To(Equal("value1"))
				Expect(extraConfig.DaemonSetTolerations[0].Effect).To(Equal(corev1.TaintEffectNoSchedule))
				Expect(extraConfig.DaemonSetTolerations[0].TolerationSeconds).To(BeNil())
				Expect(extraConfig.DaemonSetTolerations[1].Key).To(Equal("key2"))
				Expect(extraConfig.DaemonSetTolerations[1].Operator).To(Equal(corev1.TolerationOpExists))
				Expect(extraConfig.DaemonSetTolerations[1].Value).To(BeEmpty())
				Expect(extraConfig.DaemonSetTolerations[1].Effect).To(Equal(corev1.TaintEffectNoSchedule))
				Expect(extraConfig.DaemonSetTolerations[1].TolerationSeconds).To(BeNil())
			})

			It("should merge partial config and defaults", func() {
				_, err := tmpFile.WriteString(`
  initContainerResources:
    limits:
      cpu: 200m
  collectorDaemonSetCollectorContainerResources:
    limits:
      cpu: 900m
    requests:
      cpu: 500m
  collectorDaemonSetConfigurationReloaderContainerResources:
    limits:
      memory: 14Mi
    gomemlimit: 9MiB
    requests:
      memory: 13Mi
`)
				Expect(err).ToNot(HaveOccurred())

				extraConfig, err := ReadExtraConfiguration(tmpFile.Name())
				Expect(err).ToNot(HaveOccurred())

				Expect(extraConfig.InstrumentationInitContainerResources.Limits.Cpu().String()).To(Equal("200m"))
				Expect(extraConfig.InstrumentationInitContainerResources.Limits.Memory().IsZero()).To(BeTrue())
				Expect(extraConfig.InstrumentationInitContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.InstrumentationInitContainerResources.GoMemLimit).To(BeEmpty())
				Expect(extraConfig.InstrumentationInitContainerResources.Requests).To(BeNil())

				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Cpu().String()).To(Equal("900m"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Cpu().String()).To(Equal("500m"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("14Mi"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.GoMemLimit).To(Equal("9MiB"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("13Mi"))
				Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Memory().String()).To(Equal("32Mi"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.GoMemLimit).To(Equal("24MiB"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Memory().String()).To(Equal("32Mi"))
				Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
				Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("12Mi"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.GoMemLimit).To(Equal("8MiB"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("12Mi"))
				Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				Expect(extraConfig.DaemonSetTolerations).To(HaveLen(0))
			})
		})

		Describe("parse config map and convert to resource requirements", func() {
			It("should convert defaults to resource requirements", func() {
				extraConfig, err := ReadExtraConfiguration(tmpFile.Name())
				Expect(err).ToNot(HaveOccurred())

				containerResources := extraConfig.InstrumentationInitContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits).To(BeNil())
				Expect(containerResources.Requests).To(BeNil())

				containerResources = extraConfig.CollectorDaemonSetCollectorContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.Memory().String()).To(Equal("500Mi"))
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Memory().String()).To(Equal("500Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				containerResources = extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.Memory().String()).To(Equal("12Mi"))
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Memory().String()).To(Equal("12Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				containerResources = extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.Memory().String()).To(Equal("32Mi"))
				Expect(containerResources.Limits.Storage().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Memory().String()).To(Equal("32Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				containerResources = extraConfig.CollectorDeploymentCollectorContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.Memory().String()).To(Equal("500Mi"))
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Memory().String()).To(Equal("500Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				containerResources = extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.Memory().String()).To(Equal("12Mi"))
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Memory().String()).To(Equal("12Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())
			})

			It("should parse the config map content with all values set", func() {
				_, err := tmpFile.WriteString(`
  initContainerResources:
    limits:
      cpu: 200m
      memory: 300Mi
      ephemeral-storage: 1Gi
    requests:
      cpu: 100m
      memory: 400Mi
      ephemeral-storage: 2Gi
  collectorDaemonSetCollectorContainerResources:
    limits:
      cpu: 900m
      memory: 600Mi
      ephemeral-storage: 4Gi
    gomemlimit: 400MiB
    requests:
      cpu: 500m
      memory: 400Mi
      ephemeral-storage: 2Gi
  collectorDaemonSetConfigurationReloaderContainerResources:
    limits:
      cpu: 100m
      memory: 14Mi
      ephemeral-storage: 500Mi
    gomemlimit: 9MiB
    requests:
      cpu: 50m
      memory: 13Mi
      ephemeral-storage: 500Mi
  collectorDaemonSetFileLogOffsetSyncContainerResources:
    limits:
      cpu: 100m
      memory: 34Mi
      ephemeral-storage: 500Mi
    gomemlimit: 25MiB
    requests:
      cpu: 50m
      memory: 33Mi
      ephemeral-storage: 500Mi
  collectorDeploymentCollectorContainerResources:
    limits:
      cpu: 100m
      memory: 600Mi
      ephemeral-storage: 4Gi
    gomemlimit: 450MiB
    requests:
      cpu: 50m
      memory: 400Mi
      ephemeral-storage: 2Gi
  collectorDeploymentConfigurationReloaderContainerResources:
    limits:
      cpu: 100m
      memory: 14Mi
      ephemeral-storage: 500Mi
    gomemlimit: 9MiB
    requests:
      cpu: 50m
      memory: 13Mi
      ephemeral-storage: 500Mi
  daemonSetTolerations:
    - key: key1
      operator: Equal
      value: value1
      effect: NoSchedule
    - key: key2
      operator: Exists
      effect: NoSchedule
`)
				Expect(err).ToNot(HaveOccurred())

				extraConfig, err := ReadExtraConfiguration(tmpFile.Name())
				Expect(err).ToNot(HaveOccurred())

				containerResources := extraConfig.InstrumentationInitContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().String()).To(Equal("200m"))
				Expect(containerResources.Limits.Memory().String()).To(Equal("300Mi"))
				Expect(containerResources.Limits.StorageEphemeral().String()).To(Equal("1Gi"))
				Expect(containerResources.Requests.Cpu().String()).To(Equal("100m"))
				Expect(containerResources.Requests.Memory().String()).To(Equal("400Mi"))
				Expect(containerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

				containerResources = extraConfig.CollectorDaemonSetCollectorContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().String()).To(Equal("900m"))
				Expect(containerResources.Limits.Memory().String()).To(Equal("600Mi"))
				Expect(containerResources.Limits.StorageEphemeral().String()).To(Equal("4Gi"))
				Expect(containerResources.Requests.Cpu().String()).To(Equal("500m"))
				Expect(containerResources.Requests.Memory().String()).To(Equal("400Mi"))
				Expect(containerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

				containerResources = extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().String()).To(Equal("100m"))
				Expect(containerResources.Limits.Memory().String()).To(Equal("14Mi"))
				Expect(containerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
				Expect(containerResources.Requests.Cpu().String()).To(Equal("50m"))
				Expect(containerResources.Requests.Memory().String()).To(Equal("13Mi"))
				Expect(containerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))

				containerResources = extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().String()).To(Equal("100m"))
				Expect(containerResources.Limits.Memory().String()).To(Equal("34Mi"))
				Expect(containerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
				Expect(containerResources.Requests.Cpu().String()).To(Equal("50m"))
				Expect(containerResources.Requests.Memory().String()).To(Equal("33Mi"))
				Expect(containerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))

				containerResources = extraConfig.CollectorDeploymentCollectorContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().String()).To(Equal("100m"))
				Expect(containerResources.Limits.Memory().String()).To(Equal("600Mi"))
				Expect(containerResources.Limits.StorageEphemeral().String()).To(Equal("4Gi"))
				Expect(containerResources.Requests.Cpu().String()).To(Equal("50m"))
				Expect(containerResources.Requests.Memory().String()).To(Equal("400Mi"))
				Expect(containerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

				containerResources = extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().String()).To(Equal("100m"))
				Expect(containerResources.Limits.Memory().String()).To(Equal("14Mi"))
				Expect(containerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
				Expect(containerResources.Requests.Cpu().String()).To(Equal("50m"))
				Expect(containerResources.Requests.Memory().String()).To(Equal("13Mi"))
				Expect(containerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))
			})

			It("should merge partial config and defaults", func() {
				_, err := tmpFile.WriteString(`
  initContainerResources:
    limits:
      cpu: 200m
  collectorDaemonSetCollectorContainerResources:
    limits:
      cpu: 900m
    requests:
      cpu: 500m
  collectorDaemonSetConfigurationReloaderContainerResources:
    limits:
      memory: 14Mi
    gomemlimit: 9MiB
    requests:
      memory: 13Mi
`)
				Expect(err).ToNot(HaveOccurred())

				extraConfig, err := ReadExtraConfiguration(tmpFile.Name())
				Expect(err).ToNot(HaveOccurred())

				containerResources := extraConfig.InstrumentationInitContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().String()).To(Equal("200m"))
				Expect(containerResources.Limits.Memory().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests).To(BeNil())

				containerResources = extraConfig.CollectorDaemonSetCollectorContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().String()).To(Equal("900m"))
				Expect(containerResources.Limits.Memory().String()).To(Equal("500Mi"))
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().String()).To(Equal("500m"))
				Expect(containerResources.Requests.Memory().String()).To(Equal("500Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				containerResources = extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.Memory().String()).To(Equal("14Mi"))
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Memory().String()).To(Equal("13Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				containerResources = extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.Memory().String()).To(Equal("32Mi"))
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Memory().String()).To(Equal("32Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				containerResources = extraConfig.CollectorDeploymentCollectorContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.Memory().String()).To(Equal("500Mi"))
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Memory().String()).To(Equal("500Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

				containerResources = extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.ToResourceRequirements()
				Expect(containerResources.Limits.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Limits.Memory().String()).To(Equal("12Mi"))
				Expect(containerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Cpu().IsZero()).To(BeTrue())
				Expect(containerResources.Requests.Memory().String()).To(Equal("12Mi"))
				Expect(containerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())
			})
		})
	})
})
