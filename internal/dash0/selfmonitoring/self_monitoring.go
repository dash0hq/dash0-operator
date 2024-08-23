// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoring

import (
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"regexp"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	// TODO Use the const from the Go SDK when we implement self-monitoring
	otelExporterOtlpEndpointEnvVarName = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterOtlpHeadersEnvVarName  = "OTEL_EXPORTER_OTLP_HEADERS"
)

var (
	// Match in a case-insensitive fashion the structure of an HTTP Authorization header with Bearer schema,
	// and declare the named capture group `<bearerToken>` to extract the token's value.
	authorizationHeaderRegexp = regexp.MustCompile(`(?i:Authorization=Bearer (?P<bearerToken>\S+))`)
)

type SelfMonitoringConfiguration struct {
	Enabled     bool
	Endpoint    string
	BearerToken string
}

type cannotFindContainerByNameError struct {
	ContainerName     string
	WorkloadGKV       schema.GroupVersionKind
	WorkloadNamespace string
	WorkloadName      string
}

func (c *cannotFindContainerByNameError) Error() string {
	return fmt.Sprintf("cannot find the container named '%v' in the %v %v/%v", c.ContainerName, c.WorkloadGKV.Kind, c.WorkloadNamespace, c.WorkloadName)
}

func IsSelfMonitoringInCollectorDaemonSetEnabled(collectorDemonSet *appsv1.DaemonSet) (SelfMonitoringConfiguration, error) {
	selfMonitoringConfigurations := make(map[string]SelfMonitoringConfiguration)

	// Check that we have the OTEL env vars on all known containers, init and not
	for _, container := range collectorDemonSet.Spec.Template.Spec.InitContainers {
		if selfMonitoringConfiguration, err := isSelfMonitoringEnabledInContainer(&container); err != nil {
			return SelfMonitoringConfiguration{
				Enabled: false,
			}, err
		} else {
			selfMonitoringConfigurations[container.Name] = selfMonitoringConfiguration
		}
	}

	for _, container := range collectorDemonSet.Spec.Template.Spec.Containers {
		if selfMonitoringConfiguration, err := isSelfMonitoringEnabledInContainer(&container); err != nil {
			return SelfMonitoringConfiguration{
				Enabled: false,
			}, err
		} else {
			selfMonitoringConfigurations[container.Name] = selfMonitoringConfiguration
		}
	}

	var referenceMonitoringConfiguration *SelfMonitoringConfiguration
	// Check they configs are consistent
	for _, selfMonitoringConfiguration := range selfMonitoringConfigurations {
		if referenceMonitoringConfiguration == nil {
			referenceMonitoringConfiguration = &selfMonitoringConfiguration
		} else {
			if *referenceMonitoringConfiguration != selfMonitoringConfiguration {
				return SelfMonitoringConfiguration{}, fmt.Errorf("inconsistent self-monitoring configurations: %v", selfMonitoringConfigurations)
			}
		}
	}

	if referenceMonitoringConfiguration != nil {
		return *referenceMonitoringConfiguration, nil
	} else {
		return SelfMonitoringConfiguration{}, nil
	}
}

func DisableSelfMonitoringInCollectorDaemonSet(collectorDemonSet *appsv1.DaemonSet) error {
	for _, container := range collectorDemonSet.Spec.Template.Spec.InitContainers {
		disableSelfMonitoringInContainer(&container)
	}

	for _, container := range collectorDemonSet.Spec.Template.Spec.Containers {
		disableSelfMonitoringInContainer(&container)
	}

	// Error is in the signature to make it future-compatible
	return nil
}

func EnableSelfMonitoringInCollectorDaemonSet(collectorDemonSet *appsv1.DaemonSet, ingressEndpoint string, bearerToken string) error {
	for i, container := range collectorDemonSet.Spec.Template.Spec.InitContainers {
		enableSelfMonitoringInContainer(&container, ingressEndpoint, bearerToken)
		collectorDemonSet.Spec.Template.Spec.InitContainers[i] = container
	}

	for i, container := range collectorDemonSet.Spec.Template.Spec.Containers {
		enableSelfMonitoringInContainer(&container, ingressEndpoint, bearerToken)
		collectorDemonSet.Spec.Template.Spec.Containers[i] = container
	}

	// Error is in the signature to make it future-compatible
	return nil
}

func GetSelfMonitoringConfigurationFromControllerDeployment(controllerDeployment *appsv1.Deployment, managerContainerName string) (SelfMonitoringConfiguration, error) {
	managerContainerIdx := slices.IndexFunc(controllerDeployment.Spec.Template.Spec.Containers, func(c corev1.Container) bool {
		return c.Name == managerContainerName
	})

	if managerContainerIdx < 0 {
		return SelfMonitoringConfiguration{}, &cannotFindContainerByNameError{
			ContainerName:     managerContainerName,
			WorkloadGKV:       controllerDeployment.GroupVersionKind(),
			WorkloadNamespace: controllerDeployment.Namespace,
			WorkloadName:      controllerDeployment.Name,
		}
	}

	return isSelfMonitoringEnabledInContainer(&controllerDeployment.Spec.Template.Spec.Containers[managerContainerIdx])
}

func DisableSelfMonitoringInControllerDeployment(controllerDeployment *appsv1.Deployment, managerContainerName string) error {
	managerContainerIdx := slices.IndexFunc(controllerDeployment.Spec.Template.Spec.Containers, func(c corev1.Container) bool {
		return c.Name == managerContainerName
	})

	if managerContainerIdx < 0 {
		return &cannotFindContainerByNameError{
			ContainerName:     managerContainerName,
			WorkloadGKV:       controllerDeployment.GroupVersionKind(),
			WorkloadNamespace: controllerDeployment.Namespace,
			WorkloadName:      controllerDeployment.Name,
		}
	}

	managerContainer := controllerDeployment.Spec.Template.Spec.Containers[managerContainerIdx]
	disableSelfMonitoringInContainer(&managerContainer)

	updatedControllerDeployment := controllerDeployment.DeepCopy()
	updatedControllerDeployment.Spec.Template.Spec.Containers[managerContainerIdx] = managerContainer

	return nil
}

