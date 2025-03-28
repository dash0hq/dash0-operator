// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"bytes"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	commonExportErrorPrefix = "cannot assemble the exporters for the configuration:"
)

type customFilters struct {
	ErrorMode           dash0v1alpha1.FilterTransformErrorMode
	SpanConditions      []string
	SpanEventConditions []string
	MetricConditions    []string
	DataPointConditions []string
	LogRecordConditions []string
}

func (cf *customFilters) HasTraceFilters() bool {
	return len(cf.SpanConditions) > 0 || len(cf.SpanEventConditions) > 0
}

func (cf *customFilters) HasMetricFilters() bool {
	return len(cf.MetricConditions) > 0 || len(cf.DataPointConditions) > 0
}

func (cf *customFilters) HasLogFilters() bool {
	return len(cf.LogRecordConditions) > 0
}

type customTransforms struct {
	GlobalErrorMode dash0v1alpha1.FilterTransformErrorMode
	TraceGroups     []customTransformGroup
	MetricGroups    []customTransformGroup
	LogGroups       []customTransformGroup
}

type customTransformGroup struct {
	Context    string
	ErrorMode  dash0v1alpha1.FilterTransformErrorMode
	Conditions []string
	Statements []string
}

func (ct *customTransforms) HasTraceTransforms() bool {
	return len(ct.TraceGroups) > 0
}

func (ct *customTransforms) HasMetricTransforms() bool {
	return len(ct.MetricGroups) > 0
}

func (ct *customTransforms) HasLogTransforms() bool {
	return len(ct.LogGroups) > 0
}

