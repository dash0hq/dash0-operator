// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"sort"
	"strings"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

type otlpExporter struct {
	Name               string
	Endpoint           string
	Authorization      *dash0ExporterAuthorization
	Headers            []dash0common.Header
	HeaderEnvVars      []headerSecretEnvVar
	Encoding           string
	Insecure           bool
	InsecureSkipVerify bool
	Keepalive          *dash0common.KeepaliveClientConfig
	BalancerName       string
}

type defaultOtlpExporters = []otlpExporter

type namespacedOtlpExporters = map[string][]otlpExporter

type otlpExporters struct {
	Default    defaultOtlpExporters
	Namespaced namespacedOtlpExporters
}

// headerSecretEnvVar links a generated collector environment variable name to the Kubernetes secret key from which its
// value is sourced. The environment variable is added to the collector pod and referenced from the rendered header
// value via ${env:EnvVarName}.
type headerSecretEnvVar struct {
	EnvVarName   string
	SecretKeyRef *dash0common.SecretKeySelector
}

func (e *otlpExporters) AllNamespaceKeys() []string {
	keys := make([]string, 0, len(e.Namespaced))
	for k := range e.Namespaced {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (e *otlpExporters) allNamespacedExporters() []otlpExporter {
	all := make([]otlpExporter, 0, len(e.Namespaced))
	for _, k := range e.AllNamespaceKeys() {
		all = append(all, e.Namespaced[k]...)
	}
	return all
}

func (e *otlpExporters) allExporters() []otlpExporter {
	nsExporters := e.allNamespacedExporters()
	all := make([]otlpExporter, 0, len(e.Default)+len(nsExporters))
	all = append(all, e.Default...)
	all = append(all, nsExporters...)
	return all
}

const (
	dash0ExporterNamePrefix = "otlp_grpc/dash0"
	grpcExporterNamePrefix  = "otlp_grpc"
	httpExporterNamePrefix  = "otlp_http"

	headerSecretEnvVarPrefix = "DASH0_HEADER"
)

func getDefaultOtlpExporters(dash0Config *dash0v1alpha1.Dash0OperatorConfiguration) ([]otlpExporter, error) {
	if !dash0Config.HasExportsConfigured() {
		return nil, nil
	}

	exporters := make([]otlpExporter, 0, dash0Config.ExportsCount())
	for i, export := range dash0Config.EffectiveExports() {
		exp, err := convertExportSettingsToExporterList(&export, i, true, nil)
		if err != nil {
			return nil, err
		} else {
			exporters = append(exporters, exp...)
		}
	}
	return exporters, nil
}

// getNamespacedOtlpExporters will log and continue on errors. If a namespace has invalid exporter settings, the
// remaining namespaces will still be handled correctly. For the invalid namespace, the default exporters will be used.
func getNamespacedOtlpExporters(
	allMonitoringResources []dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) namespacedOtlpExporters {
	nsExporters := make(map[string][]otlpExporter, len(allMonitoringResources))
	for _, monitoringResource := range allMonitoringResources {
		if !monitoringResource.HasExportsConfigured() {
			continue
		}
		ns := monitoringResource.Namespace

		exporters := make([]otlpExporter, 0, monitoringResource.ExportsCount())
		for i, export := range monitoringResource.EffectiveExports() {
			exp, err := convertExportSettingsToExporterList(&export, i, false, &ns)
			if err != nil {
				logger.ErrorTelemetryCollectionIssue(err, fmt.Sprintf("Custom exporters for namespace %s could not be applied. "+
					"Default exporters will be used for this namespace.", ns))
				break
			} else {
				exporters = append(exporters, exp...)
			}
		}
		nsExporters[ns] = exporters
	}
	return nsExporters
}

func convertExportSettingsToExporterList(
	export *dash0common.Export,
	index int,
	isDefault bool,
	namespace *string,
) ([]otlpExporter, error) {
	if export == nil {
		return nil, nil
	}
	if !isDefault && namespace == nil {
		return nil, fmt.Errorf("no valid nameSuffix provided for namespaced exporter, unable to create OpenTelemetry collector")
	}

	var nameSuffix string
	if isDefault {
		nameSuffix = nameSuffixDefault(index)
	} else {
		nameSuffix = nameSuffixNs(index, *namespace)
	}

	var exporters []otlpExporter

	if export.Dash0 == nil && export.Grpc == nil && export.Http == nil {
		return nil, fmt.Errorf("%s no exporter configuration found", commonExportErrorPrefix)
	}

	if export.Dash0 != nil {
		var auth *dash0ExporterAuthorization
		var err error
		if isDefault {
			auth, err = dash0ExporterAuthorizationForExport(*export, index, isDefault, nil)
		} else {
			auth, err = dash0ExporterAuthorizationForExport(*export, index, isDefault, namespace)
		}
		if err != nil {
			return nil, err
		}

		dash0Exporter, err := convertDash0ExporterToOtlpExporter(export.Dash0, nameSuffix, auth)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, *dash0Exporter)
	}

	if export.Grpc != nil {
		grpcExporter, err := convertGrpcExporterToOtlpExporter(export.Grpc, nameSuffix)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, *grpcExporter)
	}

	if export.Http != nil {
		httpExporter, err := convertHttpExporterToOtlpExporter(export.Http, nameSuffix)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, *httpExporter)
	}

	return exporters, nil
}

