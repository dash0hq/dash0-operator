// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type OtlpProtocol string

type SelfMonitoringConfiguration struct {
	SelfMonitoringEnabled bool
	Export                dash0common.Export
	Token                 *string // the resolved token (in case of a Dash0 export)
}

type EndpointAndHeaders struct {
	Endpoint string
	Protocol string
	Headers  []dash0common.Header
}

const (
	CollectorSelfMonitoringAuthTokenEnvVarName = "SELF_MONITORING_AUTH_TOKEN"

	otelExporterOtlpEndpointEnvVarName = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterOtlpHeadersEnvVarName  = "OTEL_EXPORTER_OTLP_HEADERS"
	otelExporterOtlpProtocolEnvVarName = "OTEL_EXPORTER_OTLP_PROTOCOL"
	otelLogLevelEnvVarName             = "OTEL_LOG_LEVEL"
)

var (
	// See https://kubernetes.io/docs/tasks/inject-data-application/define-interdependent-environment-variables/
	selfMonitoringAuthHeaderValue = fmt.Sprintf("Bearer $(%s)", CollectorSelfMonitoringAuthTokenEnvVarName)

	collectorMetricsSelfMonitoringPrelude = `
    metrics:
      readers:
        - periodic:
            interval: 30000
            timeout: 10000
            exporter:
              otlp:`
	collectorLogsSelfMonitoringPrelude = `
    logs:
      processors:
        - batch:
            exporter:
              otlp:`
)

func ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	resource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger *logr.Logger,
) (SelfMonitoringConfiguration, error) {
	if resource == nil {
		return SelfMonitoringConfiguration{}, nil
	}

	selfMonitoringIsEnabled := util.ReadBoolPointerWithDefault(resource.Spec.SelfMonitoring.Enabled, true)
	if !selfMonitoringIsEnabled {
		return SelfMonitoringConfiguration{}, nil
	}

	if !resource.HasExportsConfigured() {
		logger.Info(
			"Invalid configuration of Dash0OperatorConfiguration resource: Self-monitoring is enabled " +
				"but no export configuration is set. Self-monitoring telemetry will not be sent.",
		)
		return SelfMonitoringConfiguration{}, nil
	}

	// for self-monitoring we only send telemetry to a single backend
	export := resource.Spec.Exports[0]
	if export.Dash0 != nil {
		token, err := GetAuthTokenForDash0Export(ctx, k8sClient, operatorNamespace, *export.Dash0, *logger)
		if err != nil || token == nil {
			logger.Info(
				"Self-monitoring is enabled but either no authorization is defined or the token could not be retrieved. " +
					"Self-monitoring telemetry will not be sent.",
			)
			return SelfMonitoringConfiguration{}, nil
		}
		return convertResourceToDash0ExportConfiguration(
			&export,
			token,
			selfMonitoringIsEnabled,
			logger,
		)
	}
	if export.Grpc != nil {
		return convertResourceToGrpcExportConfiguration(
			&export,
			selfMonitoringIsEnabled,
			logger,
		)
	}
	if export.Http != nil {
		return convertResourceToHttpExportConfiguration(
			&export,
			selfMonitoringIsEnabled,
		)
	}
	return SelfMonitoringConfiguration{},
		fmt.Errorf("no export configuration for self-monitoring has been provided, no self-monitoring telemetry will be sent")
}

