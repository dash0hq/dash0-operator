// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type resourceMatcher func(pmetric.ResourceMetrics, *ResourceMatchResult[pmetric.ResourceMetrics])

type metricMatcher func(pmetric.Metric, *ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric])

const (
	metricsJsonMaxLineLength = 1_048_576

	operatorServiceNamespace = "dash0-operator"

	namespaceNameKey    = "k8s.namespace.name"
	deploymentNameKey   = "k8s.deployment.name"
	serviceNameKey      = "service.name"
	serviceNamespaceKey = "service.namespace"
	serviceVersionKey   = "service.version"
	nodeNameKey         = "k8s.node.name"
	podUidKey           = "k8s.pod.uid"
	metricNameKey       = "metric.name"
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

//nolint:dupl
func readFileAndGetMatchingMetrics(
	metricsJsonlFilename string,
	resourceMatchFn resourceMatcher,
	metricMatchFn metricMatcher,
	failOnNamespaceOtherThan string,
	failOnNamespaceScopedMetric bool,
	timestampLowerBound time.Time,
) (*MatchResultList[pmetric.ResourceMetrics, pmetric.Metric], error) {
	fileHandle, err := os.Open(metricsJsonlFilename)
	if err != nil {
		return nil, fmt.Errorf("cannot open file %s: %v", metricsJsonlFilename, err)
	}
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, metricsJsonMaxLineLength), metricsJsonMaxLineLength)

	matchResults := newMatchResultList[pmetric.ResourceMetrics, pmetric.Metric]()

	// read file line by line, each line can contain multiple resources metrics, each with multiple scope metrics and
	// actual metrics
	for scanner.Scan() {
		resourceMetricBytes := scanner.Bytes()
		metrics, err := metricsUnmarshaller.UnmarshalMetrics(resourceMetricBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		if matchAssertionError := extractMatchingMetrics(
			metrics,
			resourceMatchFn,
			metricMatchFn,
			failOnNamespaceOtherThan,
			failOnNamespaceScopedMetric,
			timestampLowerBound,
			&matchResults,
		); matchAssertionError != nil {
			return nil, matchAssertionError
		}
		if matchResults.hasMatch() {
			break
		}
	}

	if scanner.Err() != nil {
		return nil, fmt.Errorf("error while scanning file %s: %v", metricsJsonlFilename, err)
	}

	return &matchResults, nil
}

func extractMatchingMetrics(
	metrics pmetric.Metrics,
	resourceMatchFn resourceMatcher,
	metricMatchFn metricMatcher,
	failOnNamespaceOtherThan string,
	failOnNamespaceScopedMetric bool,
	timestampLowerBound time.Time,
	allMatchResults *MatchResultList[pmetric.ResourceMetrics, pmetric.Metric],
) *MatchAssertionError {
	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		resourceMetrics := metrics.ResourceMetrics().At(i)
		if err := checkForForbidenResourceAttributes(
			resourceMetrics,
			failOnNamespaceOtherThan,
			failOnNamespaceScopedMetric,
			timestampLowerBound,
		); err != nil {
			return err
		}
		resourceMatchResult := newResourceMatchResult(resourceMetrics)
		resourceMatchFn(resourceMetrics, &resourceMatchResult)

		for j := 0; j < resourceMetrics.ScopeMetrics().Len(); j++ {
			scopeMetric := resourceMetrics.ScopeMetrics().At(j)
			for k := 0; k < scopeMetric.Metrics().Len(); k++ {
				metric := scopeMetric.Metrics().At(k)
				metricMatchResult := newObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric](
					metric.Name(),
					resourceMetrics,
					resourceMatchResult,
					metric,
				)
				mostRecentDataPoint, err := getTimestampOfMostRecentDataPoint(metric)
				if err != nil {
					return err
				}
				if !mostRecentDataPoint.After(timestampLowerBound) {
					// This metric and its data points are too old, it is probably from a previously running test case,
					// ignore it.
					continue
				}

				metricMatchFn(metric, &metricMatchResult)
				allMatchResults.addResultForObject(metricMatchResult)
			}
		}
	}
	return nil
}

