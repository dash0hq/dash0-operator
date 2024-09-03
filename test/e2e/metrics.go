// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"os"
	"strings"

	"go.opentelemetry.io/collector/pdata/pmetric"

	. "github.com/onsi/gomega"
)

const (
	metricsJsonMaxLineLength = 1_048_576
)

var (
	metricsUnmarshaller = &pmetric.JSONUnmarshaler{}
)

//nolint:all
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
		if resourceMatchFn != nil {
			if !resourceMatchFn(resourceMetric) {
				continue
			}
		}

		for j := 0; j < resourceMetric.ScopeMetrics().Len(); j++ {
			scopeMetric := resourceMetric.ScopeMetrics().At(j)
			for k := 0; k < scopeMetric.Metrics().Len(); k++ {
				metric := scopeMetric.Metrics().At(k)
				if metricMatchFn(metric) {
					return true
				}
			}
		}
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
