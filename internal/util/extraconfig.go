// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

type ResourceRequirementsWithGoMemLimit struct {
	Limits     corev1.ResourceList `json:"limits,omitempty"`
	Requests   corev1.ResourceList `json:"requests,omitempty"`
	GoMemLimit string              `json:"gomemlimit,omitempty"`
}

type ExtraConfig struct {
	InstrumentationInitContainerResources ResourceRequirementsWithGoMemLimit `json:"initContainerResources"`

	CollectorDaemonSetCollectorContainerResources             ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetCollectorContainerResources,omitempty"`
	CollectorDaemonSetConfigurationReloaderContainerResources ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetConfigurationReloaderContainerResources,omitempty"`
	CollectorDaemonSetFileLogOffsetSyncContainerResources     ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetFileLogOffsetSyncContainerResources,omitempty"`
	CollectorFilelogOffsetStorageVolume                       *corev1.Volume                     `json:"collectorFilelogOffsetStorageVolume,omitempty"`

	CollectorDeploymentCollectorContainerResources             ResourceRequirementsWithGoMemLimit `json:"collectorDeploymentCollectorContainerResources,omitempty"`
	CollectorDeploymentConfigurationReloaderContainerResources ResourceRequirementsWithGoMemLimit `json:"collectorDeploymentConfigurationReloaderContainerResources,omitempty"`

	DaemonSetTolerations []corev1.Toleration `json:"daemonSetTolerations,omitempty"`
}

var (
	ExtraConfigDefaults = ExtraConfig{
		InstrumentationInitContainerResources: ResourceRequirementsWithGoMemLimit{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("150Mi"),
			},
		},
		CollectorDaemonSetCollectorContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
			GoMemLimit: "400MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		CollectorDaemonSetConfigurationReloaderContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
			GoMemLimit: "8MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
		CollectorDaemonSetFileLogOffsetSyncContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			GoMemLimit: "24MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
		},
		CollectorDeploymentCollectorContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
			GoMemLimit: "400MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		CollectorDeploymentConfigurationReloaderContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
			GoMemLimit: "8MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
	}
)

func ReadExtraConfiguration(configurationFile string) (ExtraConfig, error) {
	if len(configurationFile) == 0 {
		return ExtraConfig{}, fmt.Errorf("filename is empty")
	}
	content, err := os.ReadFile(configurationFile)
	if err != nil {
		return ExtraConfig{}, fmt.Errorf("the configuration file (%s) is missing or cannot be opened %w", configurationFile, err)
	}

	extraConfig := &ExtraConfig{}
	if err = yaml.Unmarshal(content, extraConfig); err != nil {
		return ExtraConfig{}, fmt.Errorf("cannot unmarshal the configuration file %w", err)
	}
	applyDefaults(
		&extraConfig.InstrumentationInitContainerResources,
		&ExtraConfigDefaults.InstrumentationInitContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDaemonSetCollectorContainerResources,
		&ExtraConfigDefaults.CollectorDaemonSetCollectorContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources,
		&ExtraConfigDefaults.CollectorDaemonSetConfigurationReloaderContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources,
		&ExtraConfigDefaults.CollectorDaemonSetFileLogOffsetSyncContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDeploymentCollectorContainerResources,
		&ExtraConfigDefaults.CollectorDeploymentCollectorContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDeploymentConfigurationReloaderContainerResources,
		&ExtraConfigDefaults.CollectorDeploymentConfigurationReloaderContainerResources,
	)
	return *extraConfig, nil
}

// applyDefaults sets default values for CPU, Memory, and Ephemeral Storage on requests and limits.
func applyDefaults(spec *ResourceRequirementsWithGoMemLimit, defaults *ResourceRequirementsWithGoMemLimit) {
	if spec == nil || defaults == nil {
		// if spec is a nil pointer, we have no target object to update
		// if defaults is a nil pointer, we have no source object to copy values from
		return
	}
	if defaults.Requests != nil {
		if spec.Requests == nil {
			spec.Requests = make(corev1.ResourceList)
		}
		applyDefaultValues(spec.Requests, defaults.Requests)
	}
	if defaults.Limits != nil {
		if spec.Limits == nil {
			spec.Limits = make(corev1.ResourceList)
		}
		applyDefaultValues(spec.Limits, defaults.Limits)

	}
	if spec.GoMemLimit == "" && defaults.GoMemLimit != "" {
		spec.GoMemLimit = defaults.GoMemLimit
	}
}

// applyDefaultValues sets default values for CPU, Memory, and Ephemeral Storage on either requests or limits, expects
// both resourceList and defaults to not be nil.
func applyDefaultValues(resourceList corev1.ResourceList, defaults corev1.ResourceList) {
	if resourceList.Cpu().IsZero() && !defaults.Cpu().IsZero() {
		resourceList[corev1.ResourceCPU] = *defaults.Cpu()
	}
	if resourceList.Memory().IsZero() && !defaults.Memory().IsZero() {
		resourceList[corev1.ResourceMemory] = *defaults.Memory()
	}
	if resourceList.StorageEphemeral().IsZero() && !defaults.StorageEphemeral().IsZero() {
		resourceList[corev1.ResourceEphemeralStorage] = *defaults.StorageEphemeral()
	}

}

func (rr ResourceRequirementsWithGoMemLimit) ToResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits:   rr.Limits,
		Requests: rr.Requests,
	}
}
