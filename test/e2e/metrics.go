// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"net/url"
	"strconv"
	"time"

	. "github.com/onsi/gomega"
)

type namespaceChecks struct {
	failOnNamespaceOtherThan    string
	failOnNamespaceScopedMetric bool
}

type metricsResourceMatchConfig struct {
	expectedDeploymentName string
	expectPodUid           bool
	namespaceChecks        namespaceChecks
}

var (
	deploymentMetricsMatchConfig = metricsResourceMatchConfig{
		expectedDeploymentName: "dash0-operator-nodejs-20-express-test-deployment",
		expectPodUid:           true,
		namespaceChecks: namespaceChecks{
			failOnNamespaceOtherThan: applicationUnderTestNamespace,
		},
	}

	workloadMetricsMatchConfig = metricsResourceMatchConfig{
		expectedDeploymentName: "",
		expectPodUid:           true,
		namespaceChecks: namespaceChecks{
			failOnNamespaceOtherThan: applicationUnderTestNamespace,
		},
	}

	nodeMetricsMatchConfig = metricsResourceMatchConfig{
		expectedDeploymentName: "",
		expectPodUid:           false,
		namespaceChecks: namespaceChecks{
			failOnNamespaceScopedMetric: true,
		},
	}

	matchAllConfig = metricsResourceMatchConfig{
		expectedDeploymentName: "",
		expectPodUid:           false,
		namespaceChecks:        namespaceChecks{},
	}
)

func verifyKubeletStatsMetrics(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		expectAtLeastOne,
		metricsMatchModeWorkload,
		deploymentMetricsMatchConfig,
		deploymentMetricsMatchConfig.namespaceChecks,
		timestampLowerBound,
		kubeletStatsReceiverMetricNameList,
	)
}

func verifyK8skClusterReceiverMetrics(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		expectAtLeastOne,
		metricsMatchModeWorkload,
		workloadMetricsMatchConfig,
		workloadMetricsMatchConfig.namespaceChecks,
		timestampLowerBound,
		k8sClusterReceiverMetricNameList,
	)
}

func verifyPrometheusMetrics(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		expectAtLeastOne,
		metricsMatchModeWorkload,
		deploymentMetricsMatchConfig,
		deploymentMetricsMatchConfig.namespaceChecks,
		timestampLowerBound,
		prometheusReceiverMetricNameList,
	)
}

func verifyNonNamespaceScopedKubeletStatsMetricsOnly(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		expectAtLeastOne,
		metricsMatchModeWorkload,
		nodeMetricsMatchConfig,
		nodeMetricsMatchConfig.namespaceChecks,
		timestampLowerBound,
		kubeletStatsReceiverMetricNameList,
	)
}

func verifyOperatorSelfMonitoringMetrics(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		expectAtLeastOne,
		metricsMatchModeSelfMonitoringOperatorManager,
		metricsResourceMatchConfig{},
		// This test runs with the same timestampLowerBound as other tests ("should produce node-based metrics via the
		// kubeletstats receiver", "should produce cluster metrics via the k8s_cluster receiver", "should produce
		// Prometheus metrics via the prometheus receiver", ...), hence we will have collected metrics from the
		// namespace under monitoring and non-namespaced metrics. In this test, we do not care about the forbidden
		// metrics check.
		namespaceChecks{},
		timestampLowerBound,
		"",
	)
}

func verifyCollectorSelfMonitoringMetrics(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		expectAtLeastOne,
		metricsMatchModeSelfMonitoringCollector,
		metricsResourceMatchConfig{},
		// This test runs with the same timestampLowerBound as other tests ("should produce node-based metrics via the
		// kubeletstats receiver", "should produce cluster metrics via the k8s_cluster receiver", "should produce
		// Prometheus metrics via the prometheus receiver", ...), hence we will have collected metrics from the
		// namespace under monitoring and non-namespaced metrics. In this test, we do not care about the forbidden
		// metrics check.
		namespaceChecks{},
		timestampLowerBound,
		"",
	)
}

func verifyNoMetricsAtAll(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		expectNoMatches,
		metricsMatchModeMatchAll,
		matchAllConfig,
		matchAllConfig.namespaceChecks,
		timestampLowerBound,
		"",
	)
}

// askTelemetryMatcherForMatchingMetrics executes an HTTP request to query telemetry-matcher to search through the
// telemetry captured in otlp-sink for a metric matching the given criteria.
func askTelemetryMatcherForMatchingMetrics(
	g Gomega,
	expectationMode expectationMode,
	metricsMatchMode metricsMatchMode,
	metricsMatchConfig metricsResourceMatchConfig,
	namespaceChecks namespaceChecks,
	timestampLowerBound time.Time,
	metricNameList metricNameList,
) {
	updateTelemetryMatcherUrlForKind()
	requestUrl := compileTelemetryMatcherUrlForMetrics(
		expectationMode,
		metricsMatchMode,
		metricsMatchConfig,
		namespaceChecks,
		timestampLowerBound,
		metricNameList,
	)
	executeTelemetryMatcherRequest(g, requestUrl)
}

func compileTelemetryMatcherUrlForMetrics(
	expectationMode expectationMode,
	metricsMatchMode metricsMatchMode,
	metricsMatchConfig metricsResourceMatchConfig,
	namespaceChecks namespaceChecks,
	timestampLowerBound time.Time,
	metricNameList metricNameList,
) string {
	baseUrl := fmt.Sprintf("%s/matching-metrics", telemetryMatcherBaseUrl)
	params := url.Values{}
	params.Add(queryParamExpectationMode, string(expectationMode))
	params.Add(queryParamMetricsMatchMode, string(metricsMatchMode))
	if metricsMatchConfig.expectedDeploymentName != "" {
		params.Add(queryParamDeploymentName, metricsMatchConfig.expectedDeploymentName)
	}
	params.Add(queryParamExpectPodUid, strconv.FormatBool(metricsMatchConfig.expectPodUid))
	if namespaceChecks.failOnNamespaceOtherThan != "" {
		params.Add(queryParamFailOnNamespaceOtherThan, namespaceChecks.failOnNamespaceOtherThan)
	}
	params.Add(queryParamFailOnNamespaceScopedMetric, strconv.FormatBool(namespaceChecks.failOnNamespaceScopedMetric))
	// Note: Using the name of the Kubernetes context as the cluster name match parameter works because we set this
	// as the clusterName setting for the operator configuration resource when doing helm install or creating the
	// operator configuration resource.
	params.Add(queryParamClusterName, e2eKubernetesContext)
	params.Add(queryParamOperatorNamespace, operatorNamespace)
	params.Add(queryParamTimestampLowerBoundStr, strconv.FormatInt(timestampLowerBound.UnixMilli(), 10))
	if metricNameList != "" {
		params.Add(queryParamMetricNameList, string(metricNameList))
	}
	requestUrl := baseUrl + "?" + params.Encode()
	return requestUrl
}
