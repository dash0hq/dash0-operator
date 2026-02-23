// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"sort"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/go-logr/logr"
)

type otlpExporter struct {
	Name               string
	Endpoint           string
	Authorization      *dash0ExporterAuthorization
	Headers            []dash0common.Header
	Encoding           string
	Insecure           bool
	InsecureSkipVerify bool
}

type defaultOtlpExporters = []otlpExporter

type namespacedOtlpExporters = map[string][]otlpExporter

type otlpExporters struct {
	Default    defaultOtlpExporters
	Namespaced namespacedOtlpExporters
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
	logger logr.Logger,
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
				logger.Error(err, fmt.Sprintf("Custom exporters for namespace %s could not be applied. "+
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
	}
	setGrpcTlsFromPrefix(d0.Endpoint, &dash0Exporter)
	return &dash0Exporter, nil
}

func convertGrpcExporterToOtlpExporter(grpc *dash0common.GrpcConfiguration, nameSuffix string) (*otlpExporter, error) {
	if grpc.Endpoint == "" {
		return nil, fmt.Errorf("no endpoint provided for the gRPC exporter, unable to create the OpenTelemetry collector")
	}
	grpcExporter := otlpExporter{
		Name:     fmt.Sprintf("%s/%s", grpcExporterNamePrefix, nameSuffix),
		Endpoint: grpc.Endpoint,
		Headers:  grpc.Headers,
	}
	if grpc.Insecure != nil {
		grpcExporter.Insecure = *grpc.Insecure
	} else {
		setGrpcTlsFromPrefix(grpc.Endpoint, &grpcExporter)
	}
	setInsecureSkipVerify(grpc.Endpoint, grpc.InsecureSkipVerify, &grpcExporter)
	if len(grpc.Headers) > 0 {
		grpcExporter.Headers = grpc.Headers
	}
	return &grpcExporter, nil
}

func convertHttpExporterToOtlpExporter(http *dash0common.HttpConfiguration, nameSuffix string) (*otlpExporter, error) {
	if http.Endpoint == "" {
		return nil, fmt.Errorf("no endpoint provided for the HTTP exporter, unable to create the OpenTelemetry collector")
	}
	if http.Encoding == "" {
		return nil, fmt.Errorf("no encoding provided for the HTTP exporter, unable to create the OpenTelemetry collector")
	}
	encoding := string(http.Encoding)
	httpExporter := otlpExporter{
		Name:     fmt.Sprintf("%s/%s/%s", httpExporterNamePrefix, nameSuffix, encoding),
		Endpoint: http.Endpoint,
		Encoding: encoding,
	}
	setInsecureSkipVerify(http.Endpoint, http.InsecureSkipVerify, &httpExporter)
	if len(http.Headers) > 0 {
		httpExporter.Headers = http.Headers
	}
	return &httpExporter, nil
}

func setInsecureSkipVerify(endpoint string, insecureSkipVerify *bool, exporter *otlpExporter) {
	if !hasNonTlsPrefix(endpoint) && util.ReadBoolPointerWithDefault(insecureSkipVerify, false) {
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
