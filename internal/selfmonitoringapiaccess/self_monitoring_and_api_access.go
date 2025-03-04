// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type OtlpProtocol string

type SelfMonitoringAndApiAccessConfiguration struct {
	SelfMonitoringEnabled bool
	Export                dash0v1alpha1.Export
}

type EndpointAndHeaders struct {
	Endpoint string
	Protocol string
	Headers  []dash0v1alpha1.Header
}

const (
	otelExporterOtlpEndpointEnvVarName = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterOtlpHeadersEnvVarName  = "OTEL_EXPORTER_OTLP_HEADERS"
	otelExporterOtlpProtocolEnvVarName = "OTEL_EXPORTER_OTLP_PROTOCOL"
	otelResourceAttribtuesEnvVarName   = "OTEL_RESOURCE_ATTRIBUTES"
	otelLogLevelEnvVarName             = "OTEL_LOG_LEVEL"
)

var (
	dash0IngressEndpointRegex = regexp.MustCompile(`dash0(?:-dev)?\.com`)
	// See https://kubernetes.io/docs/tasks/inject-data-application/define-interdependent-environment-variables/
	authHeaderValue = fmt.Sprintf("Bearer $(%s)", util.SelfMonitoringAndApiAuthTokenEnvVarName)
)

func (c *SelfMonitoringAndApiAccessConfiguration) HasDash0ApiAccessConfigured() bool {
	return c.Export.Dash0 != nil &&
		c.Export.Dash0.ApiEndpoint != "" &&
		(c.Export.Dash0.Authorization.Token != nil || c.Export.Dash0.Authorization.SecretRef != nil)
}

func (c *SelfMonitoringAndApiAccessConfiguration) GetDash0Authorization() dash0v1alpha1.Authorization {
	return c.Export.Dash0.Authorization
}

func ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
	resource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger *logr.Logger,
) (SelfMonitoringAndApiAccessConfiguration, error) {
	if resource == nil {
		return SelfMonitoringAndApiAccessConfiguration{}, nil
	}

	export := resource.Spec.Export
	selfMonitoringIsEnabled := util.ReadBoolPointerWithDefault(resource.Spec.SelfMonitoring.Enabled, true)
	if export == nil {
		if selfMonitoringIsEnabled {
			logger.Info("Invalid configuration of Dash0OperatorConfiguration resource: Self-monitoring is enabled " +
				"but no export configuration is set. Self-monitoring telemetry will not be sent.")
		}
		return SelfMonitoringAndApiAccessConfiguration{}, nil
	}

	if export.Dash0 != nil {
		return convertResourceToDash0ExportConfiguration(
			export,
			selfMonitoringIsEnabled,
			logger,
		)
	}
	if export.Grpc != nil {
		return convertResourceToGrpcExportConfiguration(
			export,
			selfMonitoringIsEnabled,
			logger,
		)
	}
	if export.Http != nil {
		return convertResourceToHttpExportConfiguration(
			export,
			selfMonitoringIsEnabled,
		)
	}
	return SelfMonitoringAndApiAccessConfiguration{},
		fmt.Errorf("no export configuration for self-monitoring has been provided, no self-monitoring telemetry will be sent")
}

func convertResourceToDash0ExportConfiguration(
	export *dash0v1alpha1.Export,
	selfMonitoringEnabled bool,
	logger *logr.Logger,
) (SelfMonitoringAndApiAccessConfiguration, error) {
	if export.Grpc != nil {
		logger.Info(
			fmt.Sprintf(
				"Ignoring grpc export configuration (%s) for self-monitoring telemetry, will send to the configured Dash0 export.",
				export.Grpc.Endpoint))
	}
	if export.Http != nil {
		logger.Info(
			fmt.Sprintf(
				"Ignoring http export configuration (%s) for self-monitoring telemetry, will send to the configured Dash0 export.",
				export.Http.Endpoint))
	}

	dash0Export := export.Dash0
	return SelfMonitoringAndApiAccessConfiguration{
		SelfMonitoringEnabled: selfMonitoringEnabled,
		Export: dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint:      dash0Export.Endpoint,
				Dataset:       dash0Export.Dataset,
				Authorization: dash0Export.Authorization,
				ApiEndpoint:   dash0Export.ApiEndpoint,
			},
		},
	}, nil
}

