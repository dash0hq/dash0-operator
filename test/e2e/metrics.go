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

	"github.com/dash0hq/dash0-operator/test/e2e/pkg/shared"
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
		shared.ExpectAtLeastOne,
		shared.MetricsMatchModeWorkload,
		deploymentMetricsMatchConfig,
		deploymentMetricsMatchConfig.namespaceChecks,
		timestampLowerBound,
		shared.KubeletStatsReceiverMetricNameList,
	)
}

func verifyK8skClusterReceiverMetrics(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		shared.ExpectAtLeastOne,
		shared.MetricsMatchModeWorkload,
		workloadMetricsMatchConfig,
		workloadMetricsMatchConfig.namespaceChecks,
		timestampLowerBound,
		shared.K8sClusterReceiverMetricNameList,
	)
}

func verifyPrometheusMetrics(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		shared.ExpectAtLeastOne,
		shared.MetricsMatchModeWorkload,
		deploymentMetricsMatchConfig,
		deploymentMetricsMatchConfig.namespaceChecks,
		timestampLowerBound,
		shared.PrometheusReceiverMetricNameList,
	)
}

func verifyPrometheusMetricsIgnoreNamespaceChecks(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		shared.ExpectAtLeastOne,
		shared.MetricsMatchModeWorkload,
		deploymentMetricsMatchConfig,
		namespaceChecks{},
		timestampLowerBound,
		shared.PrometheusReceiverMetricNameList,
	)
}

func verifyNonNamespaceScopedKubeletStatsMetricsOnly(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		shared.ExpectAtLeastOne,
		shared.MetricsMatchModeWorkload,
		nodeMetricsMatchConfig,
		nodeMetricsMatchConfig.namespaceChecks,
		timestampLowerBound,
		shared.KubeletStatsReceiverMetricNameList,
	)
}

func verifyOperatorSelfMonitoringMetrics(g Gomega, timestampLowerBound time.Time) {
	askTelemetryMatcherForMatchingMetrics(
		g,
		shared.ExpectAtLeastOne,
		shared.MetricsMatchModeSelfMonitoringOperatorManager,
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
		shared.ExpectAtLeastOne,
		shared.MetricsMatchModeSelfMonitoringCollector,
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
		shared.ExpectNoMatches,
		shared.MetricsMatchModeMatchAll,
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
	expectationMode shared.ExpectationMode,
	metricsMatchMode shared.MetricsMatchMode,
	metricsMatchConfig metricsResourceMatchConfig,
	namespaceChecks namespaceChecks,
	timestampLowerBound time.Time,
	metricNameList shared.MetricNameList,
) {
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
	expectationMode shared.ExpectationMode,
	metricsMatchMode shared.MetricsMatchMode,
	metricsMatchConfig metricsResourceMatchConfig,
	namespaceChecks namespaceChecks,
	timestampLowerBound time.Time,
	metricNameList shared.MetricNameList,
) string {
	baseUrl := fmt.Sprintf("%s/matching-metrics", telemetryMatcherBaseUrl)
	params := url.Values{}
	params.Add(shared.QueryParamExpectationMode, string(expectationMode))
	params.Add(shared.QueryParamMetricsMatchMode, string(metricsMatchMode))
	if metricsMatchConfig.expectedDeploymentName != "" {
		params.Add(shared.QueryParamDeploymentName, metricsMatchConfig.expectedDeploymentName)
	}
	params.Add(shared.QueryParamExpectPodUid, strconv.FormatBool(metricsMatchConfig.expectPodUid))
	if namespaceChecks.failOnNamespaceOtherThan != "" {
		params.Add(shared.QueryParamFailOnNamespaceOtherThan, namespaceChecks.failOnNamespaceOtherThan)
	}
	params.Add(
		shared.QueryParamFailOnNamespaceScopedMetric,
		strconv.FormatBool(namespaceChecks.failOnNamespaceScopedMetric),
	)
	// Note: Using the name of the Kubernetes context as the cluster name match parameter works because we set this
	// as the clusterName setting for the operator configuration resource when doing helm install or creating the
	// operator configuration resource.
	params.Add(shared.QueryParamClusterName, e2eKubernetesContext)
	params.Add(shared.QueryParamOperatorNamespace, operatorNamespace)
	params.Add(shared.QueryParamTimestampLowerBoundStr, strconv.FormatInt(timestampLowerBound.UnixMilli(), 10))
	if metricNameList != "" {
		params.Add(shared.QueryParamMetricNameList, string(metricNameList))
	}
	requestUrl := baseUrl + "?" + params.Encode()
	return requestUrl
}