func convertResourceToDash0ExportConfiguration(
	export *dash0common.Export,
	token *string,
	selfMonitoringEnabled bool,
	logger *logr.Logger,
) (SelfMonitoringConfiguration, error) {
	if export.Grpc != nil {
		logger.Info(
			fmt.Sprintf(
				"Ignoring grpc export configuration (%s) for self-monitoring telemetry, will send to the configured Dash0 export.",
				export.Grpc.Endpoint,
			),
		)
	}
	if export.Http != nil {
		logger.Info(
			fmt.Sprintf(
				"Ignoring http export configuration (%s) for self-monitoring telemetry, will send to the configured Dash0 export.",
				export.Http.Endpoint,
			),
		)
	}

	dash0Export := export.Dash0
	return SelfMonitoringConfiguration{
		SelfMonitoringEnabled: selfMonitoringEnabled,
		Export: dash0common.Export{
			Dash0: &dash0common.Dash0Configuration{
				Endpoint:      dash0Export.Endpoint,
				Dataset:       dash0Export.Dataset,
				Authorization: dash0Export.Authorization,
				ApiEndpoint:   dash0Export.ApiEndpoint,
			},
		},
		Token: token,
	}, nil
}

func convertResourceToGrpcExportConfiguration(
	export *dash0common.Export,
	selfMonitoringEnabled bool,
	logger *logr.Logger,
) (SelfMonitoringConfiguration, error) {
	if export.Http != nil {
		logger.Info(
			fmt.Sprintf(
				"Ignoring http export configuration (%s) for self-monitoring telemetry, will send to the configured gRPC export.",
				export.Http.Endpoint,
			),
		)
	}

	grpcExport := export.Grpc
	return SelfMonitoringConfiguration{
		SelfMonitoringEnabled: selfMonitoringEnabled,
		Export: dash0common.Export{
			Grpc: &dash0common.GrpcConfiguration{
				Endpoint: grpcExport.Endpoint,
				Headers:  grpcExport.Headers,
			},
		},
	}, nil
}

func convertResourceToHttpExportConfiguration(
	export *dash0common.Export,
	selfMonitoringEnabled bool,
) (SelfMonitoringConfiguration, error) {
	httpExport := export.Http
	if httpExport.Encoding == dash0common.Json {
		return SelfMonitoringConfiguration{
			SelfMonitoringEnabled: false,
		}, fmt.Errorf("using an HTTP exporter with JSON encoding self-monitoring is not supported")
	}
	return SelfMonitoringConfiguration{
		SelfMonitoringEnabled: selfMonitoringEnabled,
		Export: dash0common.Export{
			Http: &dash0common.HttpConfiguration{
				Endpoint: httpExport.Endpoint,
				Headers:  httpExport.Headers,
				Encoding: httpExport.Encoding,
			},
		},
	}, nil
}