func convertResourceToGrpcExportConfiguration(
	export *dash0v1alpha1.Export,
	selfMonitoringEnabled bool,
	logger *logr.Logger,
) (SelfMonitoringAndApiAccessConfiguration, error) {
	if export.Http != nil {
		logger.Info(
			fmt.Sprintf(
				"Ignoring http export configuration (%s) for self-monitoring telemetry, will send to the configured gRPC export.",
				export.Http.Endpoint))
	}

	grpcExport := export.Grpc
	return SelfMonitoringAndApiAccessConfiguration{
		SelfMonitoringEnabled: selfMonitoringEnabled,
		Export: dash0v1alpha1.Export{
			Grpc: &dash0v1alpha1.GrpcConfiguration{
				Endpoint: grpcExport.Endpoint,
				Headers:  grpcExport.Headers,
			},
		},
	}, nil
}

func convertResourceToHttpExportConfiguration(
	export *dash0v1alpha1.Export,
	selfMonitoringEnabled bool,
) (SelfMonitoringAndApiAccessConfiguration, error) {
	httpExport := export.Http
	if httpExport.Encoding == dash0v1alpha1.Json {
		return SelfMonitoringAndApiAccessConfiguration{
			SelfMonitoringEnabled: false,
		}, fmt.Errorf("using an HTTP exporter with JSON encoding self-monitoring is not supported")
	}
	return SelfMonitoringAndApiAccessConfiguration{
		SelfMonitoringEnabled: selfMonitoringEnabled,
		Export: dash0v1alpha1.Export{
			Http: &dash0v1alpha1.HttpConfiguration{
				Endpoint: httpExport.Endpoint,
				Headers:  httpExport.Headers,
				Encoding: httpExport.Encoding,
			},
		},
	}, nil
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

func EnableSelfMonitoringInCollectorDaemonSet(
	collectorDaemonSet *appsv1.DaemonSet,
	selfMonitoringConfiguration SelfMonitoringAndApiAccessConfiguration,
	operatorVersion string,
	developmentMode bool,
) error {
	return enableSelfMonitoringInCollector(
		collectorDaemonSet.Spec.Template.Spec.Containers,
		selfMonitoringConfiguration,
		operatorVersion,
		developmentMode,
	)
}

func EnableSelfMonitoringInCollectorDeployment(
	collectorDeployment *appsv1.Deployment,
	selfMonitoringConfiguration SelfMonitoringAndApiAccessConfiguration,
	operatorVersion string,
	developmentMode bool,
) error {
	return enableSelfMonitoringInCollector(
		collectorDeployment.Spec.Template.Spec.Containers,
		selfMonitoringConfiguration,
		operatorVersion,
		developmentMode,
	)
}

func enableSelfMonitoringInCollector(
	collectorContainers []corev1.Container,
	selfMonitoringConfiguration SelfMonitoringAndApiAccessConfiguration,
	operatorVersion string,
	developmentMode bool,
) error {
	selfMonitoringExport := selfMonitoringConfiguration.Export
	var authTokenEnvVar *corev1.EnvVar
	if selfMonitoringExport.Dash0 != nil {
		envVar, err := util.CreateEnvVarForAuthorization(
			(*(selfMonitoringExport.Dash0)).Authorization,
			util.SelfMonitoringAndApiAuthTokenEnvVarName,
		)
		if err != nil {
			return err
		}
		authTokenEnvVar = &envVar
	}

	// For now, we do not instrument init containers. The filelogoffsetsynch init container fails with:
	//     filelog-offset-init 2024/08/29 21:45:48
	//     Failed to shutdown metrics provider, metrics data nay have been lost: failed to upload metrics:
	//     failed to exit idle mode: dns resolver: missing address
	// making the collector pod go into CrashLoopBackoff.
	//
	// This is probably due to a misconfiguration of the endpoint, but ultimately it won't do if selfmonitoring issues
	// prevent the collector from starting. We probably need to remove the log.Fatalln calls entirely there.
	//
	// for i, container := range collectorDaemonSet.Spec.Template.Spec.InitContainers {
	//	enableSelfMonitoringInContainer(
	// 	  &container, selfMonitoringExport, authTokenEnvVar, operatorVersion, developmentMode)
	//	collectorDaemonSet.Spec.Template.Spec.InitContainers[i] = container
	// }

	for i, container := range collectorContainers {
		enableSelfMonitoringInContainer(
			&container,
			selfMonitoringExport,
			authTokenEnvVar,
			operatorVersion,
			developmentMode,
		)
		collectorContainers[i] = container
	}

	return nil
}

func GetSelfMonitoringAndApiAccessConfigurationFromControllerDeployment(
	controllerDeployment *appsv1.Deployment,
	controllerContainerName string,
) (SelfMonitoringAndApiAccessConfiguration, error) {
	controllerContainerIdx, err := findControllerContainer(controllerDeployment, controllerContainerName)
	if err != nil {
		return SelfMonitoringAndApiAccessConfiguration{}, &cannotFindContainerByNameError{
			ContainerName:     controllerContainerName,
			WorkloadGKV:       controllerDeployment.GroupVersionKind(),
			WorkloadNamespace: controllerDeployment.Namespace,
			WorkloadName:      controllerDeployment.Name,
		}
	}

	return ParseSelfMonitoringConfigurationFromContainer(&controllerDeployment.Spec.Template.Spec.Containers[controllerContainerIdx])
}

func ParseSelfMonitoringConfigurationFromContainer(controllerContainer *corev1.Container) (SelfMonitoringAndApiAccessConfiguration, error) {
	endpoint, err := parseEndpoint(controllerContainer)
	if err != nil {
		return SelfMonitoringAndApiAccessConfiguration{}, err
	}

	dash0Authorization := parseDash0AuthorizationFromEnvVars(controllerContainer)
	if endpoint == "" {
		if dash0Authorization != nil {
			return SelfMonitoringAndApiAccessConfiguration{
				SelfMonitoringEnabled: false,
				Export: dash0v1alpha1.Export{
					Dash0: &dash0v1alpha1.Dash0Configuration{
						Endpoint:      "",
						Authorization: *dash0Authorization,
					},
				},
			}, nil
		} else {
			return SelfMonitoringAndApiAccessConfiguration{}, nil
		}
	}

	protocolFromEnvVar := "grpc"
	otelExporterOtlpProtocolEnvVarIdx := slices.IndexFunc(controllerContainer.Env, matchOtelExporterOtlpProtocolEnvVar)
	if otelExporterOtlpProtocolEnvVarIdx >= 0 {
		protocolFromEnvVar = controllerContainer.Env[otelExporterOtlpProtocolEnvVarIdx].Value
	}

	headers := parseHeadersFromEnvVar(controllerContainer)

	switch protocolFromEnvVar {
	case "grpc":
		return createDash0OrGrpcConfigurationFromContainer(controllerContainer, endpoint, headers), nil
	case "http/json":
		return createHttpJsonConfigurationFromContainer(endpoint, headers), nil
	case "http/protobuf":
		return createHttpProtobufConfigurationFromContainer(endpoint, headers), nil

	default:
		return SelfMonitoringAndApiAccessConfiguration{}, fmt.Errorf("unsupported protocol %v", protocolFromEnvVar)
	}
}

func parseEndpoint(container *corev1.Container) (string, error) {
	otelExporterOtlpEndpointEnvVarIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpEndpointEnvVar)
	if otelExporterOtlpEndpointEnvVarIdx < 0 {
		return "", nil
	}
	otelExporterOtlpEndpointEnvVar := container.Env[otelExporterOtlpEndpointEnvVarIdx]
	if otelExporterOtlpEndpointEnvVar.Value == "" && otelExporterOtlpEndpointEnvVar.ValueFrom != nil {
		return "", fmt.Errorf("retrieving the endpoint from OTEL_EXPORTER_OTLP_ENDPOINT with a ValueFrom source is not supported")
	} else if otelExporterOtlpEndpointEnvVar.Value == "" {
		return "", fmt.Errorf("no OTEL_EXPORTER_OTLP_ENDPOINT is set")
	}
	return otelExporterOtlpEndpointEnvVar.Value, nil
}

