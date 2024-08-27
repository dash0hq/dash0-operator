// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
)

type otlpExporter struct {
	Name     string
	Endpoint string
	Headers  []dash0v1alpha1.Header
	Encoding string
}

var (
	//go:embed config.yaml.template
	collectorConfigurationTemplateSource string
	collectorConfigurationTemplate       = template.Must(
		template.New("collector-configuration").Parse(collectorConfigurationTemplateSource))
)

func collectorConfigMap(config *oTelColConfig) (*corev1.ConfigMap, error) {
	exporters, err := assembleExporters(config.Export)
	if err != nil {
		return nil, fmt.Errorf("cannot assemble the exporters for the configuration: %w", err)
	}
	collectorConfiguration, err := renderCollectorConfigs(&collectorConfigurationTemplateValues{
		Exporters: exporters,
		IgnoreLogsFromNamespaces: []string{
			// Skipping kube-system, it requires bespoke filtering work
			"kube-system",
			// Skipping logs from the operator and the daemonset, otherwise
			// logs will compound in case of log parsing errors
			config.Namespace,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cannot render the collector configuration template: %w", err)
	}

	configMapData := map[string]string{
		collectorConfigurationYaml: collectorConfiguration,
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      collectorConfigConfigMapName(config.NamePrefix),
			Namespace: config.Namespace,
			Labels:    labels(false),
		},
		Data: configMapData,
	}, nil
}

func assembleExporters(export dash0v1alpha1.Export) ([]otlpExporter, error) {
	var exporters []otlpExporter

	if export.Dash0 == nil && export.Grpc == nil && export.Http == nil {
		return nil, fmt.Errorf("no exporter configuration found")
	}

	if export.Dash0 != nil {
		d0 := export.Dash0
		if d0.Endpoint == "" {
			return nil, fmt.Errorf("no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector")
		}
		headers := []dash0v1alpha1.Header{{
			Name:  "Authorization",
			Value: "Bearer ${env:AUTH_TOKEN}",
		}}
		if d0.Dataset != "" && d0.Dataset != "default" {
			headers = append(headers, dash0v1alpha1.Header{
				Name:  "X-Dash0-Dataset",
				Value: d0.Dataset,
			})
		}
		exporters = append(exporters, otlpExporter{
			Name:     "otlp/dash0",
			Endpoint: export.Dash0.Endpoint,
			Headers:  headers,
		})
	}

	if export.Grpc != nil {
		grpc := export.Grpc
		if grpc.Endpoint == "" {
			return nil, fmt.Errorf("no endpoint provided for the gRPC exporter, unable to create the OpenTelemetry collector")
		}
		grpcExporter := otlpExporter{
			Name:     "otlp/grpc",
			Endpoint: grpc.Endpoint,
			Headers:  grpc.Headers,
		}
		if grpc.Headers != nil && len(grpc.Headers) > 0 {
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
		httpExporter := otlpExporter{
			Name:     fmt.Sprintf("otlphttp/%s", encoding),
			Endpoint: http.Endpoint,
			Encoding: encoding,
		}
		if http.Headers != nil && len(http.Headers) > 0 {
			httpExporter.Headers = http.Headers
		}
		exporters = append(exporters, httpExporter)
	}

	return exporters, nil
}

func renderCollectorConfigs(templateValues *collectorConfigurationTemplateValues) (string, error) {
	var collectorConfiguration bytes.Buffer
	if err := collectorConfigurationTemplate.Execute(&collectorConfiguration, templateValues); err != nil {
		return "", err
	}

	return collectorConfiguration.String(), nil
}
