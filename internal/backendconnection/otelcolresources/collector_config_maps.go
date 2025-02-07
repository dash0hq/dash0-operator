// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	commonExportErrorPrefix = "cannot assemble the exporters for the configuration:"
)

type collectorConfigurationTemplateValues struct {
	Exporters                                        []OtlpExporter
	SendBatchMaxSize                                 *uint32
	IgnoreLogsFromNamespaces                         []string
	KubernetesInfrastructureMetricsCollectionEnabled bool
	UseHostMetricsReceiver                           bool
	ClusterName                                      string
	NamespaceOttlFilter                              string
	NamespacesWithPrometheusScraping                 []string
	SelfIpReference                                  string
	DevelopmentMode                                  bool
	DebugVerbosityDetailed                           bool
}

type OtlpExporter struct {
	Name     string
	Endpoint string
	Headers  []dash0v1alpha1.Header
	Encoding string
	Insecure bool
}

var (
	//go:embed daemonset.config.yaml.template
	daemonSetCollectorConfigurationTemplateSource string
	daemonSetCollectorConfigurationTemplate       = template.Must(
		template.New("daemonset-collector-configuration").Parse(daemonSetCollectorConfigurationTemplateSource))

	//go:embed deployment.config.yaml.template
	deploymentCollectorConfigurationTemplateSource string
	deploymentCollectorConfigurationTemplate       = template.Must(
		template.New("deployment-collector-configuration").Parse(deploymentCollectorConfigurationTemplateSource))

	authHeaderValue = fmt.Sprintf("Bearer ${env:%s}", authTokenEnvVarName)
)

func assembleDaemonSetCollectorConfigMap(
	config *oTelColConfig,
	monitoredNamespaces []string,
	namespacesWithPrometheusScraping []string,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleCollectorConfigMap(
		config,
		monitoredNamespaces,
		namespacesWithPrometheusScraping,
		daemonSetCollectorConfigurationTemplate,
		DaemonSetCollectorConfigConfigMapName(config.NamePrefix),
		forDeletion,
	)
}

func assembleDeploymentCollectorConfigMap(
	config *oTelColConfig,
	monitoredNamespaces []string,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleCollectorConfigMap(
		config,
		monitoredNamespaces,
		nil,
		deploymentCollectorConfigurationTemplate,
		DeploymentCollectorConfigConfigMapName(config.NamePrefix),
		forDeletion,
	)
}

func assembleCollectorConfigMap(
	config *oTelColConfig,
	monitoredNamespaces []string,
	namespacesWithPrometheusScraping []string,
	template *template.Template,
	configMapName string,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	var configMapData map[string]string
	if forDeletion {
		configMapData = map[string]string{}
	} else {
		exporters, err := ConvertExportSettingsToExporterList(config.Export)
		if err != nil {
			return nil, fmt.Errorf("%s %w", commonExportErrorPrefix, err)
		}

		selfIpReference := "${env:MY_POD_IP}"
		if config.IsIPv6Cluster {
			selfIpReference = "[${env:MY_POD_IP}]"
		}
		namespaceOttlFilter := renderOttlNamespaceFilter(monitoredNamespaces)
		collectorConfiguration, err := renderCollectorConfiguration(template,
			&collectorConfigurationTemplateValues{
				Exporters:        exporters,
				SendBatchMaxSize: config.SendBatchMaxSize,
				IgnoreLogsFromNamespaces: []string{
					// Skipping kube-system, it requires bespoke filtering work
					"kube-system",
					// Skipping logs from the operator and the daemonset, otherwise
					// logs will compound in case of log parsing errors
					config.Namespace,
				},
				KubernetesInfrastructureMetricsCollectionEnabled: config.KubernetesInfrastructureMetricsCollectionEnabled,
				UseHostMetricsReceiver:                           config.UseHostMetricsReceiver,
				ClusterName:                                      config.ClusterName,
				NamespaceOttlFilter:                              namespaceOttlFilter,
				NamespacesWithPrometheusScraping:                 namespacesWithPrometheusScraping,
				SelfIpReference:                                  selfIpReference,
				DevelopmentMode:                                  config.DevelopmentMode,
				DebugVerbosityDetailed:                           config.DebugVerbosityDetailed,
			})
		if err != nil {
			return nil, fmt.Errorf("cannot render the collector configuration template: %w", err)
		}

		configMapData = map[string]string{
			collectorConfigurationYaml: collectorConfiguration,
		}
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
		Data: configMapData,
	}, nil
}

