// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// The collector configuration matrix is the set of collector configurations that are rendered and then handed to the
// collector binary for validation, see collector_config_validate_test.go. Its purpose is to notice when the
// OpenTelemetry collector project removes or renames a configuration setting that the operator still renders. Such a
// change makes the collectors crash on startup, and it only surfaces for the configurations that actually render the
// affected setting, which are often the non-default ones.
//
// The matrix consists of a minimal and a maximal baseline plus, for every knob that a collector configuration
// template branches on, one entry that flips exactly that knob against each baseline. The isolated flips matter: a
// setting that is only rendered for one rarely used option is exactly the case that neither the end-to-end tests nor a
// single "everything enabled" configuration would cover.
//
// TestTemplateConditionCoverage verifies that the matrix renders both sides of every conditional in the templates, so
// a new conditional cannot be added without extending the matrix.

// collectorConfigMatrixEntry is one configuration in the matrix, i.e. everything that is needed to render the
// collector configuration templates once.
type collectorConfigMatrixEntry struct {
	name                             string
	config                           oTelColConfig
	monitoredNamespaces              []string
	namespacesWithLogCollection      []string
	namespacesWithEventCollection    []string
	namespacesWithPrometheusScraping []string
	filters                          []NamespacedFilter
	transforms                       []NamespacedTransform
	targetAllocatorMtlsConfig        TargetAllocatorMtlsConfig
}

// renderedCollectorConfig is one rendered collector configuration together with the name of the matrix entry and the
// collector workload it belongs to.
type renderedCollectorConfig struct {
	name    string
	content string
	// signalControlCollector marks the configurations of the Signal Control collector deployment, which runs a
	// different image than the daemonset and the cluster metrics collector.
	signalControlCollector bool
	// featureGates are the feature gates the operator passes to this collector, which have to be passed when
	// validating the configuration as well: a pipeline behind a gate is rejected while the gate is off.
	featureGates []string
}

// matrixKnob is one configuration option that at least one collector configuration template branches on. The matrix
// contains one entry per knob and baseline, with only this knob flipped relative to the baseline.
type matrixKnob struct {
	name   string
	toggle func(entry *collectorConfigMatrixEntry, enabled bool)
}

// minimalMatrixBaseline is the smallest configuration the operator can render: a single Dash0 exporter and nothing
// else switched on.
func minimalMatrixBaseline() collectorConfigMatrixEntry {
	return collectorConfigMatrixEntry{
		name: "minimal",
		config: oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Exporters:         cmTestSingleDefaultOtlpExporter(),
		},
		monitoredNamespaces: monitoredNamespaces,
	}
}

// maximalMatrixBaseline enables every option that can be enabled at the same time. Options that exclude each other
// (for example a Dash0 exporter versus a pure pass-through exporter) are covered by the per-knob entries instead.
func maximalMatrixBaseline() collectorConfigMatrixEntry {
	entry := collectorConfigMatrixEntry{
		name: "maximal",
		config: oTelColConfig{
			OperatorNamespace:             OperatorNamespace,
			NamePrefix:                    namePrefix,
			OperatorManagerDeploymentName: "dash0-operator-controller",
			Exporters:                     cmTestDash0GrpcAndHttpExporters(),
			SendBatchSize:                 ptr.To(uint32(512)),
			SendBatchMaxSize:              ptr.To(uint32(1024)),
			SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: true,
				Export:                *Dash0ExportWithEndpointAndToken(),
			},
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			CollectPodLabelsAndAnnotationsEnabled:            true,
			CollectNamespaceLabelsAndAnnotationsEnabled:      true,
			CollectNodeLabelsAndAnnotationsEnabled:           true,
			K8sAttributesDisableReplicasetInformer:           true,
			K8sAttributesWaitForMetadata:                     true,
			K8sAttributesWaitForMetadataTimeout:              "10s",
			PrometheusCrdSupportEnabled:                      true,
			TargetAllocatorNamePrefix:                        namePrefix,
			Agent0ConnectorEnabled:                           true,
			Agent0ConnectorDeploymentName:                    "dash0-operator-agent0-connector",
			KubeletStatsReceiverConfig: util.KubeletStatsReceiverConfig{
				Enabled:            true,
				Endpoint:           "${env:K8S_NODE_NAME}:10250",
				AuthType:           "serviceAccount",
				InsecureSkipVerify: true,
			},
			UseHostMetricsReceiver:            true,
			PseudoClusterUid:                  "d7d3d4f2-7f8f-4f0a-9f1a-2f0b9e1c4a55",
			ClusterName:                       "matrix-test-cluster",
			Images:                            util.Images{OperatorImage: "dash0-operator-controller:1.2.3"},
			SignalControl:                     maximalSignalControlConfig(),
			DevelopmentMode:                   true,
			DebugVerbosityDetailed:            true,
			DaemonSetCollectorMemoryLimit:     resource.MustParse("500Mi"),
			DeploymentCollectorMemoryLimit:    resource.MustParse("500Mi"),
			SignalControlCollectorMemoryLimit: resource.MustParse("500Mi"),
			EnableProfExtension:               true,
			ProfilingEnabled:                  true,
		},
		monitoredNamespaces:              monitoredNamespaces,
		namespacesWithLogCollection:      monitoredNamespaces,
		namespacesWithEventCollection:    defaultNamespacesWithEventCollection,
		namespacesWithPrometheusScraping: monitoredNamespaces,
		filters:                          matrixFilters(),
		transforms:                       matrixTransforms(),
		targetAllocatorMtlsConfig: TargetAllocatorMtlsConfig{
			Enabled:              true,
			ClientCertSecretName: "target-allocator-client-certs",
		},
	}
	// The Signal Control collector is only rendered when the gateway is active, which additionally requires the
	// Signal Control collector image to be set.
	entry.config.Images.SignalControlCollectorImage = "dash0-signal-control-collector:1.2.3"
	return entry
}

