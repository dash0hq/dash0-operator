// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
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

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/targetallocator/taresources"
)

const (
	commonExportErrorPrefix = "cannot assemble the exporters for the configuration:"
)

type customFilters struct {
	ErrorMode           dash0common.FilterTransformErrorMode
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
	GlobalErrorMode dash0common.FilterTransformErrorMode
	TraceGroups     []customTransformGroup
	MetricGroups    []customTransformGroup
	LogGroups       []customTransformGroup
}

type customTransformGroup struct {
	Context    string
	ErrorMode  dash0common.FilterTransformErrorMode
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
	OperatorNamespace                                string
	OperatorResourcesNamePrefix                      string
	Exporters                                        otlpExporters
	SendBatchMaxSize                                 *uint32
	KubernetesInfrastructureMetricsCollectionEnabled bool
	CollectPodLabelsAndAnnotationsEnabled            bool
	CollectNamespaceLabelsAndAnnotationsEnabled      bool
	DisableReplicasetInformer                        bool
	PrometheusCrdSupportEnabled                      bool
	TargetAllocatorAppKubernetesIoInstance           string
	TargetAllocatorAppKubernetesIoName               string
	TargetAllocatorServiceName                       string
	TargetAllocatorMtlsEnabled                       bool
	TargetAllocatorMtlsClientCertsDir                string
	KubeletStatsReceiverConfig                       KubeletStatsReceiverConfig
	UseHostMetricsReceiver                           bool
	IsGkeAutopilot                                   bool
	PseudoClusterUid                                 string
	ClusterName                                      string
	NamespacesWithLogCollection                      []string
	NamespacesWithEventCollection                    []string
	NamespaceOttlFilter                              string
	NamespacesWithPrometheusScraping                 []string
	CustomFilters                                    customFilters
	CustomTransforms                                 customTransforms
	SelfIpReference                                  string
	InternalTelemetryEnabled                         bool
	SelfMonitoringEnabled                            bool
	SelfMonitoringMetricsConfig                      string
	SelfMonitoringLogsConfig                         string
	DevelopmentMode                                  bool
	DebugVerbosityDetailed                           bool
	EnableProfExtension                              bool
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
)

func assembleDaemonSetCollectorConfigMap(
	config *oTelColConfig,
	monitoredNamespaces []string,
	namespacesWithLogCollection []string,
	namespacesWithPrometheusScraping []string,
	filters []NamespacedFilter,
	transforms []NamespacedTransform,
	targetAllocatorMtlsConfig TargetAllocatorMtlsConfig,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleCollectorConfigMap(
		config,
		monitoredNamespaces,
		namespacesWithLogCollection,
		nil, // namespacesWithEventCollection is not used when rendering the daemonset config map
		namespacesWithPrometheusScraping,
		filters,
		transforms,
		daemonSetCollectorConfigurationTemplate,
		DaemonSetCollectorConfigConfigMapName(config.NamePrefix),
		targetAllocatorMtlsConfig,
		forDeletion,
	)
}

func assembleDeploymentCollectorConfigMap(
	config *oTelColConfig,
	monitoredNamespaces []string,
	namespacesWithEventCollection []string,
	filters []NamespacedFilter,
	transforms []NamespacedTransform,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	return assembleCollectorConfigMap(
		config,
		monitoredNamespaces,
		nil, // namespacesWithLogCollection is not used when rendering the deployment config map
		namespacesWithEventCollection,
		nil, // namespacesWithPrometheusScraping is not used when rendering the deployment config map
		filters,
		transforms,
		deploymentCollectorConfigurationTemplate,
		DeploymentCollectorConfigConfigMapName(config.NamePrefix),
		TargetAllocatorMtlsConfig{}, // target-allocator mTLS config is not used when rendering the deployment config map
		forDeletion,
	)
}