func renderOttlNamespaceFilter(monitoredNamespaces []string) string {
	// Drop all metrics that have a namespace resource attribute but are from a namespace that is not in the
	// list of monitored namespaces.
	namespaceOttlFilter := "resource.attributes[\"k8s.namespace.name\"] != nil\n"
	for _, namespace := range monitoredNamespaces {
		// Be wary of indentation, all lines after the first must start with at least 10 spaces for YAML compliance.
		namespaceOttlFilter = namespaceOttlFilter +
			fmt.Sprintf("          and resource.attributes[\"k8s.namespace.name\"] != \"%s\"\n", namespace)
	}
	return namespaceOttlFilter
}

func ConvertExportSettingsToExporterList(export dash0v1alpha1.Export) ([]OtlpExporter, error) {
	var exporters []OtlpExporter

	if export.Dash0 == nil && export.Grpc == nil && export.Http == nil {
		return nil, fmt.Errorf("%s no exporter configuration found", commonExportErrorPrefix)
	}

	if export.Dash0 != nil {
		d0 := export.Dash0
		if d0.Endpoint == "" {
			return nil, fmt.Errorf("no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector")
		}
		headers := []dash0v1alpha1.Header{{
			Name:  util.AuthorizationHeaderName,
			Value: authHeaderValue,
		}}
		if d0.Dataset != "" && d0.Dataset != util.DatasetDefault {
			headers = append(headers, dash0v1alpha1.Header{
				Name:  util.Dash0DatasetHeaderName,
				Value: d0.Dataset,
			})
		}
		dash0Exporter := OtlpExporter{
			Name:     "otlp/dash0",
			Endpoint: export.Dash0.Endpoint,
			Headers:  headers,
		}
		setGrpcTls(export.Dash0.Endpoint, &dash0Exporter)
		exporters = append(exporters, dash0Exporter)
	}

	if export.Grpc != nil {
		grpc := export.Grpc
		if grpc.Endpoint == "" {
			return nil, fmt.Errorf("no endpoint provided for the gRPC exporter, unable to create the OpenTelemetry collector")
		}
		grpcExporter := OtlpExporter{
			Name:     "otlp/grpc",
			Endpoint: grpc.Endpoint,
			Headers:  grpc.Headers,
		}
		setGrpcTls(grpc.Endpoint, &grpcExporter)
		if len(grpc.Headers) > 0 {
			grpcExporter.Headers = grpc.Headers
		}
		exporters = append(exporters, grpcExporter)
	}

	if export.Http != nil {
		http := export.Http
		if http.Endpoint == "" {
			return nil, fmt.Errorf("no endpoint provided for the HTTP exporter, unable to create the OpenTelemetry collector")
		}
		if http.Encoding == "" {
			return nil, fmt.Errorf("no encoding provided for the HTTP exporter, unable to create the OpenTelemetry collector")
		}
		encoding := string(http.Encoding)
		httpExporter := OtlpExporter{
			Name:     fmt.Sprintf("otlphttp/%s", encoding),
			Endpoint: http.Endpoint,
			Encoding: encoding,
		}
		if len(http.Headers) > 0 {
			httpExporter.Headers = http.Headers
		}
		exporters = append(exporters, httpExporter)
	}

	return exporters, nil
}

func renderCollectorConfiguration(
	template *template.Template,
	templateValues *collectorConfigurationTemplateValues,
) (string, error) {
	var collectorConfiguration bytes.Buffer
	if err := template.Execute(&collectorConfiguration, templateValues); err != nil {
		return "", err
	}
	return collectorConfiguration.String(), nil
}

func setGrpcTls(endpoint string, exporter *OtlpExporter) {
	endpointNormalized := strings.ToLower(endpoint)
	hasNonTlsPrefix := strings.HasPrefix(endpointNormalized, "http://")
	if hasNonTlsPrefix {
		exporter.Insecure = true
	}
}
