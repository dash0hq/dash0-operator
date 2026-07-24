// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/exporters"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

type OtlpProtocol string

type SelfMonitoringConfiguration struct {
	SelfMonitoringEnabled bool
	Export                dash0common.Export
	// Token holds the resolved Dash0 auth token (in case of a Dash0 export)
	Token *string
	// ResolvedSecretHeaderValues holds resolved secret values for HTTP/gRPC headers (in case of a plain HTTP or gRPC
	// export with secret-backed headers)
	ResolvedSecretHeaderValues map[string]string
}

// RedactedForLogging returns a copy safe to log: the resolved token, the literal export token, header values and
// resolved secret header values are replaced with the redaction marker.
func (c SelfMonitoringConfiguration) RedactedForLogging() SelfMonitoringConfiguration {
	redacted := c
	redacted.Export = *c.Export.DeepCopy()
	redacted.Export.Redact()
	if c.Token != nil && len(*c.Token) > 0 {
		redactedToken := dash0common.RedactedValue
		redacted.Token = &redactedToken
	}
	if len(c.ResolvedSecretHeaderValues) > 0 {
		redactedHeaders := make(map[string]string, len(c.ResolvedSecretHeaderValues))
		for k := range c.ResolvedSecretHeaderValues {
			redactedHeaders[k] = dash0common.RedactedValue
		}
		redacted.ResolvedSecretHeaderValues = redactedHeaders
	}
	return redacted
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

	// selfMonitoringHeaderSecretNameSuffix is the nameSuffix passed to exporters.HeaderSecretEnvVarName when deriving
	// the names of the environment variables that back secret-sourced self-monitoring header values in the collector
	// pods. It is distinct from the suffixes used for the data-pipeline exporters ("DEFAULT_N", "NS/..._N"), so the
	// generated names never collide with those.
	selfMonitoringHeaderSecretNameSuffix = "SELF_MONITORING"
)