func checkForForbidenResourceAttributes(
	resourceMetrics pmetric.ResourceMetrics,
	failOnNamespaceOtherThan string,
	failOnNamespaceScopedMetric bool,
	timestampLowerBound time.Time,
) *MatchAssertionError {
	mostRecentTimestamp, err := getMostRecentTimestampOfAnyMetricDataPoint(resourceMetrics)
	if err != nil {
		return err
	}

	if mostRecentTimestamp.Before(timestampLowerBound) {
		// These metrics and their data points are too old, they are probably from a previously running test case,
		// ignore them.
		return nil
	}

	attributes := resourceMetrics.Resource().Attributes()
	namespace, namespaceIsSet := attributes.Get(namespaceNameKey)
	if namespaceIsSet {
		// Make sure we only collect metrics from monitored namespaces (or only non-namespace-scoped metrics if no
		// namespace is monitored). If the metric has a namespace resource attribute, it needs to be the only
		// namespaces that has a Dash0Monitoring resource. We allow metrics that do not have any namespace resource
		// attribute, like all node-related metrics.
		metricName := "(unknown metric name)"
		scopeMetrics := resourceMetrics.ScopeMetrics()
		if scopeMetrics.Len() > 0 {
			metricsFromArbitraryScope := scopeMetrics.At(0).Metrics()
			if metricsFromArbitraryScope.Len() > 0 {
				metricName = metricsFromArbitraryScope.At(0).Name()
			}
		}
		if failOnNamespaceOtherThan != "" {
			if namespace.Str() != failOnNamespaceOtherThan {
				return newMatchAssertionError(
					"found at least one metric (%s) from a non-monitored namespace (%s); the operator's "+
						"collectors should only collect metrics from monitored namespaces and metrics that are not "+
						"namespace-scoped (like Kubernetes node metrics)",
					metricName,
					namespace.Str())
			}
		}
		if failOnNamespaceScopedMetric {
			return newMatchAssertionError(
				"found at least one metric (%s) that has k8s.namespace.name set (%s); the operator's "+
					"collectors in the current configuration should only collect metrics which are not "+
					"namespace-scoped (like Kubernetes node metrics)",
				metricName,
				namespace.Str())
		}
	}
	return nil
}

func resourceAttributeMatcherWorkloads(expectedDeploymentName string, expectPodUid bool) resourceMatcher {
	return func(resourceMetrics pmetric.ResourceMetrics, matchResult *ResourceMatchResult[pmetric.ResourceMetrics]) {
		attributes := resourceMetrics.Resource().Attributes()
		if expectedDeploymentName != "" {
			actualDeploymentName, deploymentNameIsSet := attributes.Get(deploymentNameKey)
			if !deploymentNameIsSet {
				matchResult.addFailedAssertion(
					deploymentNameKey,
					fmt.Sprintf(
						"expected %s but the metric has no such resource attribute",
						expectedDeploymentName,
					),
				)
			} else if actualDeploymentName.Str() != expectedDeploymentName {
				matchResult.addFailedAssertion(
					deploymentNameKey,
					fmt.Sprintf(
						"expected %s but it was %s",
						expectedDeploymentName,
						actualDeploymentName.Str(),
					),
				)
			} else {
				matchResult.addPassedAssertion(deploymentNameKey)
			}
		}

		serviceNamespace, serviceNamespaceIsSet := attributes.Get(serviceNamespaceKey)
		if serviceNamespaceIsSet && serviceNamespace.Str() == operatorServiceNamespace {
			// make sure we do not accidentally set self-monitoring related resource attributes on other resources
			matchResult.addFailedAssertion(
				serviceNamespaceKey,
				fmt.Sprintf("expected %s but it was %s", operatorServiceNamespace, serviceNamespace.Str()),
			)
		} else {
			matchResult.addPassedAssertion(serviceNamespaceKey)
		}

		_, k8sNodeNameIsSet := attributes.Get(nodeNameKey)
		if !k8sNodeNameIsSet {
			matchResult.addFailedAssertion(
				nodeNameKey,
				"expected attribute to be set, but the metric has no such resource attribute",
			)
		} else {
			matchResult.addPassedAssertion(nodeNameKey)
		}

		checkPodUid(attributes, expectPodUid, matchResult)
	}
}

