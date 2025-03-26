// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"os"

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("limits and requests for the otelcol resources", func() {

	var tmpFile *os.File

	BeforeEach(func() {
		var err error
		tmpFile, err = os.CreateTemp(os.TempDir(), "otelcolextra.yaml")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if tmpFile != nil {
			Expect(os.Remove(tmpFile.Name())).To(Succeed())
		}
	})

	It("should parse the config map content", func() {
		_, err := tmpFile.WriteString(`
  collectorDaemonSetCollectorContainerResources:
    limits:
      cpu: 900m
      memory: 600Mi
      storage: 2Gi
      ephemeral-storage: 4Gi
    gomemlimit: 400MiB
    requests:
      cpu: 500m
      memory: 400Mi
      storage: 1Gi
      ephemeral-storage: 2Gi
  collectorDaemonSetConfigurationReloaderContainerResources:
    limits:
      cpu: 100m
      memory: 14Mi
      storage: 500Mi
      ephemeral-storage: 500Mi
    gomemlimit: 9MiB
    requests:
      cpu: 50m
      memory: 13Mi
      storage: 500Mi
      ephemeral-storage: 500Mi
  collectorDaemonSetFileLogOffsetSyncContainerResources:
    limits:
      cpu: 100m
      memory: 34Mi
      storage: 500Mi
      ephemeral-storage: 500Mi
    gomemlimit: 25MiB
    requests:
      cpu: 50m
      memory: 33Mi
      storage: 500Mi
      ephemeral-storage: 500Mi
  collectorDeploymentCollectorContainerResources:
    limits:
      cpu: 100m
      memory: 600Mi
      storage: 2Gi
      ephemeral-storage: 4Gi
    gomemlimit: 450MiB
    requests:
      cpu: 50m
      memory: 400Mi
      storage: 1Gi
      ephemeral-storage: 2Gi
  collectorDeploymentConfigurationReloaderContainerResources:
    limits:
      cpu: 100m
      memory: 14Mi
      storage: 500Mi
      ephemeral-storage: 500Mi
    gomemlimit: 9MiB
    requests:
      cpu: 50m
      memory: 13Mi
      storage: 500Mi
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

		extraConfig, err := ReadOTelColExtraConfiguration(tmpFile.Name())
		Expect(err).ToNot(HaveOccurred())

		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Cpu().String()).To(Equal("900m"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Memory().String()).To(Equal("600Mi"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Storage().String()).To(Equal("2Gi"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.StorageEphemeral().String()).To(Equal("4Gi"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Cpu().String()).To(Equal("500m"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Memory().String()).To(Equal("400Mi"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Storage().String()).To(Equal("1Gi"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Cpu().String()).To(Equal("100m"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("14Mi"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Storage().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.GoMemLimit).To(Equal("9MiB"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Cpu().String()).To(Equal("50m"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("13Mi"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Storage().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))

		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Cpu().String()).To(Equal("100m"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Memory().String()).To(Equal("34Mi"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Storage().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.GoMemLimit).To(Equal("25MiB"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Cpu().String()).To(Equal("50m"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Memory().String()).To(Equal("33Mi"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Storage().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))

		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Cpu().String()).To(Equal("100m"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Memory().String()).To(Equal("600Mi"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Storage().String()).To(Equal("2Gi"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.StorageEphemeral().String()).To(Equal("4Gi"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.GoMemLimit).To(Equal("450MiB"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Cpu().String()).To(Equal("50m"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Memory().String()).To(Equal("400Mi"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Storage().String()).To(Equal("1Gi"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Cpu().String()).To(Equal("100m"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("14Mi"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Storage().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.GoMemLimit).To(Equal("9MiB"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Cpu().String()).To(Equal("50m"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("13Mi"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Storage().String()).To(Equal("500Mi"))
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

	It("should apply defaults for empty config", func() {
		extraConfig, err := ReadOTelColExtraConfiguration(tmpFile.Name())
		Expect(err).ToNot(HaveOccurred())

		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("12Mi"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.GoMemLimit).To(Equal("8MiB"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("12Mi"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Memory().String()).To(Equal("32Mi"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.GoMemLimit).To(Equal("24MiB"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Memory().String()).To(Equal("32Mi"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("12Mi"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.GoMemLimit).To(Equal("8MiB"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("12Mi"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.DaemonSetTolerations).To(HaveLen(0))
	})

	It("should merge partial config and defaults", func() {
		_, err := tmpFile.WriteString(`
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

		extraConfig, err := ReadOTelColExtraConfiguration(tmpFile.Name())
		Expect(err).ToNot(HaveOccurred())

		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Cpu().String()).To(Equal("900m"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Cpu().String()).To(Equal("500m"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("14Mi"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.GoMemLimit).To(Equal("9MiB"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("13Mi"))
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Memory().String()).To(Equal("32Mi"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.GoMemLimit).To(Equal("24MiB"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Memory().String()).To(Equal("32Mi"))
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("12Mi"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.GoMemLimit).To(Equal("8MiB"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("12Mi"))
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(extraConfig.CollectorDeploymentConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(extraConfig.DaemonSetTolerations).To(HaveLen(0))
	})
})