func convertDash0ExporterToOtlpExporter(
	d0 *dash0common.Dash0Configuration,
	nameSuffix string,
	auth *dash0ExporterAuthorization,
) (*otlpExporter, error) {
	if d0.Endpoint == "" {
		return nil, fmt.Errorf("no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector")
	} else if auth == nil {
		return nil, fmt.Errorf("no authorization provided for the Dash0 exporter, unable to create the OpenTelemetry collector")
	}
	headers := []dash0common.Header{
		{
			Name:  util.AuthorizationHeaderName,
			Value: authHeaderValue(auth.EnvVarName),
		},
	}
	if d0.Dataset != "" && d0.Dataset != util.DatasetDefault {
		headers = append(headers, dash0common.Header{
			Name:  util.Dash0DatasetHeaderName,
			Value: d0.Dataset,
		})
	}
	dash0Exporter := otlpExporter{
		Name:          fmt.Sprintf("%s/%s", dash0ExporterNamePrefix, nameSuffix),
		Endpoint:      d0.Endpoint,
		Headers:       headers,
		Authorization: auth,
		Keepalive:     d0.Keepalive,
		// For the dash0 exporter we always use pick_first, since client-side load balancing is
		// not needed and it avoids the issue of the exporter trying both the IPv4 and IPv6
		// addresses continuously, which leads to excessive warning logs since one will fail.
		// (Which one will fail, depends on whether the cluster has IPv4 or IPv6 networking.)
		BalancerName: string(dash0common.PickFirst),
	}
	setGrpcTlsFromPrefix(d0.Endpoint, &dash0Exporter)
	return &dash0Exporter, nil
}

func convertGrpcExporterToOtlpExporter(grpc *dash0common.GrpcConfiguration, nameSuffix string) (*otlpExporter, error) {
	if grpc.Endpoint == "" {
		return nil, fmt.Errorf("no endpoint provided for the gRPC exporter, unable to create the OpenTelemetry collector")
	}
	headers, headerEnvVars, err := resolveExporterHeaders(grpc.Headers, "GRPC", nameSuffix)
	if err != nil {
		return nil, err
	}
	grpcExporter := otlpExporter{
		Name:          fmt.Sprintf("%s/%s", grpcExporterNamePrefix, nameSuffix),
		Endpoint:      grpc.Endpoint,
		Headers:       headers,
		HeaderEnvVars: headerEnvVars,
		Keepalive:     grpc.Keepalive,
		BalancerName:  string(grpc.BalancerName),
	}
	if grpc.Insecure != nil {
		grpcExporter.Insecure = *grpc.Insecure
	} else {
		setGrpcTlsFromPrefix(grpc.Endpoint, &grpcExporter)
	}
	setInsecureSkipVerify(grpc.Endpoint, grpc.InsecureSkipVerify, &grpcExporter)
	return &grpcExporter, nil
}

