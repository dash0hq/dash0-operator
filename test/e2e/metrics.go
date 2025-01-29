// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"slices"
	"strings"

	"go.opentelemetry.io/collector/pdata/pmetric"

	. "github.com/onsi/gomega"
)

const (
	metricsJsonMaxLineLength = 1_048_576

	operatorServiceNamespace = "dash0.operator"
)

var (
	//go:embed kubeletstats_receiver_metrics.txt
	kubeletStatsReceiverMetricsFile string
	kubeletStatsReceiverMetricNames = parseMetricNameList(kubeletStatsReceiverMetricsFile)

	//go:embed k8s_cluster_receiver_metrics.txt
	k8sClusterReceiverMetricsFile string
	k8sClusterReceiverMetricNames = parseMetricNameList(k8sClusterReceiverMetricsFile)

	//go:embed prometheus_receiver_metrics.txt
	prometheusReceiverMetricsFile string
	prometheusReceiverMetricNames = parseMetricNameList(prometheusReceiverMetricsFile)

	metricsUnmarshaller = &pmetric.JSONUnmarshaler{}
)

func verifyKubeletStatsMetrics(g Gomega) {
	g.Expect(
		fileHasMatchingMetrics(
			g,
			resourceAttributeMatcher("dash0-operator-nodejs-20-express-test-deployment"),
			metricNameMatcher(kubeletStatsReceiverMetricNames),
		),
	).To(BeTrue(), "expected to find at least one kubeletstat receiver metric")
}

func verifyK8skClusterReceiverMetrics(g Gomega) {
	g.Expect(
		fileHasMatchingMetrics(
			g,
			resourceAttributeMatcher(""),
			metricNameMatcher(k8sClusterReceiverMetricNames),
		),
	).To(BeTrue(), "expected to find at least one k8s_cluster receiver metric")
}

func verifyPrometheusMetrics(g Gomega) {
	g.Expect(
		fileHasMatchingMetrics(
			g,
			resourceAttributeMatcher(""),
			metricNameMatcher(prometheusReceiverMetricNames),
		),
	).To(BeTrue(), "expected to find at least one Prometheus receiver metric")
}

func fileHasMatchingMetrics(
	g Gomega,
	resourceMatchFn func(metric pmetric.ResourceMetrics) bool,
	metricMatchFn func(metric pmetric.Metric) bool,
) bool {
	fileHandle, err := os.Open("test-resources/e2e-test-volumes/otlp-sink/metrics.jsonl")
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, metricsJsonMaxLineLength), metricsJsonMaxLineLength)

	// read file line by line
	metricsFound := false
	for scanner.Scan() {
		resourceMetricBytes := scanner.Bytes()
		metrics, err := metricsUnmarshaller.UnmarshalMetrics(resourceMetricBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		if metricsFound = hasMatchingMetrics(
			metrics,
			resourceMatchFn,
			metricMatchFn,
		); metricsFound {
			break
		}
	}

	g.Expect(scanner.Err()).NotTo(HaveOccurred())

	return metricsFound
}

//nolint:all
func hasMatchingMetrics(
	metrics pmetric.Metrics,
	resourceMatchFn func(metric pmetric.ResourceMetrics) bool,
	metricMatchFn func(metric pmetric.Metric) bool,
) bool {
	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		resourceMetric := metrics.ResourceMetrics().At(i)

		resourceMetricHasMetchingMetricName := false

		for j := 0; j < resourceMetric.ScopeMetrics().Len(); j++ {
			scopeMetric := resourceMetric.ScopeMetrics().At(j)
			for k := 0; k < scopeMetric.Metrics().Len(); k++ {
				metric := scopeMetric.Metrics().At(k)
				if metricMatchFn(metric) {
					resourceMetricHasMetchingMetricName = true
					break
				}
			}
			if resourceMetricHasMetchingMetricName {
				break
			}
		}

		if !resourceMetricHasMetchingMetricName {
			// This resource metric had no metrics with any of the metrics name we are looking for, continue with the
			// next resource metric.
			continue
		}

		if !resourceMatchFn(resourceMetric) {
			// This resource had at least one matching metric name, but the resource attributes do not match, continue
			// with the next resource metric.
			continue
		}

		// We have found a metric where metric attributes and resource attributes match.
		return true
	}

	return false
}

