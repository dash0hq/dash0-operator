// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	_ "embed"
	"os"
	"slices"
	"strings"

	"go.opentelemetry.io/collector/pdata/pmetric"

	. "github.com/onsi/gomega"
)

const (
	metricsJsonMaxLineLength = 1_048_576
)

var (
	//go:embed kubeletstats_receiver_metrics.txt
	kubeletStatsReceiverMetricsFile string
	kubeletStatsReceiverMetricNames = parseMetricNameList(kubeletStatsReceiverMetricsFile)

	//go:embed k8s_cluster_receiver_metrics.txt
	k8sClusterReceiverMetricsFile string
	k8sClusterReceiverMetricNames = parseMetricNameList(k8sClusterReceiverMetricsFile)

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
		if serviceNamespace.Str() != "dash0.operator" {
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
		if isSet && serviceNamespace.Str() == "dash0.operator" {
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