func EnableSelfMonitoringInCollectorDaemonSet(
	collectorDaemonSet *appsv1.DaemonSet,
	selfMonitoringConfiguration SelfMonitoringConfiguration,
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
	selfMonitoringConfiguration SelfMonitoringConfiguration,
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
	selfMonitoringConfiguration SelfMonitoringConfiguration,
	operatorVersion string,
	developmentMode bool,
) error {
	selfMonitoringExport := selfMonitoringConfiguration.Export
	var authTokenEnvVar *corev1.EnvVar
	if selfMonitoringExport.Dash0 != nil {
		envVar, err := util.CreateEnvVarForAuthorization(
			(*(selfMonitoringExport.Dash0)).Authorization,
			CollectorSelfMonitoringAuthTokenEnvVarName,
		)
		if err != nil {
			return err
		}
		authTokenEnvVar = &envVar
	}

	// For now, we do not instrument init containers. The filelogoffsetsync init container fails with:
	//     filelog-offset-init 2024/08/29 21:45:48
	//     Failed to shutdown metrics provider, metrics data nay have been lost: failed to upload metrics:
	//     failed to exit idle mode: dns resolver: missing address
	// making the collector pod go into CrashLoopBackoff.
	//
	// This is probably due to a misconfiguration of the endpoint, but ultimately it won't do if selfmonitoring issues
	// prevent the collector from starting. We probably need to remove the log.Fatalln calls entirely there.
	//
	// for i, container := range collectorDaemonSet.Spec.Template.Spec.InitContainers {
	//	enableSelfMonitoringInCollectorContainer(
	// 	  &container, selfMonitoringExport, authTokenEnvVar, operatorVersion, developmentMode)
	//	collectorDaemonSet.Spec.Template.Spec.InitContainers[i] = container
	// }

	for i, container := range collectorContainers {
		enableSelfMonitoringInCollectorContainer(
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

func enableSelfMonitoringInCollectorContainer(
	container *corev1.Container,
	selfMonitoringExport dash0common.Export,
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
	updateOrAppendEnvVar(
		container, util.OtelResourceAttributesEnvVarName,
		fmt.Sprintf(
			"service.namespace=dash0-operator,service.name=%s,service.version=%s",
			container.Name,
			operatorVersion,
		),
	)
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
	authTokenEnvVarIdx := slices.IndexFunc(container.Env, matchEnvVar(authTokenEnvVar.Name))
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

func convertHeadersToEnvVarValue(headers []dash0common.Header) string {
	keyValuePairs := make([]string, 0, len(headers))
	for _, header := range headers {
		keyValuePairs = append(keyValuePairs, fmt.Sprintf("%v=%v", header.Name, header.Value))
	}
	return strings.Join(keyValuePairs, ",")
}

func updateOrAppendEnvVar(container *corev1.Container, name string, value string) {
	newEnvVar := corev1.EnvVar{
		Name:  name,
		Value: value,
	}
	idx := slices.IndexFunc(
		container.Env, func(e corev1.EnvVar) bool {
			return e.Name == name
		},
	)
	if idx >= 0 {
		// We need to update the existing value
		container.Env[idx] = newEnvVar
	} else {
		container.Env = append(container.Env, newEnvVar)
	}
}

func matchOtelExporterOtlpEndpointEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpEndpointEnvVarName
}

func matchOtelExporterOtlpHeadersEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpHeadersEnvVarName
}

func matchEnvVar(envVarName string) func(corev1.EnvVar) bool {
	return func(e corev1.EnvVar) bool {
		return e.Name == envVarName
	}
}

// ConvertExportConfigurationToEnvVarSettings is used when enabling self-monitoring in a container by configuring
// the OpenTelemetry Go SDK via _environment variable_. We use this approach for the OTel collector pods that the
// operator starts.
func ConvertExportConfigurationToEnvVarSettings(selfMonitoringExport dash0common.Export) EndpointAndHeaders {
	if selfMonitoringExport.Dash0 != nil {
		dash0Export := selfMonitoringExport.Dash0
		headers := []dash0common.Header{
			{
				Name:  util.AuthorizationHeaderName,
				Value: selfMonitoringAuthHeaderValue,
			},
		}
		if dash0Export.Dataset != "" && dash0Export.Dataset != util.DatasetDefault {
			headers = append(
				headers, dash0common.Header{
					Name:  util.Dash0DatasetHeaderName,
					Value: dash0Export.Dataset,
				},
			)
		}
		return EndpointAndHeaders{
			Endpoint: prependProtocol(dash0Export.Endpoint, "https://"),
			Protocol: common.ProtocolGrpc,
			Headers:  headers,
		}
	}

	if selfMonitoringExport.Grpc != nil {
		return EndpointAndHeaders{
			Endpoint: prependProtocol(selfMonitoringExport.Grpc.Endpoint, "dns://"),
			Protocol: common.ProtocolGrpc,
			Headers:  selfMonitoringExport.Grpc.Headers,
		}
	}

	if selfMonitoringExport.Http != nil {
		protocol := common.ProtocolHttpProtobuf
		// The Go SDK does not support http/json, so we ignore this setting for now.
		// if selfMonitoringExport.Http.Encoding == dash0common.Json {
		// 	 protocol = common.ProtocolHttpJson
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
	// protocol when setting it via OTEL_EXPORTER_OTLP_ENDPOINT, see
	// https://github.com/open-telemetry/opentelemetry-go/pull/5632.
	if !common.EndpointHasScheme(endpoint) {
		// See https://grpc.github.io/grpc/core/md_doc_naming.html
		return defaultProtocol + endpoint
	}
	return endpoint
}

// ConvertExportConfigurationToCollectorMetricsSelfMonitoringPipelineString is used to create a snippet that can be
// added to the collector config maps for sending the collector's metrics to a configured export.
func ConvertExportConfigurationToCollectorMetricsSelfMonitoringPipelineString(selfMonitoringConfiguration SelfMonitoringConfiguration) string {
	return convertExportConfigurationToCollectorSelfMonitoringPipelineString(
		collectorMetricsSelfMonitoringPrelude,
		selfMonitoringConfiguration,
	)
}

// ConvertExportConfigurationToCollectorLogsSelfMonitoringPipelineString is used to create a snippet that can be added to
// the collector config maps for sending the collector's logs to a configured export.
func ConvertExportConfigurationToCollectorLogsSelfMonitoringPipelineString(selfMonitoringConfiguration SelfMonitoringConfiguration) string {
	return convertExportConfigurationToCollectorSelfMonitoringPipelineString(
		collectorLogsSelfMonitoringPrelude,
		selfMonitoringConfiguration,
	)
}

func convertExportConfigurationToCollectorSelfMonitoringPipelineString(
	prelude string,
	selfMonitoringConfiguration SelfMonitoringConfiguration,
) string {
	if !selfMonitoringConfiguration.SelfMonitoringEnabled {
		return ""
	}
	selfMonitoringExport := selfMonitoringConfiguration.Export
	if selfMonitoringExport.Dash0 != nil {
		return convertDash0ExportConfigurationToCollectorLogSelfMonitoringPipelineString(
			prelude,
			selfMonitoringExport.Dash0,
		)
	} else if selfMonitoringExport.Grpc != nil {
		return convertGrpcExportConfigurationToCollectorLogSelfMonitoringPipelineString(prelude, selfMonitoringExport.Grpc)
	} else if selfMonitoringExport.Http != nil {
		return convertHttpExportConfigurationToCollectorLogSelfMonitoringPipelineString(prelude, selfMonitoringExport.Http)
	}
	return ""
}

func convertDash0ExportConfigurationToCollectorLogSelfMonitoringPipelineString(
	prelude string,
	dash0Export *dash0common.Dash0Configuration,
) string {
	pipeline := prelude +
		fmt.Sprintf(
			`
                protocol: grpc
                endpoint: %s`, prependProtocol(dash0Export.Endpoint, "https://"),
		)
	pipeline = addInsecureFlagIfNecessary(pipeline, dash0Export.Endpoint)
	pipeline += fmt.Sprintf(
		`
                headers:
                  %s: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"`,
		util.AuthorizationHeaderName,
	)
	if dash0Export.Dataset != "" && dash0Export.Dataset != util.DatasetDefault {
		pipeline += fmt.Sprintf(
			`
                  %s: "%s"
`, util.Dash0DatasetHeaderName, dash0Export.Dataset,
		)
	} else {
		// Append final newline, which is deliberately not included in the snippet that includes protocol and
		// endpoint.
		pipeline += "\n"
	}
	return pipeline
}

func convertGrpcExportConfigurationToCollectorLogSelfMonitoringPipelineString(
	prelude string,
	grpcExport *dash0common.GrpcConfiguration,
) string {
	pipeline := prelude +
		fmt.Sprintf(
			`
                protocol: grpc
                endpoint: %s`,
			prependProtocol(grpcExport.Endpoint, "dns://"),
		)
	pipeline = addInsecureFlagIfNecessary(pipeline, grpcExport.Endpoint)
	pipeline = appendHeadersToCollectorLogSelfMonitoringPipelineString(pipeline, grpcExport.Headers)
	pipeline += "\n"
	return pipeline
}

func convertHttpExportConfigurationToCollectorLogSelfMonitoringPipelineString(
	prelude string,
	httpExport *dash0common.HttpConfiguration,
) string {
	encoding := "protobuf"
	if httpExport.Encoding == dash0common.Json {
		encoding = "json"
	}
	pipeline := prelude +
		fmt.Sprintf(
			`
                protocol: http/%s
                endpoint: %s`,
			encoding,
			httpExport.Endpoint,
		)
	pipeline = appendHeadersToCollectorLogSelfMonitoringPipelineString(pipeline, httpExport.Headers)
	pipeline += "\n"
	return pipeline
}

func appendHeadersToCollectorLogSelfMonitoringPipelineString(pipeline string, headers []dash0common.Header) string {
	if len(headers) > 0 {
		pipeline += `
                headers:`
	}
	for _, header := range headers {
		if header.Name != "" {
			pipeline += fmt.Sprintf(
				`
                  %s: "%s"`,
				header.Name,
				header.Value,
			)
		}
	}
	return pipeline
}

func addInsecureFlagIfNecessary(pipeline string, endpoint string) string {
	endpointNormalized := strings.ToLower(endpoint)
	hasNonTlsPrefix := strings.HasPrefix(endpointNormalized, "http://")
	if hasNonTlsPrefix {
		return pipeline + `
                insecure: true`
	}
	return pipeline
}

// getAuthTokenForDash0Export takes a Dash0 export configuration and returns the configured token by either
// resolving the provided secretRef or returning the token literal.
func GetAuthTokenForDash0Export(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string, // we always look up the auth secrets in the operator namespace
	dash0Export dash0common.Dash0Configuration,
	logger logr.Logger,
) (*string, error) {
	if dash0Export.Authorization.SecretRef != nil {
		// The operator configuration resource uses a secret ref to provide the Dash0 auth token, exchange the secret
		// ref for an actual token and distribute the token value to all clients that need an auth token.
		token, err := ExchangeSecretRefForToken(
			ctx,
			k8sClient,
			operatorNamespace,
			dash0Export,
			&logger,
		)
		if err != nil {
			logger.Error(err, "cannot exchange secret ref for token")
			return nil, err
		} else {
			return token, nil
		}
	} else if dash0Export.Authorization.Token != nil &&
		*dash0Export.Authorization.Token != "" {
		// The operator configuration resource uses a token literal to provide the Dash0 auth token
		return dash0Export.Authorization.Token, nil
	}
	// The operator configuration resource neither has a secret ref nor a token literal, remove the auth token from all
	// clients. This case should not happen since we only call this func if HasDash0ApiAccessConfigured returns true.
	return nil, errors.New("authorization has neither secretRef nor token literal")
}

func ExchangeSecretRefForToken(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string, // we always look up the auth secrets in the operator namespace
	dash0Config dash0common.Dash0Configuration,
	logger *logr.Logger,
) (*string, error) {
	if dash0Config.Authorization.SecretRef == nil {
		return nil, fmt.Errorf("dash0Config has no secret ref")
	}
	secretRef := dash0Config.Authorization.SecretRef
	var dash0AuthTokenSecret corev1.Secret
	if err := k8sClient.Get(
		ctx,
		client.ObjectKey{
			Name:      secretRef.Name,
			Namespace: operatorNamespace,
		},
		&dash0AuthTokenSecret,
	); err != nil {
		msg := fmt.Sprintf(
			"failed to fetch secret with name %s in namespace %s for Dash0 self-monitoring/API access",
			secretRef.Name,
			operatorNamespace,
		)
		logger.Error(err, msg)
		return nil, fmt.Errorf(msg+": %w", err)
	} else {
		rawToken, hasToken := dash0AuthTokenSecret.Data[secretRef.Key]
		if !hasToken || rawToken == nil || len(rawToken) == 0 {
			err = fmt.Errorf(
				"secret \"%s/%s\" does not contain key \"%s\"",
				operatorNamespace,
				secretRef.Name,
				secretRef.Key,
			)
			logger.Error(err, "secret does not contain the expected key")
			return nil, err
		}
		decodedToken := string(rawToken)
		return &decodedToken, nil
	}
}