func parseHeadersFromEnvVar(container *corev1.Container) []dash0v1alpha1.Header {
	otelExporterOtlpHeadersEnvVarValue := ""
	var headers []dash0v1alpha1.Header
	if otelExporterOtlpHeadersEnvVarIdx :=
		slices.IndexFunc(container.Env, matchOtelExporterOtlpHeadersEnvVar); otelExporterOtlpHeadersEnvVarIdx >= 0 {
		otelExporterOtlpHeadersEnvVarValue = container.Env[otelExporterOtlpHeadersEnvVarIdx].Value
		keyValuePairs := strings.Split(otelExporterOtlpHeadersEnvVarValue, ",")
		for _, keyValuePair := range keyValuePairs {
			parts := strings.Split(keyValuePair, "=")
			if len(parts) == 2 {
				headers = append(headers, dash0v1alpha1.Header{
					Name:  parts[0],
					Value: parts[1],
				})
			}
		}
	}

	return headers
}

func parseDash0AuthorizationFromEnvVars(container *corev1.Container) *dash0v1alpha1.Authorization {
	if idx := slices.IndexFunc(container.Env, matchSelfMonitoringAndApiAccessAuthTokenEnvVar); idx >= 0 {
		authTokenEnvVar := container.Env[idx]
		if authTokenEnvVar.Value != "" {
			return &dash0v1alpha1.Authorization{
				Token: &authTokenEnvVar.Value,
			}
		} else if authTokenEnvVar.ValueFrom != nil &&
			authTokenEnvVar.ValueFrom.SecretKeyRef != nil &&
			authTokenEnvVar.ValueFrom.SecretKeyRef.LocalObjectReference.Name != "" &&
			authTokenEnvVar.ValueFrom.SecretKeyRef.Key != "" {
			return &dash0v1alpha1.Authorization{
				SecretRef: &dash0v1alpha1.SecretRef{
					Name: authTokenEnvVar.ValueFrom.SecretKeyRef.LocalObjectReference.Name,
					Key:  authTokenEnvVar.ValueFrom.SecretKeyRef.Key,
				},
			}
		}
	}
	return nil
}

