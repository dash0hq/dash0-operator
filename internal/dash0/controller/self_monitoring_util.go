// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	// TODO Use the const from the Go SDK when we implement self-monitoring
	otelExporterOtlpEndpointEnvVarName = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterOtlpHeadersEnvVarName  = "OTEL_EXPORTER_OTLP_HEADERS"
)

type CannotFindManagerContainerError struct {
	DeploymentNamespace string
	DeploymentName      string
}

func (c *CannotFindManagerContainerError) Error() string {
	return fmt.Sprintf("cannot find the container named '%v' in the controller's own deployment %v/%v", ManagerContainerName, c.DeploymentNamespace, c.DeploymentName)
}

func IsSelfMonitoringEnabled(controllerDeployment *appsv1.Deployment) (bool, error) {
	managerContainer, err := getManagerContainer(controllerDeployment)
	if err != nil {
		return false, &CannotFindManagerContainerError{
			DeploymentNamespace: controllerDeployment.Namespace,
			DeploymentName:      controllerDeployment.Name,
		}
	}

	otelExporterOtlpEndpointEnvVarIdx := slices.IndexFunc(managerContainer.Env, matchOtelExporterOtlpEndpointEnvVar)

	return otelExporterOtlpEndpointEnvVarIdx > -1, nil
}

func DisableSelfMonitoring(controllerDeployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	updatedControllerDeployment := controllerDeployment.DeepCopy()
	managerContainer, err := getManagerContainer(updatedControllerDeployment)
	if err != nil {
		return nil, &CannotFindManagerContainerError{
			DeploymentNamespace: controllerDeployment.Namespace,
			DeploymentName:      controllerDeployment.Name,
		}
	}

	// Remove OTLP env vars
	otelExporterOtlpEndpointEnvVarIdx := slices.IndexFunc(managerContainer.Env, matchOtelExporterOtlpEndpointEnvVar)
	if otelExporterOtlpEndpointEnvVarIdx > -1 {
		managerContainer.Env = append(managerContainer.Env[:otelExporterOtlpEndpointEnvVarIdx], managerContainer.Env[otelExporterOtlpEndpointEnvVarIdx+1:]...)

	}

	otelExporterOtlpHeadersEnvVarIdx := slices.IndexFunc(managerContainer.Env, matchOtelExporterOtlpHeadersEnvVar)
	if otelExporterOtlpHeadersEnvVarIdx > -1 {
		managerContainer.Env = append(managerContainer.Env[:otelExporterOtlpHeadersEnvVarIdx], managerContainer.Env[otelExporterOtlpHeadersEnvVarIdx+1:]...)
	}

	return updatedControllerDeployment, nil
}

func EnableSelfMonitoring(controllerDeployment *appsv1.Deployment, ingressEndpoint string, bearerToken string) (*appsv1.Deployment, error) {
	updatedControllerDeployment := controllerDeployment.DeepCopy()
	managerContainer, err := getManagerContainer(updatedControllerDeployment)
	if err != nil {
		return nil, &CannotFindManagerContainerError{
			DeploymentNamespace: controllerDeployment.Namespace,
			DeploymentName:      controllerDeployment.Name,
		}
	}

	otelExporterOtlpEndpointEnvVarIdx := slices.IndexFunc(managerContainer.Env, matchOtelExporterOtlpEndpointEnvVar)
	newOtelExporterOtlpEndpointEnvVar := corev1.EnvVar{
		Name:  otelExporterOtlpEndpointEnvVarName,
		Value: ingressEndpoint,
	}
	if otelExporterOtlpEndpointEnvVarIdx > -1 {
		// We need to update the existing value
		managerContainer.Env[otelExporterOtlpEndpointEnvVarIdx] = newOtelExporterOtlpEndpointEnvVar
	} else {
		managerContainer.Env = append(managerContainer.Env, newOtelExporterOtlpEndpointEnvVar)
	}

	otelExporterOtlpHeadersEnvVarIdx := slices.IndexFunc(managerContainer.Env, matchOtelExporterOtlpHeadersEnvVar)

	if bearerToken == "" {
		// We need to remove auth if set or not set up otherwise
		if otelExporterOtlpHeadersEnvVarIdx > -1 {
			managerContainer.Env = append(managerContainer.Env[:otelExporterOtlpHeadersEnvVarIdx], managerContainer.Env[otelExporterOtlpHeadersEnvVarIdx+1:]...)
		}
	} else {
		newOtelExporterOtlpHeadersEnvVar := corev1.EnvVar{
			Name:  otelExporterOtlpHeadersEnvVarName,
			Value: fmt.Sprintf("Authorization=Bearer %v", bearerToken),
		}

		if otelExporterOtlpHeadersEnvVarIdx > -1 {
			// We need to update the existing value
			managerContainer.Env[otelExporterOtlpHeadersEnvVarIdx] = newOtelExporterOtlpHeadersEnvVar
		} else {
			// TODO Append after the OtlpEndpoint one
			managerContainer.Env = append(managerContainer.Env, newOtelExporterOtlpHeadersEnvVar)
		}
	}

	return updatedControllerDeployment, nil
}

func getManagerContainer(controllerDeployment *appsv1.Deployment) (*corev1.Container, error) {
	managerContainerIdx := slices.IndexFunc(controllerDeployment.Spec.Template.Spec.Containers, func(c corev1.Container) bool {
		return c.Name == ManagerContainerName
	})

	if managerContainerIdx < 0 {
		return nil, &CannotFindManagerContainerError{
			DeploymentNamespace: controllerDeployment.Namespace,
			DeploymentName:      controllerDeployment.Name,
		}
	}

	return &controllerDeployment.Spec.Template.Spec.Containers[managerContainerIdx], nil
}

func matchOtelExporterOtlpEndpointEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpEndpointEnvVarName
}

func matchOtelExporterOtlpHeadersEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpHeadersEnvVarName
}