func convertHttpExporterToOtlpExporter(http *dash0common.HttpConfiguration, nameSuffix string) (*otlpExporter, error) {
	if http.Endpoint == "" {
		return nil, fmt.Errorf("no endpoint provided for the HTTP exporter, unable to create the OpenTelemetry collector")
	}
	if http.Encoding == "" {
		return nil, fmt.Errorf("no encoding provided for the HTTP exporter, unable to create the OpenTelemetry collector")
	}
	headers, headerEnvVars, err := resolveExporterHeaders(http.Headers, "HTTP", nameSuffix)
	if err != nil {
		return nil, err
	}
	encoding := string(http.Encoding)
	httpExporter := otlpExporter{
		Name:          fmt.Sprintf("%s/%s/%s", httpExporterNamePrefix, nameSuffix, encoding),
		Endpoint:      http.Endpoint,
		Headers:       headers,
		HeaderEnvVars: headerEnvVars,
		Encoding:      encoding,
	}
	setInsecureSkipVerify(http.Endpoint, http.InsecureSkipVerify, &httpExporter)
	return &httpExporter, nil
}

func setInsecureSkipVerify(endpoint string, insecureSkipVerify *bool, exporter *otlpExporter) {
	if !hasNonTlsPrefix(endpoint) && pointers.ReadBoolPointerWithDefault(insecureSkipVerify, false) {
		exporter.InsecureSkipVerify = true
	}
}

func authHeaderValue(envVarName string) string {
	return fmt.Sprintf("Bearer ${env:%s}", envVarName)
}

func nameSuffixDefault(index int) string {
	return fmt.Sprintf("default_%d", index)
}

func nameSuffixNs(index int, namespace string) string {
	return fmt.Sprintf("ns/%s_%d", namespace, index)
}

// resolveExporterHeaders translates the configured headers of a gRPC or HTTP exporter into the headers rendered into
// the collector configuration plus the list of environment variables that need to be added to the collector pod.
// Headers with a literal value are passed through unchanged. Headers whose value is sourced from a Kubernetes secret
// (valueFrom.secretKeyRef) are rendered as a ${env:...} reference, and a corresponding environment variable (backed by
// the secret) is returned so the collector can resolve the value at runtime. The protocol ("GRPC" or "HTTP") and the
// exporter's nameSuffix are used to derive a collector-pod-wide unique environment variable name per header.
func resolveExporterHeaders(
	headers []dash0common.Header,
	protocol string,
	nameSuffix string,
) ([]dash0common.Header, []headerSecretEnvVar, error) {
	if len(headers) == 0 {
		return nil, nil, nil
	}
	resolved := make([]dash0common.Header, 0, len(headers))
	var headerEnvVars []headerSecretEnvVar
	for i, header := range headers {
		hasValue := header.Value != ""
		hasValueFrom := header.ValueFrom != nil
		if hasValue == hasValueFrom {
			return nil, nil, fmt.Errorf(
				"%s header %q must have exactly one of value or valueFrom set", commonExportErrorPrefix, header.Name)
		}
		if !hasValueFrom {
			resolved = append(resolved, dash0common.Header{Name: header.Name, Value: header.Value})
			continue
		}
		if header.ValueFrom.SecretKeyRef == nil ||
			header.ValueFrom.SecretKeyRef.Name == "" ||
			header.ValueFrom.SecretKeyRef.Key == "" {
			return nil, nil, fmt.Errorf(
				"%s header %q valueFrom must reference a secret name and key", commonExportErrorPrefix, header.Name)
		}
		envVarName := headerSecretEnvVarName(protocol, nameSuffix, i)
		resolved = append(resolved, dash0common.Header{
			Name:  header.Name,
			Value: fmt.Sprintf("${env:%s}", envVarName),
		})
		headerEnvVars = append(headerEnvVars, headerSecretEnvVar{
			EnvVarName:   envVarName,
			SecretKeyRef: header.ValueFrom.SecretKeyRef,
		})
	}
	return resolved, headerEnvVars, nil
}

// headerSecretEnvVarName derives a unique, valid environment variable name for a secret-backed header value. The
// protocol and nameSuffix (e.g. "default_0" or "ns/some-namespace_0") together with the header index guarantee
// uniqueness across all exporters of a single collector pod.
func headerSecretEnvVarName(protocol string, nameSuffix string, headerIndex int) string {
	return fmt.Sprintf("%s_%s_%s_%d", headerSecretEnvVarPrefix, protocol, sanitizeForEnvVarName(nameSuffix), headerIndex)
}

// sanitizeForEnvVarName converts an arbitrary string into an uppercase identifier consisting only of the characters
// [A-Z0-9_], which is required for environment variable names.
func sanitizeForEnvVarName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