func DisableSelfMonitoringInControllerDeployment(
	controllerDeployment *appsv1.Deployment,
	controllerContainerName string,
	removeAuthToken bool,
) error {
	controllerContainerIdx, err := findControllerContainer(controllerDeployment, controllerContainerName)
	if err != nil {
		return err
	}

	controllerContainer := controllerDeployment.Spec.Template.Spec.Containers[controllerContainerIdx]
	disableSelfMonitoringInContainer(&controllerContainer, removeAuthToken)
	controllerDeployment.Spec.Template.Spec.Containers[controllerContainerIdx] = controllerContainer

	return nil
}

func EnableSelfMonitoringInControllerDeployment(
	controllerDeployment *appsv1.Deployment,
	controllerContainerName string,
	selfMonitoringConfiguration SelfMonitoringAndApiAccessConfiguration,
	operatorVersion string,
	developmentMode bool,
) error {
	controllerContainerIdx, err := findControllerContainer(controllerDeployment, controllerContainerName)
	if err != nil {
		return err
	}

	selfMonitoringExport := selfMonitoringConfiguration.Export
	var authTokenEnvVar *corev1.EnvVar
	if selfMonitoringExport.Dash0 != nil {
		envVar, err := util.CreateEnvVarForAuthorization(
			(*(selfMonitoringExport.Dash0)).Authorization,
			util.SelfMonitoringAndApiAuthTokenEnvVarName,
		)
		if err != nil {
			return err
		}
		authTokenEnvVar = &envVar
	}
	controllerContainer := controllerDeployment.Spec.Template.Spec.Containers[controllerContainerIdx]
	enableSelfMonitoringInContainer(
		&controllerContainer,
		selfMonitoringExport,
		authTokenEnvVar,
		operatorVersion,
		developmentMode,
	)
	controllerDeployment.Spec.Template.Spec.Containers[controllerContainerIdx] = controllerContainer

	return nil
}

