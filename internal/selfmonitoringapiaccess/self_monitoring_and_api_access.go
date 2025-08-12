// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type AuthTokenClient interface {
	SetAuthToken(context.Context, string, *logr.Logger)
	RemoveAuthToken(context.Context, *logr.Logger)
}

type OtlpProtocol string

type SelfMonitoringConfiguration struct {
	SelfMonitoringEnabled bool
	Export                dash0v1alpha1.Export
}

type EndpointAndHeaders struct {
	Endpoint string
	Protocol string
	Headers  []dash0v1alpha1.Header
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

	collectorLogSelfMonitoringPrelude = `
    logs:
      processors:
        - batch:
            exporter:
              otlp:`
)

func (c *SelfMonitoringConfiguration) HasDash0ApiAccessConfigured() bool {
	return c.Export.Dash0 != nil &&
		c.Export.Dash0.ApiEndpoint != "" &&
		(c.Export.Dash0.Authorization.Token != nil || c.Export.Dash0.Authorization.SecretRef != nil)
}

func (c *SelfMonitoringConfiguration) GetDash0Authorization() dash0v1alpha1.Authorization {
	return c.Export.Dash0.Authorization
}

func ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
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

	export := resource.Spec.Export
	if export == nil {
		logger.Info("Invalid configuration of Dash0OperatorConfiguration resource: Self-monitoring is enabled " +
			"but no export configuration is set. Self-monitoring telemetry will not be sent.")
		return SelfMonitoringConfiguration{}, nil
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
	return SelfMonitoringConfiguration{},
		fmt.Errorf("no export configuration for self-monitoring has been provided, no self-monitoring telemetry will be sent")
}