// selfMonitoringExportNameSuffix is the exporter name suffix used when deriving secret-backed header environment
// variable names for self-monitoring. Self-monitoring always sends telemetry via the operator configuration's first
// (default) export, hence exporters.NameSuffixDefault with index 0, matching the suffix the exporters section is
// rendered with, so the ${env:...} references rendered here resolve to the environment variables actually injected
// into the collector pod.
var selfMonitoringExportNameSuffix = exporters.NameSuffixDefault(0)

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
	logger logd.Logger,
) (SelfMonitoringConfiguration, error) {
	// Maintenance note: ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration is called from
	// operator_configuration_controller.go#Reconcile to create the in-process OTel SDK config for the operator manager,
	// and also from otelcol_resources.go#CreateOrUpdateOpenTelemetryCollectorResources to create the OTel SDK config for
	// managed pods/containers. Secrets (the Dash0 Auth token and arbitrary secret-backed header values for plain
	// HTTP/gRPC exports) need to be handled differently, depending on the code path.
	if resource == nil {
		return SelfMonitoringConfiguration{}, nil
	}

	selfMonitoringIsEnabled := pointers.ReadBoolPointerWithDefault(resource.Spec.SelfMonitoring.Enabled, true)
	if !selfMonitoringIsEnabled {
		return SelfMonitoringConfiguration{}, nil
	}

	if !resource.HasExportsConfigured() {
		logger.Warn(
			"Invalid configuration of Dash0OperatorConfiguration resource: Self-monitoring is enabled " +
				"but no export configuration is set. Self-monitoring telemetry will not be sent.",
		)
		return SelfMonitoringConfiguration{}, nil
	}

	// for self-monitoring we only send telemetry to a single backend
	export := resource.EffectiveExports()[0]
	if export.Dash0 != nil {
		token, err := GetAuthTokenForDash0Export(ctx, k8sClient, operatorNamespace, *export.Dash0, logger)
		if err != nil || token == nil {
			logger.Warn(
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
			ctx,
			k8sClient,
			operatorNamespace,
			&export,
			selfMonitoringIsEnabled,
			logger,
		)
	}
	if export.Http != nil {
		return convertResourceToHttpExportConfiguration(
			ctx,
			k8sClient,
			operatorNamespace,
			&export,
			selfMonitoringIsEnabled,
			logger,
		)
	}
	return SelfMonitoringConfiguration{},
		fmt.Errorf("no export configuration for self-monitoring has been provided, no self-monitoring telemetry will be sent")
}

func convertResourceToDash0ExportConfiguration(
	export *dash0common.Export,
	token *string,
	selfMonitoringEnabled bool,
	logger logd.Logger,
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
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	export *dash0common.Export,
	selfMonitoringEnabled bool,
	logger logd.Logger,
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
	resolvedSecretHeaderValues, err := resolveSelfMonitoringHeaderSecrets(
		ctx,
		k8sClient,
		operatorNamespace,
		grpcExport.Headers,
		logger,
	)
	if err != nil {
		logger.Warn(
			"Self-monitoring is enabled but a header value sourced from a secret could not be resolved. " +
				"Self-monitoring telemetry will not be sent.",
		)
		return SelfMonitoringConfiguration{}, nil
	}
	return SelfMonitoringConfiguration{
		SelfMonitoringEnabled: selfMonitoringEnabled,
		Export: dash0common.Export{
			Grpc: &dash0common.GrpcConfiguration{
				Endpoint: grpcExport.Endpoint,
				Headers:  grpcExport.Headers,
			},
		},
		ResolvedSecretHeaderValues: resolvedSecretHeaderValues,
	}, nil
}

func convertResourceToHttpExportConfiguration(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	export *dash0common.Export,
	selfMonitoringEnabled bool,
	logger logd.Logger,
) (SelfMonitoringConfiguration, error) {
	httpExport := export.Http
	if httpExport.Encoding == dash0common.Json {
		return SelfMonitoringConfiguration{
			SelfMonitoringEnabled: false,
		}, fmt.Errorf("using an HTTP exporter with JSON encoding self-monitoring is not supported")
	}
	resolvedSecretHeaderValues, err := resolveSelfMonitoringHeaderSecrets(ctx, k8sClient, operatorNamespace, httpExport.Headers, logger)
	if err != nil {
		logger.Warn(
			"Self-monitoring is enabled but a header value sourced from a secret could not be resolved. " +
				"Self-monitoring telemetry will not be sent.",
		)
		return SelfMonitoringConfiguration{}, nil
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
		ResolvedSecretHeaderValues: resolvedSecretHeaderValues,
	}, nil
}

// resolveSelfMonitoringHeaderSecrets returns a map with every secret-backed header value (valueFrom.secretKeyRef)
// resolved to its literal value, fetched from the referenced secret in the operator namespace.
func resolveSelfMonitoringHeaderSecrets(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	headers []dash0common.Header,
	logger logd.Logger,
) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	resolved := make(map[string]string, len(headers))
	for _, header := range headers {
		if header.ValueFrom == nil || header.ValueFrom.SecretKeyRef == nil {
			continue
		}
		value, err := getSecretValue(
			ctx,
			k8sClient,
			operatorNamespace,
			header.ValueFrom.SecretKeyRef,
			fmt.Sprintf("to resolve the value for header \"%s\"", header.Name),
			logger,
		)
		if err != nil {
			return nil, err
		}
		resolved[header.Name] = value
	}
	if len(resolved) == 0 {
		return nil, nil
	}
	return resolved, nil
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

// enableSelfMonitoringInCollectorContainer is called for all collector containers (e.g. configuration-reloader,
// filelog-offset-sync etc.) to set the required environment variables for the Go OTel SDK.
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
		// Remove stale OTEL_EXPORTER_OTLP_HEADERS env var if it is set.
		if headersEnvVarIdx >= 0 {
			container.Env =
				slices.Delete(container.Env, headersEnvVarIdx, headersEnvVarIdx+1)
		}
		return
	}

	// Set OTEL_EXPORTER_OTLP_HEADERS on the container depending on the configured headers in the export.
	headersValue, secretHeaderEnvVars :=
		convertHeadersToEnvVarValue(headers, headerSecretEnvVarProtocol(selfMonitoringExport))

	// Header values sourced from a Kubernetes secret are injected as dedicated environment variables (backed by the
	// secret) and referenced from OTEL_EXPORTER_OTLP_HEADERS via $(...). The collector resources do not use
	// resolved plaintext secrets (see otelcol_resources.go), we rely on Kubernetes dependent environment variable
	// expansion to resolve them at container startup. The referenced env vars have to be defined before
	// OTEL_EXPORTER_OTLP_HEADERS, hence we prepend them to the container's env list. See
	// https://kubernetes.io/docs/tasks/inject-data-application/define-interdependent-environment-variables/.
	for _, secretHeaderEnvVar := range secretHeaderEnvVars {
		prependOrUpdateEnvVar(container, secretHeaderEnvVar)
	}
	// Prepending secret env vars may have shifted the index of an existing OTEL_EXPORTER_OTLP_HEADERS env var.
	headersEnvVarIdx = slices.IndexFunc(container.Env, matchOtelExporterOtlpHeadersEnvVar)

	newOtelExporterOtlpHeadersEnvVar := corev1.EnvVar{
		Name:  otelExporterOtlpHeadersEnvVarName,
		Value: headersValue,
	}
	if headersEnvVarIdx >= 0 {
		// update the existing environment variable
		container.Env[headersEnvVarIdx] = newOtelExporterOtlpHeadersEnvVar
	} else {
		// insert a new environment variable right after OTEL_EXPORTER_OTLP_ENDPOINT
		endpointEnvVarIdx := slices.IndexFunc(container.Env, matchOtelExporterOtlpEndpointEnvVar)
		container.Env = slices.Insert(container.Env, endpointEnvVarIdx+1, newOtelExporterOtlpHeadersEnvVar)
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

// convertHeadersToEnvVarValue renders the value for the OTEL_EXPORTER_OTLP_HEADERS environment variable from the given
// headers. Headers with a literal value are rendered as "name=value". Headers whose value is sourced from a Kubernetes
// secret (valueFrom.secretKeyRef) are rendered as "name=$(ENV_VAR_NAME)", referencing a dedicated environment variable
// backed by the secret; those environment variables are returned so the caller can inject them into the container.
// Kubernetes expands the $(ENV_VAR_NAME) references at container startup (the referenced env vars must be defined
// earlier in the container's env list), so the collector's OTel SDK receives the resolved header values without the
// operator ever placing the plaintext secret into the collector resources.
func convertHeadersToEnvVarValue(headers []dash0common.Header, protocol string) (string, []corev1.EnvVar) {
	keyValuePairs := make([]string, 0, len(headers))
	var secretHeaderEnvVars []corev1.EnvVar
	for i, header := range headers {
		if header.ValueFrom != nil && header.ValueFrom.SecretKeyRef != nil {
			envVarName := exporters.HeaderSecretEnvVarName(protocol, selfMonitoringHeaderSecretNameSuffix, i)
			secretHeaderEnvVar, err := util.CreateEnvVarForSecretKeySelector(header.ValueFrom.SecretKeyRef, envVarName)
			if err != nil {
				// Upstream validation guarantees a secret name and key are present; skip a malformed header defensively
				// rather than emitting a broken reference.
				continue
			}
			secretHeaderEnvVars = append(secretHeaderEnvVars, secretHeaderEnvVar)
			keyValuePairs = append(keyValuePairs, fmt.Sprintf("%s=$(%s)", header.Name, envVarName))
			continue
		}
		keyValuePairs = append(keyValuePairs, fmt.Sprintf("%v=%v", header.Name, header.Value))
	}
	return strings.Join(keyValuePairs, ","), secretHeaderEnvVars
}

// headerSecretEnvVarProtocol returns the protocol identifier ("GRPC" or "HTTP") used when deriving the names of the
// environment variables that back secret-sourced header values.
func headerSecretEnvVarProtocol(selfMonitoringExport dash0common.Export) string {
	if selfMonitoringExport.Http != nil {
		return "HTTP"
	} else if selfMonitoringExport.Grpc != nil {
		return "GRPC"
	}
	// case: selfMonitoringExport.Dash0 != nil: A Dash0 export never carries secret-backed headers (its authorization
	// token is handled separately via CollectorSelfMonitoringAuthTokenEnvVarName), so the returned value is irrelevant
	// for that case.
	return "DASH0"
}

// prependOrUpdateEnvVar updates the environment variable in place if it already exists, otherwise it inserts it at the
// front of the container's env list. Prepending ensures the env var is defined before OTEL_EXPORTER_OTLP_HEADERS, which
// is required for the $(...) references in that variable to be expanded by Kubernetes.
func prependOrUpdateEnvVar(container *corev1.Container, envVar corev1.EnvVar) {
	idx := slices.IndexFunc(container.Env, matchEnvVar(envVar.Name))
	if idx >= 0 {
		container.Env[idx] = envVar
	} else {
		container.Env = slices.Insert(container.Env, 0, envVar)
	}
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
                  - name: %s
                    value: "Bearer ${env:SELF_MONITORING_AUTH_TOKEN}"`,
		util.AuthorizationHeaderName,
	)
	if dash0Export.Dataset != "" && dash0Export.Dataset != util.DatasetDefault {
		pipeline += fmt.Sprintf(
			`
                  - name: %s
                    value: "%s"
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
	pipeline = appendHeadersToCollectorLogSelfMonitoringPipelineString(pipeline, grpcExport.Headers, "GRPC")
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
	pipeline = appendHeadersToCollectorLogSelfMonitoringPipelineString(pipeline, httpExport.Headers, "HTTP")
	pipeline += "\n"
	return pipeline
}

// appendHeadersToCollectorLogSelfMonitoringPipelineString renders the given headers into the self-monitoring pipeline.
// Headers with a literal value are rendered as-is. Headers whose value is sourced from a Kubernetes secret
// (valueFrom.secretKeyRef) are rendered as a ${env:...} reference; the referenced environment variable is injected into
// the collector pod by the exporters section (see otelcolresources.resolveExporterHeaders), so the name must be derived
// identically via util.HeaderSecretEnvVarName. The protocol ("GRPC" or "HTTP") is part of that name. The header index
// must match the exporters section, which iterates the same headers slice, so we iterate the full slice by index here
// as well even though headers without a name are skipped.
func appendHeadersToCollectorLogSelfMonitoringPipelineString(
	pipeline string,
	headers []dash0common.Header,
	protocol string,
) string {
	if len(headers) > 0 {
		pipeline += `
                headers:`
	}
	for i, header := range headers {
		if header.Name == "" {
			continue
		}
		value := header.Value
		if header.ValueFrom != nil && header.ValueFrom.SecretKeyRef != nil {
			value = fmt.Sprintf(
				"${env:%s}",
				exporters.HeaderSecretEnvVarName(protocol, selfMonitoringExportNameSuffix, i),
			)
		}
		pipeline += fmt.Sprintf(
			`
                  - name: %s
                    value: "%s"`,
			header.Name,
			value,
		)
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
	logger logd.Logger,
) (*string, error) {
	if dash0Export.Authorization.SecretRef != nil {
		// The operator configuration resource uses a secret ref to provide the Dash0 auth token, exchange the secret
		// ref for an actual token and distribute the token value to all clients that need an auth token.
		token, err := ExchangeSecretRefForToken(
			ctx,
			k8sClient,
			operatorNamespace,
			dash0Export,
			logger,
		)
		if err != nil {
			logger.Error(err, "cannot exchange secret ref for token")
			return nil, err
		}
		return token, nil
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
	logger logd.Logger,
) (*string, error) {
	if dash0Config.Authorization.SecretRef == nil {
		return nil, fmt.Errorf("dash0Config has no secret ref")
	}
	secretRef := dash0Config.Authorization.SecretRef
	token, err := getSecretValue(
		ctx,
		k8sClient,
		operatorNamespace,
		&dash0common.SecretKeySelector{
			Name: secretRef.Name,
			Key:  secretRef.Key,
		},
		"for Dash0 self-monitoring/API access",
		logger,
	)
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// getSecretValue fetches the value stored under secretKeyRef.Key in the secret secretKeyRef.Name in the operator
// namespace. It is used to resolve header values that are sourced from a Kubernetes secret for the operator manager's
// in-process self-monitoring OTel SDK, which (unlike the collector) cannot use ${env:...} references.
func getSecretValue(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	secretRef *dash0common.SecretKeySelector,
	purpose string,
	logger logd.Logger,
) (string, error) {
	var secret corev1.Secret
	if err := k8sClient.Get(
		ctx,
		client.ObjectKey{Name: secretRef.Name, Namespace: operatorNamespace},
		&secret,
	); err != nil {
		msg := fmt.Sprintf(
			"failed to fetch secret with name %s in namespace %s %s",
			secretRef.Name,
			operatorNamespace,
			purpose,
		)
		logger.Error(err, msg)
		return "", fmt.Errorf(msg+": %w", err)
	}
	rawValue, hasKey := secret.Data[secretRef.Key]
	if !hasKey || len(rawValue) == 0 {
		err := fmt.Errorf(
			"secret \"%s/%s\" does not contain key \"%s\"",
			operatorNamespace,
			secretRef.Name,
			secretRef.Key,
		)
		logger.Error(err, "secret does not contain the expected key")
		return "", err
	}
	return string(rawValue), nil
}