func UpdateApiTokenWithoutAddingSelfMonitoringToControllerDeployment(
	controllerDeployment *appsv1.Deployment,
	controllerContainerName string,
	authorization dash0v1alpha1.Authorization,
) error {
	controllerContainerIdx, err := findControllerContainer(controllerDeployment, controllerContainerName)
	if err != nil {
		return err
	}

	var authTokenEnvVar *corev1.EnvVar
	envVar, err := util.CreateEnvVarForAuthorization(
		authorization,
		util.SelfMonitoringAndApiAuthTokenEnvVarName,
	)
	if err != nil {
		return err
	}
	authTokenEnvVar = &envVar

	controllerContainer := controllerDeployment.Spec.Template.Spec.Containers[controllerContainerIdx]
	addAuthTokenToContainer(
		&controllerContainer,
		authTokenEnvVar,
	)
	controllerDeployment.Spec.Template.Spec.Containers[controllerContainerIdx] = controllerContainer

	return nil
}

func findControllerContainer(controllerDeployment *appsv1.Deployment, controllerContainerName string) (int, error) {
	controllerContainerIdx := slices.IndexFunc(controllerDeployment.Spec.Template.Spec.Containers, func(c corev1.Container) bool {
		return c.Name == controllerContainerName
	})
	if controllerContainerIdx >= 0 {
		return controllerContainerIdx, nil
	}

	return 0, &cannotFindContainerByNameError{
		ContainerName:     controllerContainerName,
		WorkloadGKV:       controllerDeployment.GroupVersionKind(),
		WorkloadNamespace: controllerDeployment.Namespace,
		WorkloadName:      controllerDeployment.Name,
	}
}

func isDash0Export(endpoint string, headers []dash0v1alpha1.Header) bool {
	return dash0IngressEndpointRegex.MatchString(endpoint) &&
		slices.ContainsFunc(headers, func(h dash0v1alpha1.Header) bool {
			return h.Name == util.AuthorizationHeaderName
		})
}

func createDash0OrGrpcConfigurationFromContainer(container *corev1.Container, endpoint string, headers []dash0v1alpha1.Header) SelfMonitoringAndApiAccessConfiguration {
	if isDash0Export(endpoint, headers) {
		return createDash0ConfigurationFromContainer(container, endpoint, headers)
	} else {
		return createGrpcConfigurationFromContainer(endpoint, headers)
	}
}

func createDash0ConfigurationFromContainer(container *corev1.Container, endpoint string, headers []dash0v1alpha1.Header) SelfMonitoringAndApiAccessConfiguration {
	referencesTokenEnvVar := false
	dataset := ""
	for _, header := range headers {
		if header.Name == util.AuthorizationHeaderName && header.Value == authHeaderValue {
			referencesTokenEnvVar = true
		} else if header.Name == util.Dash0DatasetHeaderName {
			dataset = header.Value
		}
	}

	dash0Configuration := &dash0v1alpha1.Dash0Configuration{
		Endpoint: endpoint,
		Dataset:  dataset,
	}
	if referencesTokenEnvVar {
		authorization := parseDash0AuthorizationFromEnvVars(container)
		if authorization != nil {
			dash0Configuration.Authorization = *authorization
		}
	}
	return SelfMonitoringAndApiAccessConfiguration{
		SelfMonitoringEnabled: true,
		Export: dash0v1alpha1.Export{
			Dash0: dash0Configuration,
		},
	}
}

func createGrpcConfigurationFromContainer(endpoint string, headers []dash0v1alpha1.Header) SelfMonitoringAndApiAccessConfiguration {
	return SelfMonitoringAndApiAccessConfiguration{
		SelfMonitoringEnabled: true,
		Export: dash0v1alpha1.Export{
			Grpc: &dash0v1alpha1.GrpcConfiguration{
				Endpoint: endpoint,
				Headers:  headers,
			},
		},
	}
}