func maximalSignalControlConfig() SignalControlConfig {
	return SignalControlConfig{
		Enabled:                            true,
		SamplingEnabled:                    true,
		SamplingFallbackSampleRatio:        "0.1",
		SamplingDebug:                      true,
		SamplingEnableBatching:             true,
		SamplingReservoirType:              "disk",
		SamplingReservoirMaxDiskBytes:      1024 * 1024 * 1024,
		SamplingReservoirMaxMemoryBytes:    128 * 1024 * 1024,
		SamplingReservoirMetricLevel:       "detailed",
		SamplingReservoirBufferDuration:    "30s",
		SignalToMetricsEnabled:             true,
		SignalToMetricsMaxTimeSeries:       ptr.To(int32(10000)),
		SignalToMetricsFlushInterval:       "60s",
		SignalToMetricsCacheExpiration:     "10m",
		RedMetricsMaxTimeSeries:            ptr.To(int32(5000)),
		RedMetricsAdditionalSpanAttributes: []string{"http.route", "rpc.method"},
		SpamFilterEnabled:                  true,
		SpamFilterCacheExpiration:          "5m",
		SpamFilterAllowNoSettingsExt:       true,
		OperationPreferSpanName:            true,
		OperationCardinalityRules: []SignalControlCardinalityRule{
			{
				Id:              "rule-1",
				SourceAttribute: "http.route",
				QuickFilter:     "/api/",
				OperationMatchers: []SignalControlOperationMatcher{
					{
						Regex:        "^/api/v1/users/[0-9]+$",
						Replacements: []string{"/api/v1/users/{id}"},
					},
				},
			},
		},
		Endpoint:         "decision-maker.example.com:443",
		ApiEndpoint:      "https://control-plane-api.dash0.com",
		AuthEnvVar:       "DASH0_SIGNAL_CONTROL_AUTH_TOKEN",
		Dataset:          "default",
		EdgeProxyEnabled: true,
		EdgeProxyName:    "dash0-operator-edge-proxy",
	}
}

// matrixFilters covers every signal and object type the filter processor can be rendered for.
func matrixFilters() []NamespacedFilter {
	return []NamespacedFilter{
		{
			Namespace: namespace1,
			Filter: dash0common.Filter{
				ErrorMode: dash0common.FilterTransformErrorModeIgnore,
				Traces: &dash0common.TraceFilter{
					SpanFilter:      []string{`attributes["http.route"] == "/ready"`},
					SpanEventFilter: []string{`name == "exception"`},
				},
				Metrics: &dash0common.MetricFilter{
					MetricFilter:    []string{`name == "k8s.replicaset.available"`},
					DataPointFilter: []string{`attributes["container.name"] == "dash0-noop"`},
				},
				Logs: &dash0common.LogFilter{
					LogRecordFilter: []string{`IsMatch(body, ".*password.*")`},
				},
				Profiles: &dash0common.ProfileFilter{
					ProfileFilter: []string{`attributes["profile.drop"] == true`},
				},
			},
		},
	}
}