func verifySelfMonitoringMetrics(g Gomega) {
	resourceMatchFn := func(resourceMetrics pmetric.ResourceMetrics) bool {
		attributes := resourceMetrics.Resource().Attributes()
		var isSet bool

		serviceNamespace, isSet := attributes.Get("service.namespace")
		if !isSet {
			return false
		}
		if serviceNamespace.Str() != operatorServiceNamespace {
			return false
		}
		kubernetesNamespace, isSet := attributes.Get("k8s.namespace.name")
		if isSet && kubernetesNamespace.Str() != operatorNamespace {
			return false
		}
		_, isSet = attributes.Get("service.name")
		if !isSet {
			return false
		}
		_, isSet = attributes.Get("service.version")
		if !isSet {
			return false
		}
		_, isSet = attributes.Get("k8s.node.name")
		if !isSet {
			return false
		}
		_, isSet = attributes.Get("k8s.pod.uid")

		return isSet
	}
	metricMatchFn := func(metric pmetric.Metric) bool {
		return strings.HasPrefix(metric.Name(), "dash0.operator.")
	}

	selfMonitoringMetricsFound := fileHasMatchingMetrics(
		g,
		resourceMatchFn,
		metricMatchFn,
	)
	g.Expect(selfMonitoringMetricsFound).To(
		BeTrue(),
		"expected to find at least one matching self-monitoring metric",
	)
}

func resourceAttributeMatcher(expectedDeploymentName string) func(resourceMetrics pmetric.ResourceMetrics) bool {
	return func(resourceMetrics pmetric.ResourceMetrics) bool {
		attributes := resourceMetrics.Resource().Attributes()
		var isSet bool

		namespace, isSet := attributes.Get("k8s.namespace.name")
		if isSet {
			// Make sure we only collect metrics from monitored namespaces. If the metric has a namespace resource
			// attribute, it needs to be the only namespaces that has a Dash0Monitoring resource. We allow metrics
			// that do not have any namespace resource attribute, like all node-related metrics.
			//.
			// Deliberately not returning false here, but instead calling Expect directly, and _not_ the Gomega's
			// instance g.Expect of the surrounding Eventually function, to make the test fail immediately.
			metricName := "(unknown metric name)"
			scopeMetrics := resourceMetrics.ScopeMetrics()
			if scopeMetrics.Len() > 0 {
				metricsFromArbitraryScope := scopeMetrics.At(0).Metrics()
				if metricsFromArbitraryScope.Len() > 0 {
					metricName = metricsFromArbitraryScope.At(0).Name()
				}
			}
			Expect(namespace.Str()).To(Equal(applicationUnderTestNamespace),
				fmt.Sprintf("Found at least one metric (%s) from a non-monitored namespace (%s); the operator's "+
					"collectors should only collect metrics from monitored namespaces and metrics that are not "+
					"namespace-scoped (like Kubernetes node metrics).",
					metricName,
					namespace.Str()),
			)
		}

		if expectedDeploymentName != "" {
			deploymentName, isSet := attributes.Get("k8s.deployment.name")
			if !isSet {
				return false
			}
			if deploymentName.Str() != expectedDeploymentName {
				return false
			}
		}

		serviceNamespace, isSet := attributes.Get("service.namespace")
		if isSet && serviceNamespace.Str() == operatorServiceNamespace {
			// make sure we do not accidentally set self-monitoring related resource attributes on other resources
			return false
		}
		_, isSet = attributes.Get("k8s.node.name")
		if !isSet {
			return false
		}
		_, isSet = attributes.Get("k8s.pod.uid")

		return isSet
	}
}

func metricNameMatcher(metricNameList []string) func(metric pmetric.Metric) bool {
	return func(metric pmetric.Metric) bool {
		return slices.Contains(metricNameList, metric.Name())
	}
}

func parseMetricNameList(metricNameListRaw string) []string {
	return strings.Split(metricNameListRaw, "\n")
}