func createHttpProtobufConfigurationFromContainer(endpoint string, headers []dash0v1alpha1.Header) SelfMonitoringAndApiAccessConfiguration {
	return SelfMonitoringAndApiAccessConfiguration{
		SelfMonitoringEnabled: true,
		Export: dash0v1alpha1.Export{
			Http: &dash0v1alpha1.HttpConfiguration{
				Endpoint: endpoint,
				Headers:  headers,
				Encoding: dash0v1alpha1.Proto,
			},
		},
	}
}

func createHttpJsonConfigurationFromContainer(endpoint string, headers []dash0v1alpha1.Header) SelfMonitoringAndApiAccessConfiguration {
	return SelfMonitoringAndApiAccessConfiguration{
		SelfMonitoringEnabled: true,
		Export: dash0v1alpha1.Export{
			Http: &dash0v1alpha1.HttpConfiguration{
				Endpoint: endpoint,
				Headers:  headers,
				Encoding: dash0v1alpha1.Json,
			},
		},
	}
}

func enableSelfMonitoringInContainer(
	container *corev1.Container,
	selfMonitoringExport dash0v1alpha1.Export,
	authTokenEnvVar *corev1.EnvVar,
	operatorVersion string,
	developmentMode bool,
) {
	if authTokenEnvVar != nil {
		addAuthTokenToContainer(container, authTokenEnvVar)
	}

	exportSettings := ConvertExportConfigurationToEnvVarSettings(selfMonitoringExport)
	updateOrAppendEnvVar(container, otelExporterOtlpEndpointEnvVarName, exportSettings.Endpoint)
	updateOrAppendEnvVar(container, otelExporterOtlpProtocolEnvVarName, exportSettings.Protocol)
	updateOrAppendEnvVar(container, otelResourceAttribtuesEnvVarName,
		fmt.Sprintf(
			"service.namespace=dash0.operator,service.name=%s,service.version=%s",
			container.Name,
			operatorVersion,
		))
	if developmentMode {
		updateOrAppendEnvVar(container, otelLogLevelEnvVarName, "debug")
	}

	headers := exportSettings.Headers
	headersEnvVarIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpHeadersEnvVar)
	if len(headers) == 0 {
		// We need to remove headers if set up
		if headersEnvVarIdx >= 0 {
			container.Env =
				slices.Delete(container.Env, headersEnvVarIdx, headersEnvVarIdx+1)
		}
	} else {
		newOtelExporterOtlpHeadersEnvVar := corev1.EnvVar{
			Name:  otelExporterOtlpHeadersEnvVarName,
			Value: convertHeadersToEnvVarValue(headers),
		}
		if headersEnvVarIdx >= 0 {
			// update the existing environment variable
			container.Env[headersEnvVarIdx] = newOtelExporterOtlpHeadersEnvVar
		} else {
			// append a new environment variable
			headersEnvVarIdx = slices.IndexFunc(container.Env, matchOtelExporterOtlpEndpointEnvVar)
			container.Env = slices.Insert(container.Env, headersEnvVarIdx+1, newOtelExporterOtlpHeadersEnvVar)
		}
	}
}

func addAuthTokenToContainer(container *corev1.Container, authTokenEnvVar *corev1.EnvVar) {
	authTokenEnvVarIdx := slices.IndexFunc(container.Env, matchSelfMonitoringAndApiAccessAuthTokenEnvVar)
	if authTokenEnvVarIdx == 0 {
		// update the existing value
		container.Env[authTokenEnvVarIdx] = *authTokenEnvVar
	} else if authTokenEnvVarIdx > 0 {
		// Since we reference this env var in the OTEL_EXPORTER_OTLP_HEADERS env var, we want to have this as the
		// very first env var, to make sure it is defined before OTEL_EXPORTER_OTLP_HEADERS. (This is a requirement
		// for using
		// https://kubernetes.io/docs/tasks/inject-data-application/define-interdependent-environment-variables/.)
		container.Env = slices.Delete(container.Env, authTokenEnvVarIdx, authTokenEnvVarIdx+1)
		container.Env = slices.Insert(container.Env, 0, *authTokenEnvVar)
	} else {
		// the env var is not present yet, add it to the start of the list
		container.Env = slices.Insert(container.Env, 0, *authTokenEnvVar)
	}
}