func EnableSelfMonitoringInControllerDeployment(controllerDeployment *appsv1.Deployment, managerContainerName string, ingressEndpoint string, bearerToken string) error {
	managerContainerIdx := slices.IndexFunc(controllerDeployment.Spec.Template.Spec.Containers, func(c corev1.Container) bool {
		return c.Name == managerContainerName
	})

	if managerContainerIdx < 0 {
		return &cannotFindContainerByNameError{
			ContainerName:     managerContainerName,
			WorkloadGKV:       controllerDeployment.GroupVersionKind(),
			WorkloadNamespace: controllerDeployment.Namespace,
			WorkloadName:      controllerDeployment.Name,
		}
	}

	managerContainer := controllerDeployment.Spec.Template.Spec.Containers[managerContainerIdx]
	enableSelfMonitoringInContainer(&managerContainer, ingressEndpoint, bearerToken)

	updatedControllerDeployment := controllerDeployment.DeepCopy()
	updatedControllerDeployment.Spec.Template.Spec.Containers[managerContainerIdx] = managerContainer

	return nil
}

func isSelfMonitoringEnabledInContainer(managerContainer *corev1.Container) (SelfMonitoringConfiguration, error) {
	otelExporterOtlpEndpointEnvVarIdx := slices.IndexFunc(managerContainer.Env, matchOtelExporterOtlpEndpointEnvVar)
	if otelExporterOtlpEndpointEnvVarIdx < 0 {
		return SelfMonitoringConfiguration{
			Enabled: false,
		}, nil
	}

	otelExporterOtlpEndpointEnvVar := managerContainer.Env[otelExporterOtlpEndpointEnvVarIdx]
	if otelExporterOtlpEndpointEnvVar.Value == "" {
		return SelfMonitoringConfiguration{}, fmt.Errorf("retrieving the endpoint from ValueFrom is not supported")
	}

	bearerToken := ""
	if otelExporterOtlpHeadersEnvVarIdx := slices.IndexFunc(managerContainer.Env, matchOtelExporterOtlpHeadersEnvVar); otelExporterOtlpHeadersEnvVarIdx > -1 {
		otelExporterOtlpHeadersEnvVar := managerContainer.Env[otelExporterOtlpHeadersEnvVarIdx]
		matches := authorizationHeaderRegexp.FindStringSubmatch(otelExporterOtlpHeadersEnvVar.Value)
		if len(matches) == 2 {
			bearerToken = matches[1]
		}
	}

	return SelfMonitoringConfiguration{
		Enabled:     true,
		Endpoint:    otelExporterOtlpEndpointEnvVar.Value,
		BearerToken: bearerToken,
	}, nil
}

func disableSelfMonitoringInContainer(container *corev1.Container) {
	otelExporterOtlpEndpointEnvVarIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpEndpointEnvVar)
	if otelExporterOtlpEndpointEnvVarIdx > -1 {
		container.Env = append(container.Env[:otelExporterOtlpEndpointEnvVarIdx], container.Env[otelExporterOtlpEndpointEnvVarIdx+1:]...)

	}

	otelExporterOtlpHeadersEnvVarIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpHeadersEnvVar)
	if otelExporterOtlpHeadersEnvVarIdx > -1 {
		container.Env = append(container.Env[:otelExporterOtlpHeadersEnvVarIdx], container.Env[otelExporterOtlpHeadersEnvVarIdx+1:]...)
	}
}

func enableSelfMonitoringInContainer(container *corev1.Container, endpoint string, bearerToken string) {
	otelExporterOtlpEndpointEnvVarIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpEndpointEnvVar)
	newOtelExporterOtlpEndpointEnvVar := corev1.EnvVar{
		Name:  otelExporterOtlpEndpointEnvVarName,
		Value: endpoint,
	}
	if otelExporterOtlpEndpointEnvVarIdx > -1 {
		// We need to update the existing value
		container.Env[otelExporterOtlpEndpointEnvVarIdx] = newOtelExporterOtlpEndpointEnvVar
	} else {
		container.Env = append(container.Env, newOtelExporterOtlpEndpointEnvVar)
	}

	otelExporterOtlpHeadersEnvVarIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpHeadersEnvVar)

	if bearerToken == "" {
		// We need to remove auth if set or not set up otherwise
		if otelExporterOtlpHeadersEnvVarIdx > -1 {
			container.Env = append(container.Env[:otelExporterOtlpHeadersEnvVarIdx], container.Env[otelExporterOtlpHeadersEnvVarIdx+1:]...)
		}
	} else {
		newOtelExporterOtlpHeadersEnvVar := corev1.EnvVar{
			Name:  otelExporterOtlpHeadersEnvVarName,
			Value: fmt.Sprintf("Authorization=Bearer %v", bearerToken),
		}

		if otelExporterOtlpHeadersEnvVarIdx > -1 {
			// We need to update the existing value
			container.Env[otelExporterOtlpHeadersEnvVarIdx] = newOtelExporterOtlpHeadersEnvVar
		} else {
			// TODO Append after the OtlpEndpoint one
			container.Env = append(container.Env, newOtelExporterOtlpHeadersEnvVar)
		}
	}
}

func matchOtelExporterOtlpEndpointEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpEndpointEnvVarName
}

func matchOtelExporterOtlpHeadersEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpHeadersEnvVarName
}