func convertResourceToDash0ExportConfiguration(
	export *dash0v1alpha1.Export,
	selfMonitoringEnabled bool,
	logger *logr.Logger,
) (SelfMonitoringConfiguration, error) {
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
	return SelfMonitoringConfiguration{
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
) (SelfMonitoringConfiguration, error) {
	if export.Http != nil {
		logger.Info(
			fmt.Sprintf(
				"Ignoring http export configuration (%s) for self-monitoring telemetry, will send to the configured gRPC export.",
				export.Http.Endpoint))
	}

	grpcExport := export.Grpc
	return SelfMonitoringConfiguration{
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
) (SelfMonitoringConfiguration, error) {
	httpExport := export.Http
	if httpExport.Encoding == dash0v1alpha1.Json {
		return SelfMonitoringConfiguration{
			SelfMonitoringEnabled: false,
		}, fmt.Errorf("using an HTTP exporter with JSON encoding self-monitoring is not supported")
	}
	return SelfMonitoringConfiguration{
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
	updateOrAppendEnvVar(container, util.OtelResourceAttributesEnvVarName,
		fmt.Sprintf(
			"service.namespace=dash0-operator,service.name=%s,service.version=%s",
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

func convertHeadersToEnvVarValue(headers []dash0v1alpha1.Header) string {
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
func ConvertExportConfigurationToEnvVarSettings(selfMonitoringExport dash0v1alpha1.Export) EndpointAndHeaders {
	if selfMonitoringExport.Dash0 != nil {
		dash0Export := selfMonitoringExport.Dash0
		headers := []dash0v1alpha1.Header{{
			Name:  util.AuthorizationHeaderName,
			Value: selfMonitoringAuthHeaderValue,
		}}
		if dash0Export.Dataset != "" && dash0Export.Dataset != util.DatasetDefault {
			headers = append(headers, dash0v1alpha1.Header{
				Name:  util.Dash0DatasetHeaderName,
				Value: dash0Export.Dataset,
			})
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
		// if selfMonitoringExport.Http.Encoding == dash0v1alpha1.Json {
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

// ConvertExportConfigurationToCollectorLogSelfMonitoringPipelineString is used to create a snippet that can be added to
// the collector config maps for sending the collector's logs to a configured export.
func ConvertExportConfigurationToCollectorLogSelfMonitoringPipelineString(selfMonitoringConfiguration SelfMonitoringConfiguration) string {
	if !selfMonitoringConfiguration.SelfMonitoringEnabled {
		return ""
	}
	selfMonitoringExport := selfMonitoringConfiguration.Export
	if selfMonitoringExport.Dash0 != nil {
		return convertDash0ExportConfigurationToCollectorLogSelfMonitoringPipelineString(selfMonitoringExport.Dash0)
	} else if selfMonitoringExport.Grpc != nil {
		return convertGrpcExportConfigurationToCollectorLogSelfMonitoringPipelineString(selfMonitoringExport.Grpc)
	} else if selfMonitoringExport.Http != nil {
		return convertHttpExportConfigurationToCollectorLogSelfMonitoringPipelineString(selfMonitoringExport.Http)
	}
	return ""
}

func convertDash0ExportConfigurationToCollectorLogSelfMonitoringPipelineString(dash0Export *dash0v1alpha1.Dash0Configuration) string {
	pipeline := collectorLogSelfMonitoringPrelude +
		fmt.Sprintf(`
                protocol: grpc
                endpoint: %s`, prependProtocol(dash0Export.Endpoint, "https://"))
	pipeline = addInsecureFlagIfNecessary(pipeline, dash0Export.Endpoint)
	pipeline += fmt.Sprintf(`
                headers:
                  %s: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"`,
		util.AuthorizationHeaderName,
	)
	if dash0Export.Dataset != "" && dash0Export.Dataset != util.DatasetDefault {
		pipeline += fmt.Sprintf(`
                  %s: "%s"
`, util.Dash0DatasetHeaderName, dash0Export.Dataset)
	} else {
		// Append final newline, which is deliberately not included in the snippet that includes protocol and
		// endpoint.
		pipeline += "\n"
	}
	return pipeline
}

func convertGrpcExportConfigurationToCollectorLogSelfMonitoringPipelineString(grpcExport *dash0v1alpha1.GrpcConfiguration) string {
	pipeline := collectorLogSelfMonitoringPrelude +
		fmt.Sprintf(`
                protocol: grpc
                endpoint: %s`,
			prependProtocol(grpcExport.Endpoint, "dns://"),
		)
	pipeline = addInsecureFlagIfNecessary(pipeline, grpcExport.Endpoint)
	pipeline = appendHeadersToCollectorLogSelfMonitoringPipelineString(pipeline, grpcExport.Headers)
	pipeline += "\n"
	return pipeline
}

func convertHttpExportConfigurationToCollectorLogSelfMonitoringPipelineString(httpExport *dash0v1alpha1.HttpConfiguration) string {
	encoding := "protobuf"
	if httpExport.Encoding == dash0v1alpha1.Json {
		encoding = "json"
	}
	pipeline := collectorLogSelfMonitoringPrelude +
		fmt.Sprintf(`
                protocol: http/%s
                endpoint: %s`,
			encoding,
			httpExport.Endpoint,
		)
	pipeline = appendHeadersToCollectorLogSelfMonitoringPipelineString(pipeline, httpExport.Headers)
	pipeline += "\n"
	return pipeline
}

func appendHeadersToCollectorLogSelfMonitoringPipelineString(pipeline string, headers []dash0v1alpha1.Header) string {
	if len(headers) > 0 {
		pipeline += `
                headers:`
	}
	for _, header := range headers {
		if header.Name != "" {
			pipeline += fmt.Sprintf(`
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

func ExchangeSecretRefForToken(
	ctx context.Context,
	k8sClient client.Client,
	authTokenClients []AuthTokenClient,
	operatorNamespace string,
	operatorConfiguration *dash0v1alpha1.Dash0OperatorConfiguration,
	logger *logr.Logger,
) error {
	if operatorConfiguration == nil {
		removeToken(ctx, authTokenClients, logger)
		return fmt.Errorf("operatorConfiguration is nil")
	}
	if operatorConfiguration.Spec.Export == nil {
		removeToken(ctx, authTokenClients, logger)
		return fmt.Errorf("operatorConfiguration has no export")
	}
	if operatorConfiguration.Spec.Export.Dash0 == nil {
		removeToken(ctx, authTokenClients, logger)
		return fmt.Errorf("operatorConfiguration has no Dash0 export")
	}
	if operatorConfiguration.Spec.Export.Dash0.Authorization.SecretRef == nil {
		removeToken(ctx, authTokenClients, logger)
		return fmt.Errorf("operatorConfiguration has no secret ref")
	}
	secretRef := operatorConfiguration.Spec.Export.Dash0.Authorization.SecretRef
	var dash0AuthTokenSecret corev1.Secret
	if err := k8sClient.Get(
		ctx,
		client.ObjectKey{
			Name:      secretRef.Name,
			Namespace: operatorNamespace,
		},
		&dash0AuthTokenSecret,
	); err != nil {
		removeToken(ctx, authTokenClients, logger)
		msg := fmt.Sprintf("failed to fetch secret with name %s in namespace %s for Dash0 self-monitoring/API access",
			secretRef.Name,
			operatorNamespace,
		)
		logger.Error(err, msg)
		return fmt.Errorf(msg+": %w", err)
	} else {
		rawToken, hasToken := dash0AuthTokenSecret.Data[secretRef.Key]
		if !hasToken || rawToken == nil || len(rawToken) == 0 {
			removeToken(ctx, authTokenClients, logger)
			err = fmt.Errorf("secret \"%s/%s\" does not contain key \"%s\"",
				operatorNamespace,
				secretRef.Name,
				secretRef.Key)
			logger.Error(err, "secret does not contain the expected key")
			return err
		}
		decodedToken := string(rawToken)
		for _, authTokenClient := range authTokenClients {
			authTokenClient.SetAuthToken(ctx, decodedToken, logger)
		}
		return nil
	}
}

func removeToken(ctx context.Context, authTokenClients []AuthTokenClient, logger *logr.Logger) {
	for _, authTokenClient := range authTokenClients {
		authTokenClient.RemoveAuthToken(ctx, logger)
	}
}