func checkPodUid(attributes pcommon.Map, expectPodUid bool, matchResult *ResourceMatchResult[pmetric.ResourceMetrics]) {
	k8sPodUid, k8sPodUidIsSet := attributes.Get(podUidKey)
	if expectPodUid && !k8sPodUidIsSet {
		matchResult.addFailedAssertion(
			podUidKey,
			"expected attribute to be set, but the metric has no such resource attribute",
		)
	} else if !expectPodUid && k8sPodUidIsSet {
		matchResult.addFailedAssertion(
			podUidKey,
			fmt.Sprintf("expected attribute to not be set, but it was set to %s", k8sPodUid.Str()),
		)
	} else {
		matchResult.addPassedAssertion(podUidKey)
	}
}

func metricNameIsMemberOfList(metricNameList []string) func(
	pmetric.Metric,
	*ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric],
) {
	return func(metric pmetric.Metric, matchResult *ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric]) {
		actualMetricName := metric.Name()
		if slices.Contains(metricNameList, actualMetricName) {
			matchResult.addPassedAssertion(metricNameKey)
		} else {
			matchResult.addFailedAssertion(
				metricNameKey,
				fmt.Sprintf("the metric name did not match the list of expected metric names, it was %s", actualMetricName),
			)
		}
	}
}

func resourceAttributeMatcherSelfMonitoringOperatorManager(operatorNamespace string) resourceMatcher {
	return func(resourceMetrics pmetric.ResourceMetrics, matchResult *ResourceMatchResult[pmetric.ResourceMetrics]) {
		attributes := resourceMetrics.Resource().Attributes()
		var isSet bool

		serviceNamespace, isSet := attributes.Get(serviceNamespaceKey)
		if !isSet {
			matchResult.addFailedAssertion(
				serviceNamespaceKey,
				fmt.Sprintf("expected %s but the metric has no such resource attribute", operatorServiceNamespace),
			)
		} else if serviceNamespace.Str() != operatorServiceNamespace {
			matchResult.addFailedAssertion(
				serviceNamespaceKey,
				fmt.Sprintf("expected %s but it was %s", operatorServiceNamespace, serviceNamespace.Str()),
			)
		} else {
			matchResult.addPassedAssertion(serviceNamespaceKey)
		}

		kubernetesNamespace, isSet := attributes.Get(namespaceNameKey)
		if isSet && kubernetesNamespace.Str() != operatorNamespace {
			matchResult.addFailedAssertion(
				namespaceNameKey,
				fmt.Sprintf("expected %s but it was %s", operatorNamespace, kubernetesNamespace.Str()),
			)
		} else {
			// We are deliberately not requesting the namespace to be set to produce a match; the self-monitoring
			// telemetry does not go through the k8sattributes processor, in fact it does neither go through the
			// daemonset collector nor the cluster metrics collector, instead it is sent directly to the export
			// endpoint, hence it does not have extended Kubernetes resource attributes attached.
			matchResult.addPassedAssertion(namespaceNameKey)
		}

		_, isSet = attributes.Get(serviceNameKey)
		if !isSet {
			matchResult.addFailedAssertion(
				serviceNameKey,
				"expected attribute to be set, but the metric has no such resource attribute",
			)
		} else {
			matchResult.addPassedAssertion(serviceNameKey)
		}

		_, isSet = attributes.Get(serviceVersionKey)
		if !isSet {
			matchResult.addFailedAssertion(
				serviceVersionKey,
				"expected attribute to be set, but the metric has no such resource attribute",
			)
		} else {
			matchResult.addPassedAssertion(serviceVersionKey)
		}

		_, isSet = attributes.Get(nodeNameKey)
		if !isSet {
			matchResult.addFailedAssertion(
				nodeNameKey,
				"expected attribute to be set, but the metric has no such resource attribute",
			)
		} else {
			matchResult.addPassedAssertion(nodeNameKey)
		}

		checkPodUid(attributes, true, matchResult)
	}
}