type collectorConfigurationTemplateValues struct {
	Exporters                                        []OtlpExporter
	SendBatchMaxSize                                 *uint32
	IgnoreLogsFromNamespaces                         []string
	KubernetesInfrastructureMetricsCollectionEnabled bool
	KubeletStatsReceiverConfig                       KubeletStatsReceiverConfig
	UseHostMetricsReceiver                           bool
	PseudoClusterUID                                 string
	ClusterName                                      string
	NamespaceOttlFilter                              string
	NamespacesWithPrometheusScraping                 []string
	CustomFilters                                    customFilters
	CustomTransforms                                 customTransforms
	SelfIpReference                                  string
	SelfMonitoringLogsConfig                         string
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

type KubeletStatsReceiverConfig struct {
	Enabled            bool
	Endpoint           string
	AuthType           string
	InsecureSkipVerify bool
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
	filters []NamespacedFilter,
	transforms []NamespacedTransform,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleCollectorConfigMap(
		config,
		monitoredNamespaces,
		namespacesWithPrometheusScraping,
		filters,
		transforms,
		daemonSetCollectorConfigurationTemplate,
		DaemonSetCollectorConfigConfigMapName(config.NamePrefix),
		forDeletion,
	)
}

func assembleDeploymentCollectorConfigMap(
	config *oTelColConfig,
	monitoredNamespaces []string,
	filters []NamespacedFilter,
	transforms []NamespacedTransform,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleCollectorConfigMap(
		config,
		monitoredNamespaces,
		nil,
		filters,
		transforms,
		deploymentCollectorConfigurationTemplate,
		DeploymentCollectorConfigConfigMapName(config.NamePrefix),
		forDeletion,
	)
}

func assembleCollectorConfigMap(
	config *oTelColConfig,
	monitoredNamespaces []string,
	namespacesWithPrometheusScraping []string,
	filters []NamespacedFilter,
	transforms []NamespacedTransform,
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

		selfIpReference := "${env:K8S_POD_IP}"
		if config.IsIPv6Cluster {
			selfIpReference = "[${env:K8S_POD_IP}]"
		}
		namespaceOttlFilter := renderOttlNamespaceFilter(monitoredNamespaces)
		customTelemetryFilters := aggregateCustomFilters(filters)
		customTelemetryTransforms := aggregateCustomTransforms(transforms)
		selfMonitoringLogsConfig :=
			selfmonitoringapiaccess.ConvertExportConfigurationToCollectorLogSelfMonitoringPipelineString(
				config.SelfMonitoringConfiguration,
			)

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
				KubeletStatsReceiverConfig:                       config.KubeletStatsReceiverConfig,
				UseHostMetricsReceiver:                           config.UseHostMetricsReceiver,
				PseudoClusterUID:                                 config.PseudoClusterUID,
				ClusterName:                                      config.ClusterName,
				NamespaceOttlFilter:                              namespaceOttlFilter,
				NamespacesWithPrometheusScraping:                 namespacesWithPrometheusScraping,
				CustomFilters:                                    customTelemetryFilters,
				CustomTransforms:                                 customTelemetryTransforms,
				SelfIpReference:                                  selfIpReference,
				DevelopmentMode:                                  config.DevelopmentMode,
				DebugVerbosityDetailed:                           config.DebugVerbosityDetailed,
				SelfMonitoringLogsConfig:                         selfMonitoringLogsConfig,
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

func aggregateCustomFilters(filtersSpec []NamespacedFilter) customFilters {
	var errorMode dash0v1alpha1.FilterTransformErrorMode
	var allSpanFilters []string
	var allSpanEventFilters []string
	var allMetricFilters []string
	var allDataPointFilters []string
	var allLogRecordFilters []string
	for _, filterSpecForNamespace := range filtersSpec {
		if !filterSpecForNamespace.HasAnyFilters() {
			continue
		}
		namespace := filterSpecForNamespace.Namespace
		if filterSpecForNamespace.Traces != nil && filterSpecForNamespace.Traces.HasAnyFilters() {
			if len(filterSpecForNamespace.Traces.SpanFilter) > 0 {
				for _, condition := range filterSpecForNamespace.Traces.SpanFilter {
					allSpanFilters =
						append(allSpanFilters, prependNamespaceCheckToOttlFilterCondition(namespace, condition))
				}
			}
			if len(filterSpecForNamespace.Traces.SpanEventFilter) > 0 {
				for _, condition := range filterSpecForNamespace.Traces.SpanEventFilter {
					allSpanEventFilters =
						append(allSpanEventFilters, prependNamespaceCheckToOttlFilterCondition(namespace, condition))
				}
			}
		}
		if filterSpecForNamespace.Metrics != nil && filterSpecForNamespace.Metrics.HasAnyFilters() {
			if len(filterSpecForNamespace.Metrics.MetricFilter) > 0 {
				for _, condition := range filterSpecForNamespace.Metrics.MetricFilter {
					allMetricFilters =
						append(allMetricFilters, prependNamespaceCheckToOttlFilterCondition(namespace, condition))
				}
			}
			if len(filterSpecForNamespace.Metrics.DataPointFilter) > 0 {
				for _, condition := range filterSpecForNamespace.Metrics.DataPointFilter {
					allDataPointFilters =
						append(allDataPointFilters, prependNamespaceCheckToOttlFilterCondition(namespace, condition))
				}
			}
		}
		if filterSpecForNamespace.Logs != nil && filterSpecForNamespace.Logs.HasAnyFilters() {
			if len(filterSpecForNamespace.Logs.LogRecordFilter) > 0 {
				for _, condition := range filterSpecForNamespace.Logs.LogRecordFilter {
					allLogRecordFilters =
						append(allLogRecordFilters, prependNamespaceCheckToOttlFilterCondition(namespace, condition))
				}
			}
		}

		// Each namespace could specify a different error mode, however, we only render one filterprocessor per signal,
		// so we use the "most severe" error mode (silent < ignore < propagate).
		errorMode = compareErrorMode(errorMode, filterSpecForNamespace.ErrorMode)
	}

	if errorMode == "" {
		// If no error mode has been specified at all, use ignore as the default. This should not actually happen
		// if there is at least one monitoring resource with a telemetry filter, since the Dash0Monitoring spec
		// defines a default via +kubebuilder:default=ignore.
		errorMode = dash0v1alpha1.FilterTransformErrorModeIgnore
	}

	return customFilters{
		ErrorMode:           errorMode,
		SpanConditions:      allSpanFilters,
		SpanEventConditions: allSpanEventFilters,
		MetricConditions:    allMetricFilters,
		DataPointConditions: allDataPointFilters,
		LogRecordConditions: allLogRecordFilters,
	}
}

func prependNamespaceCheckToOttlFilterCondition(namespace string, condition string) string {
	return fmt.Sprintf(`resource.attributes["k8s.namespace.name"] == "%s" and (%s)`, namespace, condition)
}

func aggregateCustomTransforms(transformsSpec []NamespacedTransform) customTransforms {
	var globalErrorMode dash0v1alpha1.FilterTransformErrorMode
	var allTraceGroups []customTransformGroup
	var allMetricTraceGroups []customTransformGroup
	var allLogGroups []customTransformGroup
	for _, namespacedTransform := range transformsSpec {
		transform := namespacedTransform.Transform
		if !transform.HasAnyStatements() {
			continue
		}
		namespace := namespacedTransform.Namespace
		if len(transform.Traces) > 0 {
			allTraceGroups =
				slices.Concat(allTraceGroups,
					addNamespaceConditionToTransformGroups(namespace, transform.Traces))
		}
		if len(transform.Metrics) > 0 {
			allMetricTraceGroups =
				slices.Concat(allMetricTraceGroups,
					addNamespaceConditionToTransformGroups(namespace, transform.Metrics))
		}
		if len(transform.Logs) > 0 {
			allLogGroups =
				slices.Concat(allLogGroups,
					addNamespaceConditionToTransformGroups(namespace, transform.Logs))
		}

		// Each namespace could specify a different error mode, however, we only render one transform processor per
		// signal, so we use the "most severe" error mode (silent < ignore < propagate).
		if transform.ErrorMode != nil {
			globalErrorMode = compareErrorMode(globalErrorMode, *transform.ErrorMode)
		}
	}

	if globalErrorMode == "" {
		// If no error mode has been specified at all, use ignore as the default. This should not actually happen
		// if there is at least one monitoring resource with a transform configuration, since the Dash0Monitoring spec
		// defines a default via +kubebuilder:default=ignore.
		globalErrorMode = dash0v1alpha1.FilterTransformErrorModeIgnore
	}

	return customTransforms{
		GlobalErrorMode: globalErrorMode,
		TraceGroups:     allTraceGroups,
		MetricGroups:    allMetricTraceGroups,
		LogGroups:       allLogGroups,
	}
}

func addNamespaceConditionToTransformGroups(
	namespace string,
	transformGroups []dash0v1alpha1.NormalizedTransformGroup,
) []customTransformGroup {
	groupsWithNamespaceCondition := make([]customTransformGroup, 0, len(transformGroups))
	for _, transformGroup := range transformGroups {
		transformGroupWithNamespaceCondition := customTransformGroup{}
		if transformGroup.Context != nil && *transformGroup.Context != "" {
			transformGroupWithNamespaceCondition.Context = *transformGroup.Context
		}
		if transformGroup.ErrorMode != nil && *transformGroup.ErrorMode != "" {
			transformGroupWithNamespaceCondition.ErrorMode = *transformGroup.ErrorMode
		}
		if len(transformGroup.Conditions) > 0 {
			transformGroupWithNamespaceCondition.Conditions = transformGroup.Conditions
		}
		transformGroupWithNamespaceCondition.Conditions =
			append(transformGroupWithNamespaceCondition.Conditions,
				fmt.Sprintf(`resource.attributes["k8s.namespace.name"] == "%s"`, namespace),
			)
		if len(transformGroup.Statements) > 0 {
			transformGroupWithNamespaceCondition.Statements = transformGroup.Statements
		}

		groupsWithNamespaceCondition = append(groupsWithNamespaceCondition, transformGroupWithNamespaceCondition)
	}
	return groupsWithNamespaceCondition
}

func compareErrorMode(
	errorMode1 dash0v1alpha1.FilterTransformErrorMode,
	errorMode2 dash0v1alpha1.FilterTransformErrorMode,
) dash0v1alpha1.FilterTransformErrorMode {
	if (errorMode1 == "") ||
		(errorMode1 == dash0v1alpha1.FilterTransformErrorModeSilent &&
			(errorMode2 == dash0v1alpha1.FilterTransformErrorModeIgnore || errorMode2 == dash0v1alpha1.FilterTransformErrorModePropagate)) ||
		(errorMode1 == dash0v1alpha1.FilterTransformErrorModeIgnore && errorMode2 == dash0v1alpha1.FilterTransformErrorModePropagate) {
		return errorMode2
	}
	return errorMode1
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