// matrixTransforms covers every signal the transform processor can be rendered for.
func matrixTransforms() []NamespacedTransform {
	context := "resource"
	errorMode := dash0common.FilterTransformErrorModeIgnore
	group := func() dash0common.NormalizedTransformGroup {
		return dash0common.NormalizedTransformGroup{
			Context:    &context,
			ErrorMode:  &errorMode,
			Conditions: []string{`attributes["k8s.namespace.name"] != nil`},
			Statements: []string{`set(attributes["dash0.matrix"], "true")`},
		}
	}
	return []NamespacedTransform{
		{
			Namespace: namespace1,
			Transform: dash0common.NormalizedTransformSpec{
				ErrorMode: &errorMode,
				Traces:    []dash0common.NormalizedTransformGroup{group()},
				Metrics:   []dash0common.NormalizedTransformGroup{group()},
				Logs:      []dash0common.NormalizedTransformGroup{group()},
				Profiles:  []dash0common.NormalizedTransformGroup{group()},
			},
		},
	}
}

// matrixKnobs lists the configuration options that the collector configuration templates branch on. Every knob is
// flipped in isolation against both baselines.
func matrixKnobs() []matrixKnob {
	return []matrixKnob{
		{
			name: "kubernetes-infrastructure-metrics",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.KubernetesInfrastructureMetricsCollectionEnabled = enabled
			},
		},
		{
			name: "pod-labels-and-annotations",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.CollectPodLabelsAndAnnotationsEnabled = enabled
			},
		},
		{
			name: "namespace-labels-and-annotations",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.CollectNamespaceLabelsAndAnnotationsEnabled = enabled
			},
		},
		{
			name: "node-labels-and-annotations",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.CollectNodeLabelsAndAnnotationsEnabled = enabled
			},
		},
		{
			// This is the knob behind OPE-568: it renders a k8s_attributes setting that upstream removed.
			name: "k8s-attributes-disable-replicaset-informer",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.K8sAttributesDisableReplicasetInformer = enabled
			},
		},
		{
			name: "k8s-attributes-wait-for-metadata",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.K8sAttributesWaitForMetadata = enabled
				if enabled {
					e.config.K8sAttributesWaitForMetadataTimeout = "10s"
				} else {
					e.config.K8sAttributesWaitForMetadataTimeout = ""
				}
			},
		},
		{
			name: "k8s-attributes-wait-for-metadata-without-timeout",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.K8sAttributesWaitForMetadata = enabled
				e.config.K8sAttributesWaitForMetadataTimeout = ""
			},
		},
		{
			name: "prometheus-crd-support",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.PrometheusCrdSupportEnabled = enabled
				if enabled {
					e.config.TargetAllocatorNamePrefix = namePrefix
				} else {
					e.config.TargetAllocatorNamePrefix = ""
				}
			},
		},
		{
			name: "target-allocator-mtls",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.PrometheusCrdSupportEnabled = true
				e.config.TargetAllocatorNamePrefix = namePrefix
				e.targetAllocatorMtlsConfig = TargetAllocatorMtlsConfig{Enabled: enabled}
				if enabled {
					e.targetAllocatorMtlsConfig.ClientCertSecretName = "target-allocator-client-certs"
				}
			},
		},
		{
			name: "agent0-connector",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.Agent0ConnectorEnabled = enabled
				if enabled {
					e.config.Agent0ConnectorDeploymentName = "dash0-operator-agent0-connector"
				} else {
					e.config.Agent0ConnectorDeploymentName = ""
				}
			},
		},
		{
			name: "kubeletstats-receiver",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.KubeletStatsReceiverConfig = util.KubeletStatsReceiverConfig{
					Enabled:  enabled,
					Endpoint: "${env:K8S_NODE_NAME}:10250",
					AuthType: "serviceAccount",
				}
			},
		},
		{
			name: "kubeletstats-insecure-skip-verify",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.KubeletStatsReceiverConfig = util.KubeletStatsReceiverConfig{
					Enabled:            true,
					Endpoint:           "${env:K8S_NODE_NAME}:10250",
					AuthType:           "serviceAccount",
					InsecureSkipVerify: enabled,
				}
			},
		},
		{
			name: "hostmetrics-receiver",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.UseHostMetricsReceiver = enabled
			},
		},
		{
			name: "gke-autopilot",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.IsGkeAutopilot = enabled
			},
		},
		{
			name: "ipv6-cluster",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.IsIPv6Cluster = enabled
			},
		},
		{
			name: "cluster-name",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.config.ClusterName = "matrix-test-cluster"
				} else {
					e.config.ClusterName = ""
				}
			},
		},
		{
			name: "batch-sizes",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.config.SendBatchSize = ptr.To(uint32(512))
					e.config.SendBatchMaxSize = ptr.To(uint32(1024))
				} else {
					e.config.SendBatchSize = nil
					e.config.SendBatchMaxSize = nil
				}
			},
		},
		{
			name: "self-monitoring",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.config.SelfMonitoringConfiguration = selfmonitoringapiaccess.SelfMonitoringConfiguration{
						SelfMonitoringEnabled: true,
						Export:                *Dash0ExportWithEndpointAndToken(),
					}
				} else {
					e.config.SelfMonitoringConfiguration = selfmonitoringapiaccess.SelfMonitoringConfiguration{}
				}
			},
		},
		{
			name: "development-mode",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.DevelopmentMode = enabled
			},
		},
		{
			name: "debug-verbosity-detailed",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.DevelopmentMode = true
				e.config.DebugVerbosityDetailed = enabled
			},
		},
		{
			name: "pprof-extension",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.EnableProfExtension = enabled
			},
		},
		{
			name: "profiling",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				e.config.ProfilingEnabled = enabled
			},
		},
		{
			name: "log-collection",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.namespacesWithLogCollection = monitoredNamespaces
				} else {
					e.namespacesWithLogCollection = nil
				}
			},
		},
		{
			name: "event-collection",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.namespacesWithEventCollection = defaultNamespacesWithEventCollection
				} else {
					e.namespacesWithEventCollection = nil
				}
			},
		},
		{
			name: "prometheus-scraping",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.namespacesWithPrometheusScraping = monitoredNamespaces
				} else {
					e.namespacesWithPrometheusScraping = nil
				}
			},
		},
		{
			name: "custom-filters",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.filters = matrixFilters()
				} else {
					e.filters = nil
				}
			},
		},
		{
			name: "custom-transforms",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.transforms = matrixTransforms()
				} else {
					e.transforms = nil
				}
			},
		},
		{
			name: "monitored-namespaces",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.monitoredNamespaces = monitoredNamespaces
				} else {
					e.monitoredNamespaces = nil
				}
			},
		},
		{
			name: "passthrough-exporters-only",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.config.Exporters = cmTestGrpcExporterWithInsecure()
				} else {
					e.config.Exporters = cmTestSingleDefaultOtlpExporter()
				}
			},
		},
		{
			name: "namespaced-exporters",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.config.Exporters = matrixNamespacedExporters()
				} else {
					e.config.Exporters = cmTestSingleDefaultOtlpExporter()
				}
			},
		},
		{
			name: "signal-control",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				if enabled {
					e.config.SignalControl = maximalSignalControlConfig()
					e.config.Images.SignalControlCollectorImage = "dash0-signal-control-collector:1.2.3"
				} else {
					e.config.SignalControl = SignalControlConfig{}
					e.config.Images.SignalControlCollectorImage = ""
				}
			},
		},
		{
			name: "signal-control-sampling",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				e.config.SignalControl.SamplingEnabled = enabled
			},
		},
		{
			name: "signal-control-sampling-debug",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				e.config.SignalControl.SamplingDebug = enabled
			},
		},
		{
			name: "signal-control-sampling-batching",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				e.config.SignalControl.SamplingEnableBatching = enabled
			},
		},
		{
			name: "signal-control-sampling-fallback-ratio",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				if enabled {
					e.config.SignalControl.SamplingFallbackSampleRatio = "0.1"
				} else {
					e.config.SignalControl.SamplingFallbackSampleRatio = ""
				}
			},
		},
		{
			name: "signal-control-memory-reservoir",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				if enabled {
					e.config.SignalControl.SamplingReservoirType = "memory"
					e.config.SignalControl.SamplingReservoirMaxMemoryBytes = 128 * 1024 * 1024
				} else {
					e.config.SignalControl.SamplingReservoirType = "disk"
					e.config.SignalControl.SamplingReservoirMaxMemoryBytes = 0
				}
			},
		},
		{
			name: "signal-control-reservoir-buffer-duration",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				if enabled {
					e.config.SignalControl.SamplingReservoirBufferDuration = "30s"
				} else {
					e.config.SignalControl.SamplingReservoirBufferDuration = ""
				}
			},
		},
		{
			name: "signal-control-signal-to-metrics",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				e.config.SignalControl.SignalToMetricsEnabled = enabled
			},
		},
		{
			name: "signal-control-signal-to-metrics-limits",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				e.config.SignalControl.SignalToMetricsEnabled = true
				if enabled {
					e.config.SignalControl.SignalToMetricsMaxTimeSeries = ptr.To(int32(10000))
					e.config.SignalControl.SignalToMetricsFlushInterval = "60s"
					e.config.SignalControl.SignalToMetricsCacheExpiration = "10m"
					e.config.SignalControl.RedMetricsMaxTimeSeries = ptr.To(int32(5000))
					e.config.SignalControl.RedMetricsAdditionalSpanAttributes = []string{"http.route"}
				} else {
					e.config.SignalControl.SignalToMetricsMaxTimeSeries = nil
					e.config.SignalControl.SignalToMetricsFlushInterval = ""
					e.config.SignalControl.SignalToMetricsCacheExpiration = ""
					e.config.SignalControl.RedMetricsMaxTimeSeries = nil
					e.config.SignalControl.RedMetricsAdditionalSpanAttributes = nil
				}
			},
		},
		{
			name: "signal-control-spam-filter",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				e.config.SignalControl.SpamFilterEnabled = enabled
			},
		},
		{
			name: "signal-control-spam-filter-options",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				e.config.SignalControl.SpamFilterEnabled = true
				e.config.SignalControl.SpamFilterAllowNoSettingsExt = enabled
				if enabled {
					e.config.SignalControl.SpamFilterCacheExpiration = "5m"
				} else {
					e.config.SignalControl.SpamFilterCacheExpiration = ""
				}
			},
		},
		{
			name: "signal-control-operation-prefer-span-name",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				e.config.SignalControl.OperationPreferSpanName = enabled
			},
		},
		{
			name: "signal-control-cardinality-rules",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				if enabled {
					e.config.SignalControl.OperationCardinalityRules =
						maximalSignalControlConfig().OperationCardinalityRules
				} else {
					e.config.SignalControl.OperationCardinalityRules = nil
				}
			},
		},
		{
			name: "signal-control-api-endpoint",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				if enabled {
					e.config.SignalControl.ApiEndpoint = "https://control-plane-api.dash0.com"
				} else {
					e.config.SignalControl.ApiEndpoint = ""
				}
			},
		},
		{
			name: "signal-control-edge-proxy",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				enableSignalControl(e)
				e.config.SignalControl.EdgeProxyEnabled = enabled
				if enabled {
					e.config.SignalControl.EdgeProxyName = "dash0-operator-edge-proxy"
				} else {
					e.config.SignalControl.EdgeProxyName = ""
				}
			},
		},
		{
			// With a memory limit the memory_limiter uses absolute thresholds, without one it falls back to the
			// percentage-based configuration.
			name: "collector-memory-limit",
			toggle: func(e *collectorConfigMatrixEntry, enabled bool) {
				memoryLimit := resource.Quantity{}
				if enabled {
					memoryLimit = resource.MustParse("500Mi")
				}
				e.config.DaemonSetCollectorMemoryLimit = memoryLimit
				e.config.DeploymentCollectorMemoryLimit = memoryLimit
				e.config.SignalControlCollectorMemoryLimit = memoryLimit
			},
		},
	}
}