func resourceAttributeMatcherSelfMonitoringCollector(operatorNamespace string) resourceMatcher {
	return func(resourceMetrics pmetric.ResourceMetrics, matchResult *ResourceMatchResult[pmetric.ResourceMetrics]) {
		attributes := resourceMetrics.Resource().Attributes()
		var isSet bool

		serviceNamespace, isSet := attributes.Get(serviceNamespaceKey)
		if !isSet {
			matchResult.addFailedAssertion(
				serviceNamespaceKey,
				fmt.Sprintf("expected %s but the metric has no such resource attribute", operatorServiceNamespace),
			)
		} else if serviceNamespace.Str() != operatorServiceNamespace {
			matchResult.addFailedAssertion(
				serviceNamespaceKey,
				fmt.Sprintf("expected %s but it was %s", operatorServiceNamespace, serviceNamespace.Str()),
			)
		} else {
			matchResult.addPassedAssertion(serviceNamespaceKey)
		}

		kubernetesNamespace, isSet := attributes.Get(namespaceNameKey)
		if !isSet {
			matchResult.addFailedAssertion(
				namespaceNameKey,
				fmt.Sprintf("expected %s but the metric has no such resource attribute", operatorNamespace),
			)
		} else if kubernetesNamespace.Str() != operatorNamespace {
			matchResult.addFailedAssertion(
				namespaceNameKey,
				fmt.Sprintf("expected %s but it was %s", operatorNamespace, kubernetesNamespace.Str()),
			)
		} else {
			matchResult.addPassedAssertion(namespaceNameKey)
		}

		_, isSet = attributes.Get(serviceNameKey)
		if !isSet {
			matchResult.addFailedAssertion(
				serviceNameKey,
				"expected attribute to be set, but the metric has no such resource attribute",
			)
		} else {
			matchResult.addPassedAssertion(serviceNameKey)
		}

		_, isSet = attributes.Get(serviceVersionKey)
		if !isSet {
			matchResult.addFailedAssertion(
				serviceVersionKey,
				"expected attribute to be set, but the metric has no such resource attribute",
			)
		} else {
			matchResult.addPassedAssertion(serviceVersionKey)
		}

		_, isSet = attributes.Get(nodeNameKey)
		if !isSet {
			matchResult.addFailedAssertion(
				nodeNameKey,
				"expected attribute to be set, but the metric has no such resource attribute",
			)
		} else {
			matchResult.addPassedAssertion(nodeNameKey)
		}

		checkPodUid(attributes, true, matchResult)
	}
}

func hasDash0OperatorPrefixMatcher() metricMatcher {
	return func(metric pmetric.Metric, matchResult *ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric]) {
		actualMetricName := metric.Name()
		if strings.HasPrefix(metric.Name(), "dash0.operator.") {
			matchResult.addPassedAssertion(metricNameKey)
		} else {
			matchResult.addFailedAssertion(
				metricNameKey,
				fmt.Sprintf("the metric name did not start with \"dash0.operator.\", it was %s", actualMetricName),
			)
		}
	}
}

func hasOtelColPrefixMatcher() metricMatcher {
	return func(metric pmetric.Metric, matchResult *ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric]) {
		actualMetricName := metric.Name()
		if strings.HasPrefix(metric.Name(), "otelcol_") {
			matchResult.addPassedAssertion(metricNameKey)
		} else {
			matchResult.addFailedAssertion(
				metricNameKey,
				fmt.Sprintf("the metric name did not start with \"otelcol_\", it was %s", actualMetricName),
			)
		}
	}
}

func matchAllResourceAttributeMatcher() resourceMatcher {
	return func(_ pmetric.ResourceMetrics, matchResult *ResourceMatchResult[pmetric.ResourceMetrics]) {
		matchResult.addSkippedAssertion("no-op", "this matcher does not check any resource attributes")
	}
}

func matchAllMetricMatcher() func(
	pmetric.Metric,
	*ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric],
) {
	return func(_ pmetric.Metric, matchResult *ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric]) {
		matchResult.addSkippedAssertion("no-op", "this matcher does not check any metric attributes")
	}
}

func getMostRecentTimestampOfAnyMetricDataPoint(
	resourceMetrics pmetric.ResourceMetrics,
) (time.Time, *MatchAssertionError) {
	mostRecentTimestamp := time.Unix(0, 0)
	foundAtLeastOneTimestamp := false

	for j := 0; j < resourceMetrics.ScopeMetrics().Len(); j++ {
		scopeMetric := resourceMetrics.ScopeMetrics().At(j)
		for k := 0; k < scopeMetric.Metrics().Len(); k++ {
			metric := scopeMetric.Metrics().At(k)
			mostRecentTimestampFromMetric, err := getTimestampOfMostRecentDataPoint(metric)
			if err != nil {
				return time.Time{}, err
			}
			if mostRecentTimestampFromMetric.After(mostRecentTimestamp) {
				mostRecentTimestamp = mostRecentTimestampFromMetric
				foundAtLeastOneTimestamp = true
			}
		}
	}
	if !foundAtLeastOneTimestamp {
		return time.Time{}, newMatchAssertionError("no metric with any data point with a time stamp found")
	}
	return mostRecentTimestamp, nil
}

