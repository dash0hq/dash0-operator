// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

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

type OTelColResourceSpecs struct {
	CollectorDaemonSetCollectorContainerResources             ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetCollectorContainerResources,omitempty"`
	CollectorDaemonSetConfigurationReloaderContainerResources ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetConfigurationReloaderContainerResources,omitempty"`
	CollectorDaemonSetFileLogOffsetSynchContainerResources    ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetFileLogOffsetSynchContainerResources,omitempty"`

	CollectorDeploymentCollectorContainerResources             ResourceRequirementsWithGoMemLimit `json:"collectorDeploymentCollectorContainerResources,omitempty"`
	CollectorDeploymentConfigurationReloaderContainerResources ResourceRequirementsWithGoMemLimit `json:"collectorDeploymentConfigurationReloaderContainerResources,omitempty"`
}

var (
	DefaultOTelColResourceSpecs = OTelColResourceSpecs{
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
		CollectorDaemonSetFileLogOffsetSynchContainerResources: ResourceRequirementsWithGoMemLimit{
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

func ReadOTelColResourcesConfiguration(configurationFile string) (*OTelColResourceSpecs, error) {
	if len(configurationFile) == 0 {
		return nil, fmt.Errorf("filename is empty")
	}
	content, err := os.ReadFile(configurationFile)
	if err != nil {
		return nil, fmt.Errorf("the configuration file (%s) is missing or cannot be opened %w", configurationFile, err)
	}

	resourcesSpecs := &OTelColResourceSpecs{}
	if err = yaml.Unmarshal(content, resourcesSpecs); err != nil {
		return nil, fmt.Errorf("cannot unmarshal the configuration file %w", err)
	}
	applyDefaults(
		&resourcesSpecs.CollectorDaemonSetCollectorContainerResources,
		&DefaultOTelColResourceSpecs.CollectorDaemonSetCollectorContainerResources,
	)
	applyDefaults(
		&resourcesSpecs.CollectorDaemonSetConfigurationReloaderContainerResources,
		&DefaultOTelColResourceSpecs.CollectorDaemonSetConfigurationReloaderContainerResources,
	)
	applyDefaults(
		&resourcesSpecs.CollectorDaemonSetFileLogOffsetSynchContainerResources,
		&DefaultOTelColResourceSpecs.CollectorDaemonSetFileLogOffsetSynchContainerResources,
	)
	applyDefaults(
		&resourcesSpecs.CollectorDeploymentCollectorContainerResources,
		&DefaultOTelColResourceSpecs.CollectorDeploymentCollectorContainerResources,
	)
	applyDefaults(
		&resourcesSpecs.CollectorDeploymentConfigurationReloaderContainerResources,
		&DefaultOTelColResourceSpecs.CollectorDeploymentConfigurationReloaderContainerResources,
	)

	return resourcesSpecs, nil
}

func applyDefaults(spec *ResourceRequirementsWithGoMemLimit, defaults *ResourceRequirementsWithGoMemLimit) {
	if spec.Limits == nil {
		spec.Limits = make(corev1.ResourceList)
	}
	if spec.Limits.Memory().IsZero() {
		spec.Limits[corev1.ResourceMemory] =
			*defaults.Limits.Memory()
	}
	if spec.GoMemLimit == "" {
		spec.GoMemLimit =
			defaults.GoMemLimit
	}
	if spec.Requests == nil {
		spec.Requests = make(corev1.ResourceList)
	}
	if spec.Requests.Memory().IsZero() {
		spec.Requests[corev1.ResourceMemory] =
			*defaults.Requests.Memory()
	}
}

func (rr ResourceRequirementsWithGoMemLimit) ToResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits:   rr.Limits,
		Requests: rr.Requests,
	}
}