// enableSignalControl brings an entry into a state where Signal Control is active, so that a knob below Signal
// Control has an effect regardless of the baseline it is applied to.
func enableSignalControl(e *collectorConfigMatrixEntry) {
	if !e.config.SignalControl.Enabled {
		e.config.SignalControl = maximalSignalControlConfig()
	}
	e.config.Images.SignalControlCollectorImage = "dash0-signal-control-collector:1.2.3"
}

func matrixNamespacedExporters() otlpExporters {
	exporters := cmTestSingleDefaultOtlpExporter()
	export := Dash0ExportWithEndpointAndToken()
	auth, _ := dash0ExporterAuthorizationForExport(*export, 1, true, nil)
	dash0Exporter, _ := convertDash0ExporterToOtlpExporter(export.Dash0, namespace1, auth)
	grpcExporter, _ := convertGrpcExporterToOtlpExporter(GrpcExportTest().Grpc, namespace2)
	exporters.Namespaced = map[string][]otlpExporter{
		namespace1: {*dash0Exporter},
		namespace2: {*grpcExporter},
	}
	return exporters
}

// collectorConfigMatrix returns both baselines plus, for every knob, the baseline with only that knob flipped.
func collectorConfigMatrix() []collectorConfigMatrixEntry {
	baselines := []struct {
		entry   collectorConfigMatrixEntry
		flipsTo bool
	}{
		// The minimal baseline has everything switched off, so flipping a knob means enabling it, and vice versa for
		// the maximal baseline.
		{entry: minimalMatrixBaseline(), flipsTo: true},
		{entry: maximalMatrixBaseline(), flipsTo: false},
	}

	matrix := make([]collectorConfigMatrixEntry, 0, 2+2*len(matrixKnobs()))
	for _, baseline := range baselines {
		matrix = append(matrix, baseline.entry)
	}
	for _, baseline := range baselines {
		for _, knob := range matrixKnobs() {
			entry := baseline.entry
			knob.toggle(&entry, baseline.flipsTo)
			entry.name = fmt.Sprintf("%s--%s-%s", baseline.entry.name, knob.name, enabledSuffix(baseline.flipsTo))
			matrix = append(matrix, entry)
		}
	}
	return matrix
}