func assembleCollectorConfigMap(
	config *oTelColConfig,
	monitoredNamespaces []string,
	namespacesWithLogCollection []string,
	namespacesWithEventCollection []string,
	namespacesWithPrometheusScraping []string,
	filters []NamespacedFilter,
	transforms []NamespacedTransform,
	template *template.Template,
	configMapName string,
	targetAllocatorMtlsConfig TargetAllocatorMtlsConfig,
	forDeletion bool,
) (*corev1.ConfigMap, error) {
	var configMapData map[string]string
	if forDeletion {
		configMapData = map[string]string{}
	} else {
		selfIpReference := "${env:K8S_POD_IP}"
		if config.IsIPv6Cluster {
			selfIpReference = "[${env:K8S_POD_IP}]"
		}
		namespaceOttlFilter := renderOttlNamespaceFilter(
			monitoredNamespaces,
			config.SelfMonitoringConfiguration.SelfMonitoringEnabled,
			config.PrometheusCrdSupportEnabled,
		)
		customTelemetryFilters := aggregateCustomFilters(filters)
		customTelemetryTransforms := aggregateCustomTransforms(transforms)
		selfMonitoringMetricsConfig :=
			selfmonitoringapiaccess.ConvertExportConfigurationToCollectorMetricsSelfMonitoringPipelineString(
				config.SelfMonitoringConfiguration,
			)
		selfMonitoringLogsConfig :=
			selfmonitoringapiaccess.ConvertExportConfigurationToCollectorLogsSelfMonitoringPipelineString(
				config.SelfMonitoringConfiguration,
			)

		targetAllocatorServiceName := taresources.ServiceName(config.TargetAllocatorNamePrefix)

		collectorConfiguration, err := renderCollectorConfiguration(template,
			&collectorConfigurationTemplateValues{
				OperatorNamespace:           config.OperatorNamespace,
				OperatorResourcesNamePrefix: config.NamePrefix,
				Exporters:                   config.Exporters,
				SendBatchMaxSize:            config.SendBatchMaxSize,
				KubernetesInfrastructureMetricsCollectionEnabled: config.KubernetesInfrastructureMetricsCollectionEnabled,
				CollectPodLabelsAndAnnotationsEnabled:            config.CollectPodLabelsAndAnnotationsEnabled,
				CollectNamespaceLabelsAndAnnotationsEnabled:      config.CollectNamespaceLabelsAndAnnotationsEnabled,
				DisableReplicasetInformer:                        config.DisableReplicasetInformer,
				PrometheusCrdSupportEnabled:                      config.PrometheusCrdSupportEnabled,
				TargetAllocatorAppKubernetesIoName:               taresources.AppKubernetesIoNameValue,
				TargetAllocatorAppKubernetesIoInstance:           taresources.AppKubernetesIoInstanceValue,
				TargetAllocatorServiceName:                       targetAllocatorServiceName,
				TargetAllocatorMtlsEnabled:                       targetAllocatorMtlsConfig.Enabled,
				TargetAllocatorMtlsClientCertsDir:                targetAllocatorCertsVolumeDir,
				KubeletStatsReceiverConfig:                       config.KubeletStatsReceiverConfig,
				UseHostMetricsReceiver:                           config.UseHostMetricsReceiver,
				IsGkeAutopilot:                                   config.IsGkeAutopilot,
				PseudoClusterUid:                                 string(config.PseudoClusterUid),
				ClusterName:                                      config.ClusterName,
				NamespacesWithLogCollection:                      namespacesWithLogCollection,
				NamespacesWithEventCollection:                    namespacesWithEventCollection,
				NamespaceOttlFilter:                              namespaceOttlFilter,
				NamespacesWithPrometheusScraping:                 namespacesWithPrometheusScraping,
				CustomFilters:                                    customTelemetryFilters,
				CustomTransforms:                                 customTelemetryTransforms,
				SelfIpReference:                                  selfIpReference,
				InternalTelemetryEnabled:                         selfMonitoringMetricsConfig != "" || selfMonitoringLogsConfig != "",
				SelfMonitoringEnabled:                            config.SelfMonitoringConfiguration.SelfMonitoringEnabled,
				SelfMonitoringMetricsConfig:                      selfMonitoringMetricsConfig,
				SelfMonitoringLogsConfig:                         selfMonitoringLogsConfig,
				DevelopmentMode:                                  config.DevelopmentMode,
				DebugVerbosityDetailed:                           config.DebugVerbosityDetailed,
				EnableProfExtension:                              config.EnableProfExtension,
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
			Namespace: config.OperatorNamespace,
			Labels:    labels(false),
		},

		Data: configMapData,
	}, nil
}

func renderOttlNamespaceFilter(monitoredNamespaces []string, selfMonitoringEnabled bool, prometheusCrdSupportEnabled bool) string {
	// Do not drop metrics from the target-allocator even if there isn't a Dash0Monitoring resource in the namespace.
	// target-allocator metrics are considered self-monitoring and therefore controlled via the global SelfMonitoring
	// config (and prometheusCrdSupportEnabled) from the Dash0OperatorConfiguration.
	taExclusion := ""
	if selfMonitoringEnabled && prometheusCrdSupportEnabled {
		taExclusion = fmt.Sprintf(
			"(resource.attributes[\"k8s.pod.label.app.kubernetes.io/instance\"] != \"%s\" and "+
				"resource.attributes[\"k8s.pod.label.app.kubernetes.io/name\"] != \"%s\") and ",
			taresources.AppKubernetesIoInstanceValue, taresources.AppKubernetesIoNameValue)
	}
	// Drop all metrics that have a namespace resource attribute but are from a namespace that is not in the
	// list of monitored namespaces.
	namespaceOttlFilter := fmt.Sprintf("%sresource.attributes[\"k8s.namespace.name\"] != nil\n", taExclusion)
	for _, namespace := range monitoredNamespaces {
		// Be wary of indentation, all lines after the first must start with at least 10 spaces for YAML compliance.
		namespaceOttlFilter = namespaceOttlFilter +
			fmt.Sprintf("          and resource.attributes[\"k8s.namespace.name\"] != \"%s\"\n", namespace)
	}
	return namespaceOttlFilter
}

func aggregateCustomFilters(filtersSpec []NamespacedFilter) customFilters {
	var errorMode dash0common.FilterTransformErrorMode
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
		errorMode = dash0common.FilterTransformErrorModeIgnore
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
	var globalErrorMode dash0common.FilterTransformErrorMode
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
		globalErrorMode = dash0common.FilterTransformErrorModeIgnore
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
	transformGroups []dash0common.NormalizedTransformGroup,
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
	errorMode1 dash0common.FilterTransformErrorMode,
	errorMode2 dash0common.FilterTransformErrorMode,
) dash0common.FilterTransformErrorMode {
	if (errorMode1 == "") ||
		(errorMode1 == dash0common.FilterTransformErrorModeSilent &&
			(errorMode2 == dash0common.FilterTransformErrorModeIgnore || errorMode2 == dash0common.FilterTransformErrorModePropagate)) ||
		(errorMode1 == dash0common.FilterTransformErrorModeIgnore && errorMode2 == dash0common.FilterTransformErrorModePropagate) {
		return errorMode2
	}
	return errorMode1
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

func hasNonTlsPrefix(endpoint string) bool {
	endpointNormalized := strings.ToLower(endpoint)
	return strings.HasPrefix(endpointNormalized, "http://")
}

func setGrpcTlsFromPrefix(endpoint string, exporter *otlpExporter) {
	if exporter.Insecure || hasNonTlsPrefix(endpoint) {
		exporter.Insecure = true
	}
}