func ConvertExportConfigurationToEnvVarSettings(selfMonitoringExport dash0v1alpha1.Export) EndpointAndHeaders {
	if selfMonitoringExport.Dash0 != nil {
		dash0Export := selfMonitoringExport.Dash0
		headers := []dash0v1alpha1.Header{{
			Name:  util.AuthorizationHeaderName,
			Value: authHeaderValue,
		}}
		if dash0Export.Dataset != "" && dash0Export.Dataset != util.DatasetDefault {
			headers = append(headers, dash0v1alpha1.Header{
				Name:  util.Dash0DatasetHeaderName,
				Value: dash0Export.Dataset,
			})
		}
		return EndpointAndHeaders{
			Endpoint: prependProtocol(dash0Export.Endpoint, "https://"),
			Protocol: "grpc",
			Headers:  headers,
		}
	}

	if selfMonitoringExport.Grpc != nil {
		return EndpointAndHeaders{
			Endpoint: prependProtocol(selfMonitoringExport.Grpc.Endpoint, "dns://"),
			Protocol: "grpc",
			Headers:  selfMonitoringExport.Grpc.Headers,
		}
	}

	if selfMonitoringExport.Http != nil {
		protocol := "http/protobuf"
		// The Go SDK does not support http/json, so we ignore this setting for now.
		// if selfMonitoringExport.Http.Encoding == dash0v1alpha1.Json {
		// 	 protocol = "http/json"
		// }
		return EndpointAndHeaders{
			Endpoint: selfMonitoringExport.Http.Endpoint,
			Protocol: protocol,
			Headers:  selfMonitoringExport.Http.Headers,
		}
	}
	return EndpointAndHeaders{}
}

func prependProtocol(endpoint string, defaultProtocol string) string {
	// Most gRPC implementations are fine without a protocol, but the Go SDK with gRPC requires the endpoint with a
	// protocol, see https://github.com/open-telemetry/opentelemetry-go/pull/5632.
	if !regexp.MustCompile(`^\w+://`).MatchString(endpoint) {
		// See https://grpc.github.io/grpc/core/md_doc_naming.html
		return defaultProtocol + endpoint
	}
	return endpoint
}

func convertHeadersToEnvVarValue(headers []dash0v1alpha1.Header) string {
	keyValuePairs := make([]string, 0, len(headers))
	for _, header := range headers {
		keyValuePairs = append(keyValuePairs, fmt.Sprintf("%v=%v", header.Name, header.Value))
	}
	return strings.Join(keyValuePairs, ",")
}

func disableSelfMonitoringInContainer(container *corev1.Container, removeAuthToken bool) {
	if removeAuthToken {
		removeEnvVar(container, util.SelfMonitoringAndApiAuthTokenEnvVarName)
	}
	removeEnvVar(container, otelExporterOtlpEndpointEnvVarName)
	removeEnvVar(container, otelExporterOtlpProtocolEnvVarName)
	removeEnvVar(container, otelExporterOtlpHeadersEnvVarName)
	removeEnvVar(container, otelResourceAttribtuesEnvVarName)
}

func updateOrAppendEnvVar(container *corev1.Container, name string, value string) {
	newEnvVar := corev1.EnvVar{
		Name:  name,
		Value: value,
	}
	idx := slices.IndexFunc(container.Env, func(e corev1.EnvVar) bool {
		return e.Name == name
	})
	if idx >= 0 {
		// We need to update the existing value
		container.Env[idx] = newEnvVar
	} else {
		container.Env = append(container.Env, newEnvVar)
	}
}

func removeEnvVar(container *corev1.Container, name string) {
	idx := slices.IndexFunc(container.Env, func(e corev1.EnvVar) bool {
		return e.Name == name
	})
	if idx >= 0 {
		container.Env = slices.Delete(container.Env, idx, idx+1)
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

func matchSelfMonitoringAndApiAccessAuthTokenEnvVar(e corev1.EnvVar) bool {
	return e.Name == util.SelfMonitoringAndApiAuthTokenEnvVarName
}