func enabledSuffix(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// templateValues renders the template values for one matrix entry, i.e. the exact view of the configuration that the
// collector configuration templates are evaluated against.
func (e *collectorConfigMatrixEntry) templateValues() *collectorConfigurationTemplateValues {
	return newCollectorConfigurationTemplateValues(
		&e.config,
		e.monitoredNamespaces,
		e.namespacesWithLogCollection,
		e.namespacesWithEventCollection,
		e.namespacesWithPrometheusScraping,
		e.filters,
		e.transforms,
		e.config.DaemonSetCollectorMemoryLimit,
		e.targetAllocatorMtlsConfig,
	)
}

// render renders all collector configurations for one matrix entry. The Signal Control collector configuration is
// only rendered when that collector is actually deployed for this configuration.
func (e *collectorConfigMatrixEntry) render() ([]renderedCollectorConfig, error) {
	config := e.config
	// The matrix is validated as plain YAML, so compression must stay off no matter what the entry configures.
	config.CompressConfigMap = false

	daemonSetConfigMap, err := assembleDaemonSetCollectorConfigMap(
		&config,
		e.monitoredNamespaces,
		e.namespacesWithLogCollection,
		e.namespacesWithPrometheusScraping,
		e.filters,
		e.transforms,
		e.targetAllocatorMtlsConfig,
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("cannot render the daemonset collector configuration for %s: %w", e.name, err)
	}
	// The feature gates have to match the ones assembleDaemonSetCollectorContainer passes to the collector.
	var daemonSetFeatureGates []string
	if config.ProfilingEnabled {
		daemonSetFeatureGates = append(daemonSetFeatureGates, "service.profilesSupport")
	}

	rendered := []renderedCollectorConfig{
		{
			name:         e.name + "--daemonset",
			content:      daemonSetConfigMap.Data[collectorConfigurationYaml],
			featureGates: daemonSetFeatureGates,
		},
	}

	// The cluster metrics collector deployment is only created when it has something to collect; the condition has to
	// match the one in assembleDesiredState, otherwise the matrix would validate a configuration that the operator
	// never deploys.
	if config.KubernetesInfrastructureMetricsCollectionEnabled || len(e.namespacesWithEventCollection) > 0 {
		deploymentConfigMap, err := assembleDeploymentCollectorConfigMap(
			&config,
			e.monitoredNamespaces,
			e.namespacesWithEventCollection,
			e.filters,
			e.transforms,
			false,
		)
		if err != nil {
			return nil, fmt.Errorf("cannot render the deployment collector configuration for %s: %w", e.name, err)
		}
		rendered = append(rendered, renderedCollectorConfig{
			name:    e.name + "--deployment",
			content: deploymentConfigMap.Data[collectorConfigurationYaml],
		})
	}

	if config.signalControlGatewayActive() {
		signalControlConfigMap, err := assembleSignalControlCollectorConfigMap(&config, e.monitoredNamespaces, false)
		if err != nil {
			return nil, fmt.Errorf("cannot render the Signal Control collector configuration for %s: %w", e.name, err)
		}
		rendered = append(rendered, renderedCollectorConfig{
			name:                   e.name + "--signalcontrol",
			content:                signalControlConfigMap.Data[collectorConfigurationYaml],
			signalControlCollector: true,
		})
	}

	return rendered, nil
}
