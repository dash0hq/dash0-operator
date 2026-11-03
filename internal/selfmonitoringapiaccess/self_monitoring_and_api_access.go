// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package selfmonitoringapiaccess

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
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

	// collectorServiceName is the service.name of the OpenTelemetry collector's internal telemetry. The Go SDK sidecar
	// containers in collector pods report with the same service.name.
	collectorServiceName = "dash0-operator-collector"

	// selfMonitoringHeaderSecretNameSuffix is the nameSuffix passed to exporters.HeaderSecretEnvVarName when deriving
	// the names of the environment variables that back secret-sourced self-monitoring header values in the collector
	// pods. It is distinct from the suffixes used for the data-pipeline exporters ("DEFAULT_N", "NS/..._N"), so the
	// generated names never collide with those.
	selfMonitoringHeaderSecretNameSuffix = "SELF_MONITORING"

	grpcDnsSchemeWithoutAuthority = "dns:///"

	grpcUnixSocketReason = "the unix sockets of an export are not reachable from the operator manager pod, and the " +
		"self-monitoring telemetry of the OpenTelemetry collectors does not support unix sockets"
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

	// grpcTargetSchemeRegex matches gRPC targets with a scheme, including those without slashes (e.g. unix:path), but not
	// host:port. Like gRPC, it treats the schemes of the default gRPC resolvers as scheme even if a digit follows (e.g.
	// dns:10.0.0.1:4317).
	grpcTargetSchemeRegex = regexp.MustCompile(
		`^((?i:dns|passthrough|unix|unix-abstract):|[a-zA-Z][a-zA-Z0-9+.-]*:([^0-9]|$))`,
	)

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
	// and also from (among others) otelcol_resources.go#CreateOrUpdateOpenTelemetryCollectorResources to create the OTel
	// SDK config for managed pods/containers. Secrets (the Dash0 Auth token and arbitrary secret-backed header values for
	// plain HTTP/gRPC exports) need to be handled differently, depending on the code path.
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
	if address, hasMissingSlash := addressOfDnsTargetWithMissingSlash(grpcExport.Endpoint); hasMissingSlash {
		logger.ErrorTelemetryCollectionIssue(
			fmt.Errorf("invalid gRPC export endpoint %s", grpcExport.Endpoint),
			fmt.Sprintf(
				"The gRPC export endpoint %s is not a valid gRPC target: gRPC interprets %s as the DNS server to use, "+
					"and the target address is missing. Probably a slash is missing, use dns:///%s instead. Exporting "+
					"telemetry to this endpoint will fail, and the OpenTelemetry collectors and other operator "+
					"components will not send self-monitoring telemetry. The operator manager sends its own logs and "+
					"metrics to %s instead, as it would for dns:///%s.",
				grpcExport.Endpoint,
				address,
				address,
				operatorManagerGrpcEndpoint(grpcExport),
				address,
			),
		)
	} else if hasUnsupportedGrpcSelfMonitoringScheme(grpcExport.Endpoint) {
		if reason := unusableGrpcTargetReason(grpcExport.Endpoint); reason == "" {
			logger.Warn(
				fmt.Sprintf(
					"The gRPC export endpoint %s has a format that only the operator manager supports for "+
						"self-monitoring telemetry. The self-monitoring telemetry of the OpenTelemetry collectors and "+
						"other operator components will probably not be sent. Use an endpoint without scheme, or with "+
						"one of the schemes http://, https:// or dns:///.",
					grpcExport.Endpoint,
				),
			)
		} else {
			logger.Warn(
				fmt.Sprintf(
					"The gRPC export endpoint %s cannot be used for self-monitoring telemetry: %s. Self-monitoring "+
						"telemetry will not be sent. Use an endpoint without scheme, or with one of the schemes "+
						"http://, https:// or dns:///.",
					grpcExport.Endpoint,
					reason,
				),
			)
		}
	}
	// For unsupported endpoints (including a dns:// endpoint with a missing slash), the messages above already cover the
	// self-monitoring telemetry of the collectors and other components.
	if !hasUnsupportedGrpcSelfMonitoringScheme(grpcExport.Endpoint) {
		warnIfInsecureSkipVerifyIsIgnored(grpcExport.InsecureSkipVerify, grpcSelfMonitoringEndpoint(grpcExport), logger)
	}
	return SelfMonitoringConfiguration{
		SelfMonitoringEnabled: selfMonitoringEnabled,
		Export: dash0common.Export{
			Grpc: &dash0common.GrpcConfiguration{
				Endpoint:           grpcExport.Endpoint,
				Headers:            grpcExport.Headers,
				Insecure:           grpcExport.Insecure,
				InsecureSkipVerify: grpcExport.InsecureSkipVerify,
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
	warnIfInsecureSkipVerifyIsIgnored(httpExport.InsecureSkipVerify, httpExport.Endpoint, logger)
	return SelfMonitoringConfiguration{
		SelfMonitoringEnabled: selfMonitoringEnabled,
		Export: dash0common.Export{
			Http: &dash0common.HttpConfiguration{
				Endpoint:           httpExport.Endpoint,
				Headers:            httpExport.Headers,
				Encoding:           httpExport.Encoding,
				InsecureSkipVerify: httpExport.InsecureSkipVerify,
			},
		},
		ResolvedSecretHeaderValues: resolvedSecretHeaderValues,
	}, nil
}

// warnIfInsecureSkipVerifyIsIgnored logs a warning if the export skips certificate verification for an endpoint that is
// not http:// (the collector uses TLS for endpoints without scheme), since only the operator manager supports that for
// self-monitoring, but not the collector internal telemetry and the Go SDK configured via env vars.
func warnIfInsecureSkipVerifyIsIgnored(insecureSkipVerify *bool, selfMonitoringEndpoint string, logger logd.Logger) {
	if !strings.HasPrefix(strings.ToLower(selfMonitoringEndpoint), "http://") &&
		pointers.ReadBoolPointerWithDefault(insecureSkipVerify, false) {
		logger.Warn(
			fmt.Sprintf(
				"The export to %s sets insecureSkipVerify, which only the operator manager supports for "+
					"self-monitoring telemetry. The OpenTelemetry collectors and other operator components send their "+
					"self-monitoring telemetry with certificate verification, which will fail if the certificate of "+
					"the endpoint cannot be verified.",
				selfMonitoringEndpoint,
			),
		)
	}
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

// EnableSelfMonitoringInDaemonSet enables self-monitoring for all containers in the given DaemonSet. It sets the
// environment variables the OpenTelemetry Go SDK needs on all containers, with one exception - the container with the
// name otelCollectorContainerName (if this argument is provided) is assumed to run the OpenTelemetry collector and
// receives a different set of environment variables (since it does not use the OpenTelemetry Go SDK), see
// enableSelfMonitoringInContainers. Pass an empty otelCollectorContainerName for pod specs without a collector
// container.
func EnableSelfMonitoringInDaemonSet(
	daemonSet *appsv1.DaemonSet,
	selfMonitoringConfiguration SelfMonitoringConfiguration,
	operatorVersion string,
	developmentMode bool,
	otelCollectorContainerName string,
) error {
	return enableSelfMonitoringInPodSpec(
		&daemonSet.Spec.Template.Spec,
		selfMonitoringConfiguration,
		operatorVersion,
		developmentMode,
		otelCollectorContainerName,
	)
}

// EnableSelfMonitoringInDeployment enables self-monitoring for all containers in the given Deployment. It sets the
// environment variables the OpenTelemetry Go SDK needs on all containers, with one exception - the container with the
// name otelCollectorContainerName (if this argument is provided) is assumed to run the OpenTelemetry collector and
// receives a different set of environment variables (since it does not use the OpenTelemetry Go SDK), see
// enableSelfMonitoringInContainers. Pass an empty otelCollectorContainerName for pod specs without a collector
// container.
func EnableSelfMonitoringInDeployment(
	deployment *appsv1.Deployment,
	selfMonitoringConfiguration SelfMonitoringConfiguration,
	operatorVersion string,
	developmentMode bool,
	otelCollectorContainerName string,
) error {
	return enableSelfMonitoringInPodSpec(
		&deployment.Spec.Template.Spec,
		selfMonitoringConfiguration,
		operatorVersion,
		developmentMode,
		otelCollectorContainerName,
	)
}

// enableSelfMonitoringInPodSpec enables self-monitoring for all containers in the given podSpec. It sets the
// environment variables the OpenTelemetry Go SDK needs on all containers, with one exception - the container with the
// name otelCollectorContainerName (if this argument is provided) is assumed to run the OpenTelemetry collector and
// receives a different set of environment variables (since it does not use the OpenTelemetry Go SDK), see
// enableSelfMonitoringInContainers. Pass an empty otelCollectorContainerName for pod specs without a collector
// container.
func enableSelfMonitoringInPodSpec(
	podSpec *corev1.PodSpec,
	selfMonitoringConfiguration SelfMonitoringConfiguration,
	operatorVersion string,
	developmentMode bool,
	otelCollectorContainerName string,
) error {
	// Note: We do not instrument init containers, that is, we only pass podSpec.Containers but not
	// podSpec.InitContainers to enableSelfMonitoringInContainers. Init containers are short-lived. Trying to flush
	// all internal telemetry at the end of an init container process might lead to the init container running longer than
	// necessary. The alternative would be not flushing telemetry at the process end. This is also not very useful,
	// because we often would lose most or all of the init containers telemetry anyway.
	return enableSelfMonitoringInContainers(
		podSpec.Containers,
		selfMonitoringConfiguration,
		operatorVersion,
		developmentMode,
		otelCollectorContainerName,
	)
}

// enableSelfMonitoringInContainers enables self-monitoring for the given containers. The container named
// otelCollectorContainerName is assumed to run the OpenTelemetry collector, which configures its internal telemetry via
// the service::telemetry section of the collector configuration; it only needs the environment variable holding the
// auth token, which the rendered self-monitoring pipeline refers to via  ${env:SELF_MONITORING_AUTH_TOKEN}. All other
// containers are assumed to use the OpenTelemetry Go SDK, which is configured exclusively via environment variables.
// In a collector pod, they report with the collector's service.name, otherwise with their container name as
// service.name. Pass an empty otelCollectorContainerName for pod specs without a collector container.
func enableSelfMonitoringInContainers(
	containers []corev1.Container,
	selfMonitoringConfiguration SelfMonitoringConfiguration,
	operatorVersion string,
	developmentMode bool,
	otelCollectorContainerName string,
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

	for i, container := range containers {
		if otelCollectorContainerName != "" && container.Name == otelCollectorContainerName {
			enableSelfMonitoringInOpenTelemetryCollectorContainer(&container, authTokenEnvVar)
			containers[i] = container
			continue
		}
		serviceName := container.Name
		if otelCollectorContainerName != "" {
			serviceName = collectorServiceName
		}
		enableSelfMonitoringInOTelSdkContainer(
			&container,
			selfMonitoringExport,
			authTokenEnvVar,
			serviceName,
			operatorVersion,
			developmentMode,
		)
		containers[i] = container
	}

	return nil
}

// enableSelfMonitoringInOpenTelemetryCollectorContainer is called for the containers that run the
// opentelemetry-collector process, to set the environment variables that the self-monitoring configuration via
// pipelines.telemetry requires.
func enableSelfMonitoringInOpenTelemetryCollectorContainer(
	container *corev1.Container,
	authTokenEnvVar *corev1.EnvVar,
) {
	if authTokenEnvVar != nil {
		addAuthTokenToContainer(container, authTokenEnvVar)
	}
}

// enableSelfMonitoringInOTelSdkContainer is called for all containers that run a Go OTel SDK (the collector pods'
// sidecar containers configuration-reloader and filelog-offset-sync, and the agent0-connector), to set the environment
// variables the SDK requires. The service.* resource attributes are only provided via OTEL_RESOURCE_ATTRIBUTES, the
// containers do not set them on their own.
func enableSelfMonitoringInOTelSdkContainer(
	container *corev1.Container,
	selfMonitoringExport dash0common.Export,
	authTokenEnvVar *corev1.EnvVar,
	serviceName string,
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
			serviceName,
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
		if dash0Export.Dataset != "" {
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
			Endpoint: grpcSelfMonitoringEndpoint(selfMonitoringExport.Grpc),
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
	// The Go SDK with gRPC requires the endpoint with a protocol when setting it via OTEL_EXPORTER_OTLP_ENDPOINT.
	if !common.EndpointHasScheme(endpoint) {
		return defaultProtocol + endpoint
	}
	return endpoint
}

// grpcSelfMonitoringEndpoint renders the endpoint of a gRPC export for self-monitoring. The consumers (the collector's
// internal telemetry, the Go SDK via OTEL_EXPORTER_OTLP_ENDPOINT and the operator manager) only handle the schemes
// http:// (plaintext) and https:// (TLS) consistently, so endpoints without a scheme (or with dns:///) get one derived
// from Insecure.
func grpcSelfMonitoringEndpoint(grpcExport *dash0common.GrpcConfiguration) string {
	endpoint := grpcExport.Endpoint
	endpointLowerCase := strings.ToLower(endpoint)
	// gRPC reads the address after dns:/// as host[:port], even if it looks like a scheme (e.g. dns:///unix:4317).
	hasDnsSchemeWithoutAuthority := strings.HasPrefix(endpointLowerCase, grpcDnsSchemeWithoutAuthority)
	if hasDnsSchemeWithoutAuthority {
		endpoint = endpoint[len(grpcDnsSchemeWithoutAuthority):]
		endpointLowerCase = strings.ToLower(endpoint)
	}
	// The collector only recognizes the lower case schemes.
	for _, scheme := range []string{"http://", "https://"} {
		if strings.HasPrefix(endpointLowerCase, scheme) {
			return scheme + endpoint[len(scheme):]
		}
	}
	if !hasDnsSchemeWithoutAuthority && grpcTargetSchemeRegex.MatchString(endpoint) {
		return endpoint
	}
	if pointers.ReadBoolPointerWithDefault(grpcExport.Insecure, false) {
		return "http://" + endpoint
	}
	return "https://" + endpoint
}

// hasUnsupportedGrpcSelfMonitoringScheme returns true if grpcSelfMonitoringEndpoint cannot convert the endpoint to an
// http:// or https:// URL, e.g. for dns://dns-server/host:port or unix:///path.
func hasUnsupportedGrpcSelfMonitoringScheme(endpoint string) bool {
	rendered := strings.ToLower(grpcSelfMonitoringEndpoint(&dash0common.GrpcConfiguration{Endpoint: endpoint}))
	return !strings.HasPrefix(rendered, "http://") && !strings.HasPrefix(rendered, "https://")
}

// unusableGrpcTargetReason returns why the operator manager cannot use the gRPC target, or an empty string if it can.
// The operator manager passes endpoints without http:// or https:// to gRPC unchanged.
func unusableGrpcTargetReason(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return dnsFallbackReason(endpoint, "it cannot be parsed as a gRPC target")
	}
	// same as the endpoint of a gRPC resolver target
	address := u.Path
	if address == "" {
		address = u.Opaque
	}
	address = strings.TrimPrefix(address, "/")
	switch u.Scheme {
	case "dns":
		return dnsTargetReason(address, u.Host)
	case "passthrough":
		if address == "" {
			return "the target address is missing"
		}
		// gRPC dials passthrough addresses starting with unix: as unix sockets
		if strings.HasPrefix(address, "unix:") {
			return grpcUnixSocketReason
		}
		if _, port, err := net.SplitHostPort(address); err != nil || port == "" {
			return fmt.Sprintf("the target address %s is not of the form host:port, which passthrough requires", address)
		} else if _, err = net.LookupPort("tcp", port); err != nil {
			return fmt.Sprintf("the port %s of the target address %s is unknown", port, address)
		}
		return ""
	case "unix", "unix-abstract":
		return grpcUnixSocketReason
	default:
		// e.g. host:https, which url.Parse reads as scheme host
		return dnsFallbackReason(
			endpoint,
			fmt.Sprintf("%s is not a scheme of the gRPC resolvers dns, passthrough, unix or unix-abstract", u.Scheme),
		)
	}
}

// dnsFallbackReason mirrors gRPC, which passes a target that it cannot parse or that has an unregistered scheme to the
// dns resolver as dns:///<target>. It returns an empty string if the dns resolver can use the target, otherwise reason.
func dnsFallbackReason(endpoint string, reason string) string {
	u, err := url.Parse(grpcDnsSchemeWithoutAuthority + endpoint)
	if err != nil {
		return "it cannot be parsed as a gRPC target"
	}
	if dnsTargetReason(strings.TrimPrefix(u.Path, "/"), "") == "" {
		return ""
	}
	return reason
}

// dnsTargetReason returns why the gRPC dns resolver cannot use the target address and DNS server, or an empty string
// if it can. Like the resolver, it only uses the DNS server for host names, not for IP addresses.
func dnsTargetReason(address string, dnsServer string) string {
	if address == "" {
		return "the target address is missing"
	}
	host, port, ok := splitDnsTarget(address, "443")
	if !ok {
		return fmt.Sprintf("the target address %s is not of the form host[:port]", address)
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return fmt.Sprintf("the port %s of the target address %s is unknown", port, address)
	}
	if _, err := netip.ParseAddr(host); err != nil && dnsServer != "" {
		if _, _, ok = splitDnsTarget(dnsServer, "53"); !ok {
			return fmt.Sprintf("the DNS server %s is not of the form host[:port]", dnsServer)
		}
	}
	return ""
}

// splitDnsTarget mirrors the address parsing of the gRPC dns resolver, which adds the default port if there is none.
func splitDnsTarget(target string, defaultPort string) (string, string, bool) {
	if _, err := netip.ParseAddr(target); err == nil {
		return target, defaultPort, true
	}
	if host, port, err := net.SplitHostPort(target); err == nil {
		return host, port, port != ""
	}
	host, port, err := net.SplitHostPort(target + ":" + defaultPort)
	return host, port, err == nil
}

// addressOfDnsTargetWithMissingSlash returns the address of a dns:// endpoint that lacks the third slash, e.g. host:4317
// for dns://host:4317 (which gRPC interprets as a DNS server without target address), if dns:///<address> would be a
// valid target.
func addressOfDnsTargetWithMissingSlash(endpoint string) (string, bool) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "dns" || u.Host == "" || strings.Trim(u.Path, "/") != "" {
		return "", false
	}
	if unusableGrpcTargetReason(grpcDnsSchemeWithoutAuthority+u.Host) != "" {
		return "", false
	}
	return u.Host, true
}

// operatorManagerGrpcEndpoint renders the endpoint of a gRPC export for the self-monitoring telemetry of the operator
// manager. In contrast to grpcSelfMonitoringEndpoint, it also accepts a dns:// endpoint with a missing slash, so the
// operator manager can still report the invalid endpoint.
func operatorManagerGrpcEndpoint(grpcExport *dash0common.GrpcConfiguration) string {
	if address, hasMissingSlash := addressOfDnsTargetWithMissingSlash(grpcExport.Endpoint); hasMissingSlash {
		return grpcSelfMonitoringEndpoint(&dash0common.GrpcConfiguration{
			Endpoint: grpcDnsSchemeWithoutAuthority + address,
			Insecure: grpcExport.Insecure,
		})
	}
	return grpcSelfMonitoringEndpoint(grpcExport)
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
	if dash0Export.Dataset != "" {
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
	endpoint := grpcSelfMonitoringEndpoint(grpcExport)
	pipeline := prelude +
		fmt.Sprintf(
			`
                protocol: grpc
                endpoint: %s`,
			endpoint,
		)
	pipeline = addInsecureFlagIfNecessary(pipeline, endpoint)
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

// GetAuthTokenForDash0Export takes a Dash0 export configuration and returns the configured token by either
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