func getTimestampOfMostRecentDataPoint(metric pmetric.Metric) (time.Time, *MatchAssertionError) {
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		return getMostRecentTimestamp(metric.Gauge().DataPoints())
	case pmetric.MetricTypeSum:
		return getMostRecentTimestamp(metric.Sum().DataPoints())
	case pmetric.MetricTypeHistogram:
		return getMostRecentTimestampFromHistogram(metric.Histogram().DataPoints())
	case pmetric.MetricTypeExponentialHistogram:
		return getMostRecentTimestampFromExponentialHistogram(metric.ExponentialHistogram().DataPoints())
	case pmetric.MetricTypeSummary:
		return getMostRecentTimestampFromSummary(metric.Summary().DataPoints())
	case pmetric.MetricTypeEmpty:
		return time.Time{}, newMatchAssertionError("unexpected metric type: empty")
	default:
		return time.Time{}, newMatchAssertionError("unknown metric type: %s", metric.Type().String())
	}
}

func getMostRecentTimestamp(dataPoints pmetric.NumberDataPointSlice) (time.Time, *MatchAssertionError) {
	if dataPoints.Len() == 0 {
		return time.Time{}, newMatchAssertionError("no metric data points found")
	}
	mostRecentTimestamp := time.Unix(0, 0)
	foundAtLeastOneTimestamp := false
	for i := 0; i < dataPoints.Len(); i++ {
		dataPointTimestamp := dataPoints.At(i).Timestamp().AsTime()
		if dataPointTimestamp.After(mostRecentTimestamp) {
			mostRecentTimestamp = dataPointTimestamp
			foundAtLeastOneTimestamp = true
		}
	}
	if !foundAtLeastOneTimestamp {
		return time.Time{}, newMatchAssertionError("no metric data point with time stamp found")
	}
	return mostRecentTimestamp, nil
}

func getMostRecentTimestampFromHistogram(dataPoints pmetric.HistogramDataPointSlice) (time.Time, *MatchAssertionError) {
	if dataPoints.Len() == 0 {
		return time.Time{}, newMatchAssertionError("no metric data points found")
	}
	t := time.Unix(0, 0)
	foundAtLeastOneTimestamp := false
	for i := 0; i < dataPoints.Len(); i++ {
		ts := dataPoints.At(i).Timestamp().AsTime()
		if ts.After(t) {
			t = ts
			foundAtLeastOneTimestamp = true
		}
	}
	if !foundAtLeastOneTimestamp {
		return time.Time{}, newMatchAssertionError("no metric data point with time stamp found")
	}
	return t, nil
}

func getMostRecentTimestampFromExponentialHistogram(
	dataPoints pmetric.ExponentialHistogramDataPointSlice,
) (time.Time, *MatchAssertionError) {
	if dataPoints.Len() == 0 {
		return time.Time{}, newMatchAssertionError("no metric data points found")
	}
	t := time.Unix(0, 0)
	foundAtLeastOneTimestamp := false
	for i := 0; i < dataPoints.Len(); i++ {
		ts := dataPoints.At(i).Timestamp().AsTime()
		if ts.After(t) {
			t = ts
			foundAtLeastOneTimestamp = true
		}
	}
	if !foundAtLeastOneTimestamp {
		return time.Time{}, newMatchAssertionError("no metric data point with time stamp found")
	}
	return t, nil
}

func getMostRecentTimestampFromSummary(dataPoints pmetric.SummaryDataPointSlice) (time.Time, *MatchAssertionError) {
	if dataPoints.Len() == 0 {
		return time.Time{}, newMatchAssertionError("no metric data points found")
	}
	t := time.Unix(0, 0)
	foundAtLeastOneTimestamp := false
	for i := 0; i < dataPoints.Len(); i++ {
		ts := dataPoints.At(i).Timestamp().AsTime()
		if ts.After(t) {
			t = ts
			foundAtLeastOneTimestamp = true
		}
	}
	if !foundAtLeastOneTimestamp {
		return time.Time{}, newMatchAssertionError("no metric data point with time stamp found")
	}
	return t, nil
}

func parseMetricNameList(metricNameListRaw string) []string {
	return strings.Split(metricNameListRaw, "\n")
}
