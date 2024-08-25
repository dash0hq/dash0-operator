// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoring

import (
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"reflect"
	"regexp"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
)

const (
	// TODO Use the const from the Go SDK when we implement self-monitoring
	otelExporterOtlpEndpointEnvVarName = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterOtlpHeadersEnvVarName  = "OTEL_EXPORTER_OTLP_HEADERS"
	otelExporterOtlpProtocolEnvVarName = "OTEL_EXPORTER_OTLP_PROTOCOL"
)

var (
	// Match in a case-insensitive fashion the structure of an HTTP Authorization header with Bearer schema,
	// and declare the named capture group `<bearerToken>` to extract the token's value.
	authorizationHeaderRegexp = regexp.MustCompile(`(?i:Authorization=Bearer (?P<bearerToken>\S+))`)
)

type OtlpProtocol string

type SelfMonitoringConfiguration struct {
	Enabled  bool
	Endpoint string
	Protocol v1alpha1.OtlpProtocol
	Headers  []string
}

type cannotFindContainerByNameError struct {
	ContainerName     string
	WorkloadGKV       schema.GroupVersionKind
	WorkloadNamespace string
	WorkloadName      string
}

func GetSelfMonitoringConfigurationFromOperatorConfigurationResource(resource v1alpha1.Dash0OperatorConfiguration) (SelfMonitoringConfiguration, error) {
	if !resource.Spec.SelfMonitoring.Enabled || resource.Spec.Export == nil {
		// TODO Log that we disable the self-monitoring because Export is nil
		return SelfMonitoringConfiguration{
			Enabled: false,
		}, nil
	}

	if resource.Spec.Export.Dash0Export != nil {
		return SelfMonitoringConfiguration{
			Enabled:  true,
			Protocol: v1alpha1.OtlpProtocolGrpc,
			Endpoint: resource.Spec.Export.Dash0Export.Endpoint,
			// TODO lock-in the insights dataset
			// TODO Add support for secret ref
			Headers: []string{fmt.Sprintf("Authorization=Bearer %v", resource.Spec.Export.Dash0Export.Authorization.Token)},
		}, nil
	}

	if resource.Spec.Export.HttpExport != nil {
		if resource.Spec.Export.HttpExport.Encoding == v1alpha1.OtlpProtocolHttpJson {
			return SelfMonitoringConfiguration{
				Enabled: false,
			}, fmt.Errorf("the HTTP/JSON encoding is not yet supported")
		}

		return SelfMonitoringConfiguration{
			Enabled: true,
			// TODO Throw an error if HTTP_JSON, it seems not supported in the Go SDK we use for self-monitoring
			Protocol: v1alpha1.OtlpProtocolHttpProtobuf,
			Endpoint: resource.Spec.Export.HttpExport.Url,
			// TODO Add support for secret ref
			Headers: resource.Spec.Export.HttpExport.Headers,
		}, nil
	}

	if resource.Spec.Export.GrpcExport != nil {
		return SelfMonitoringConfiguration{
			Enabled:  true,
			Protocol: v1alpha1.OtlpProtocolGrpc,
			Endpoint: resource.Spec.Export.GrpcExport.Url,
			// TODO Add support for secret ref
			Headers: resource.Spec.Export.GrpcExport.Headers,
		}, nil
	}

	return SelfMonitoringConfiguration{
		Enabled: false,
	}, fmt.Errorf("cannot find self-monitoring endpoint configurations")
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
			if !reflect.DeepEqual(*referenceMonitoringConfiguration, selfMonitoringConfiguration) {
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

func EnableSelfMonitoringInCollectorDaemonSet(collectorDemonSet *appsv1.DaemonSet, selfMonitoringConfiguration SelfMonitoringConfiguration) error {
	for i, container := range collectorDemonSet.Spec.Template.Spec.InitContainers {
		enableSelfMonitoringInContainer(&container, selfMonitoringConfiguration)
		collectorDemonSet.Spec.Template.Spec.InitContainers[i] = container
	}

	for i, container := range collectorDemonSet.Spec.Template.Spec.Containers {
		enableSelfMonitoringInContainer(&container, selfMonitoringConfiguration)
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
		return SelfMonitoringConfiguration{
				Enabled: false,
			}, &cannotFindContainerByNameError{
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

	controllerDeployment.Spec.Template.Spec.Containers[managerContainerIdx] = managerContainer

	return nil
}

func EnableSelfMonitoringInControllerDeployment(controllerDeployment *appsv1.Deployment, managerContainerName string, selfMonitoringConfiguration SelfMonitoringConfiguration) error {
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
	enableSelfMonitoringInContainer(&managerContainer, selfMonitoringConfiguration)

	controllerDeployment.Spec.Template.Spec.Containers[managerContainerIdx] = managerContainer

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

	otelExporterOtlpProtocol := v1alpha1.OtlpProtocolGrpc
	otelExporterOtlpProtocolEnvVarIdx := slices.IndexFunc(managerContainer.Env, matchOtelExporterOtlpProtocolEnvVar)
	if otelExporterOtlpProtocolEnvVarIdx > -1 {
		otelExporterOtlpProtocol = v1alpha1.OtlpProtocol(managerContainer.Env[otelExporterOtlpProtocolEnvVarIdx].Value)
	}

	otelExporterOtlpHeadersEnvVarValue := ""
	if otelExporterOtlpHeadersEnvVarIdx := slices.IndexFunc(managerContainer.Env, matchOtelExporterOtlpHeadersEnvVar); otelExporterOtlpHeadersEnvVarIdx > -1 {
		otelExporterOtlpHeadersEnvVarValue = managerContainer.Env[otelExporterOtlpHeadersEnvVarIdx].Value
	}

	return SelfMonitoringConfiguration{
		Enabled:  true,
		Endpoint: otelExporterOtlpEndpointEnvVar.Value,
		Protocol: otelExporterOtlpProtocol,
		Headers:  strings.Split(otelExporterOtlpHeadersEnvVarValue, ","),
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

func enableSelfMonitoringInContainer(container *corev1.Container, selfMonitoringConfiguration SelfMonitoringConfiguration) {
	otelExporterOtlpEndpointEnvVarIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpEndpointEnvVar)
	newOtelExporterOtlpEndpointEnvVar := corev1.EnvVar{
		Name:  otelExporterOtlpEndpointEnvVarName,
		Value: selfMonitoringConfiguration.Endpoint,
	}
	if otelExporterOtlpEndpointEnvVarIdx > -1 {
		// We need to update the existing value
		container.Env[otelExporterOtlpEndpointEnvVarIdx] = newOtelExporterOtlpEndpointEnvVar
	} else {
		container.Env = append(container.Env, newOtelExporterOtlpEndpointEnvVar)
	}

	otelExporterOtlpHeadersEnvVarIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpHeadersEnvVar)

	if len(selfMonitoringConfiguration.Headers) < 1 {
		// We need to remove headers if set up
		if otelExporterOtlpHeadersEnvVarIdx > -1 {
			container.Env = append(container.Env[:otelExporterOtlpHeadersEnvVarIdx], container.Env[otelExporterOtlpHeadersEnvVarIdx+1:]...)
		}
	} else {
		newOtelExporterOtlpHeadersEnvVar := corev1.EnvVar{
			Name:  otelExporterOtlpHeadersEnvVarName,
			Value: strings.Join(selfMonitoringConfiguration.Headers, ","),
		}

		if otelExporterOtlpHeadersEnvVarIdx > -1 {
			// We need to update the existing value
			container.Env[otelExporterOtlpHeadersEnvVarIdx] = newOtelExporterOtlpHeadersEnvVar
		} else {
			// Append after the OtlpEndpoint one
			otelExporterOtlpEndpointEnvVarIdx = slices.IndexFunc(container.Env, matchOtelExporterOtlpEndpointEnvVar)

			env := container.Env[:otelExporterOtlpEndpointEnvVarIdx+1]
			env = append(env, newOtelExporterOtlpHeadersEnvVar)
			if otelExporterOtlpEndpointEnvVarIdx+2 < len(env) {
				env = append(env, container.Env[otelExporterOtlpEndpointEnvVarIdx+2:]...)
			}
			container.Env = env
		}
	}
}

func matchOtelExporterOtlpEndpointEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpEndpointEnvVarName
}

func matchOtelExporterOtlpHeadersEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpHeadersEnvVarName
}

func matchOtelExporterOtlpProtocolEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpProtocolEnvVarName
}
