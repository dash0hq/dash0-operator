// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("limits and requests for the otelcol resources", func() {

	var tmpFile *os.File

	BeforeEach(func() {
		var err error
		tmpFile, err = os.CreateTemp(os.TempDir(), "limits_requests_config.yaml")
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
  collectorDaemonSetFileLogOffsetSynchContainerResources:
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
`)
		Expect(err).ToNot(HaveOccurred())

		resourceSpec, err := ReadOTelColResourcesConfiguration(tmpFile.Name())
		Expect(err).ToNot(HaveOccurred())

		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.Cpu().String()).To(Equal("900m"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.Memory().String()).To(Equal("600Mi"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.Storage().String()).To(Equal("2Gi"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.StorageEphemeral().String()).To(Equal("4Gi"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.Cpu().String()).To(Equal("500m"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.Memory().String()).To(Equal("400Mi"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.Storage().String()).To(Equal("1Gi"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Cpu().String()).To(Equal("100m"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("14Mi"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Storage().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.GoMemLimit).To(Equal("9MiB"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Cpu().String()).To(Equal("50m"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("13Mi"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Storage().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))

		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.Cpu().String()).To(Equal("100m"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.Memory().String()).To(Equal("34Mi"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.Storage().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.GoMemLimit).To(Equal("25MiB"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.Cpu().String()).To(Equal("50m"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.Memory().String()).To(Equal("33Mi"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.Storage().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))

		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.Cpu().String()).To(Equal("100m"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.Memory().String()).To(Equal("600Mi"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.Storage().String()).To(Equal("2Gi"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.StorageEphemeral().String()).To(Equal("4Gi"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.GoMemLimit).To(Equal("450MiB"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.Cpu().String()).To(Equal("50m"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.Memory().String()).To(Equal("400Mi"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.Storage().String()).To(Equal("1Gi"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.StorageEphemeral().String()).To(Equal("2Gi"))

		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Cpu().String()).To(Equal("100m"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("14Mi"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Storage().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.StorageEphemeral().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.GoMemLimit).To(Equal("9MiB"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Cpu().String()).To(Equal("50m"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("13Mi"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Storage().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.StorageEphemeral().String()).To(Equal("500Mi"))
	})

	It("should apply defaults for empty config", func() {
		resourceSpec, err := ReadOTelColResourcesConfiguration(tmpFile.Name())
		Expect(err).ToNot(HaveOccurred())

		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("12Mi"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.GoMemLimit).To(Equal("8MiB"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("12Mi"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.Memory().String()).To(Equal("32Mi"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.GoMemLimit).To(Equal("24MiB"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.Memory().String()).To(Equal("32Mi"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("12Mi"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.GoMemLimit).To(Equal("8MiB"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("12Mi"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())
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

		resourceSpec, err := ReadOTelColResourcesConfiguration(tmpFile.Name())
		Expect(err).ToNot(HaveOccurred())

		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.Cpu().String()).To(Equal("900m"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.Cpu().String()).To(Equal("500m"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("14Mi"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.GoMemLimit).To(Equal("9MiB"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("13Mi"))
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.Memory().String()).To(Equal("32Mi"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.GoMemLimit).To(Equal("24MiB"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.Memory().String()).To(Equal("32Mi"))
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDaemonSetFileLogOffsetSynchContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.GoMemLimit).To(Equal("400MiB"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentCollectorContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())

		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Memory().String()).To(Equal("12Mi"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Limits.StorageEphemeral().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.GoMemLimit).To(Equal("8MiB"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Cpu().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Memory().String()).To(Equal("12Mi"))
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.Storage().IsZero()).To(BeTrue())
		Expect(resourceSpec.CollectorDeploymentConfigurationReloaderContainerResources.Requests.StorageEphemeral().IsZero()).To(BeTrue())
	})
})
