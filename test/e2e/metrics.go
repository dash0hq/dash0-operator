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

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	. "github.com/onsi/gomega"
)

type metricsResourceMatchConfig struct {
	expectedDeploymentName      string
	expectPodUid                bool
	allowNamespaceScopedMetrics bool
}

const (
	metricsJsonMaxLineLength = 1_048_576

	operatorServiceNamespace = "dash0.operator"

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

	deploymentMetricsMatchConfig = metricsResourceMatchConfig{
		expectedDeploymentName:      "dash0-operator-nodejs-20-express-test-deployment",
		expectPodUid:                true,
		allowNamespaceScopedMetrics: true,
	}

	workloadMetricsMatchConfig = metricsResourceMatchConfig{
		expectedDeploymentName:      "",
		expectPodUid:                true,
		allowNamespaceScopedMetrics: true,
	}

	nodeMetricsMatchConfig = metricsResourceMatchConfig{
		expectedDeploymentName:      "",
		expectPodUid:                false,
		allowNamespaceScopedMetrics: false,
	}
)

func verifyKubeletStatsMetrics(g Gomega) {
	allMatchResults := fileHasMatchingMetrics(
		g,
		resourceAttributeMatcher(deploymentMetricsMatchConfig),
		metricNameIsMemberOfList(kubeletStatsReceiverMetricNames),
	)
	allMatchResults.expectAtLeastOneMatch(
		g,
		"expected to find at least one kubeletstat receiver metric",
	)
}

func verifyK8skClusterReceiverMetrics(g Gomega) {
	allMatchResults := fileHasMatchingMetrics(
		g,
		resourceAttributeMatcher(workloadMetricsMatchConfig),
		metricNameIsMemberOfList(k8sClusterReceiverMetricNames),
	)
	allMatchResults.expectAtLeastOneMatch(
		g,
		"expected to find at least one k8s_cluster receiver metric",
	)
}

func verifyPrometheusMetrics(g Gomega) {
	allMatchResults := fileHasMatchingMetrics(
		g,
		resourceAttributeMatcher(deploymentMetricsMatchConfig),
		metricNameIsMemberOfList(prometheusReceiverMetricNames),
	)
	allMatchResults.expectAtLeastOneMatch(
		g,
		"expected to find at least one Prometheus receiver metric",
	)
}

func verifyNonNamespaceScopedKubeletStatsMetricsOnly(g Gomega) {
	allMatchResults := fileHasMatchingMetrics(
		g,
		resourceAttributeMatcher(nodeMetricsMatchConfig),
		metricNameIsMemberOfList(kubeletStatsReceiverMetricNames),
	)
	allMatchResults.expectAtLeastOneMatch(
		g,
		"expected to find at least one kubeletstat receiver metric (non-namespace-scoped)",
	)
}

func fileHasMatchingMetrics(
	g Gomega,
	resourceMatchFn func(pmetric.ResourceMetrics, *ResourceMatchResult[pmetric.ResourceMetrics]),
	metricMatchFn func(pmetric.Metric, *ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric]),
) MatchResultList[pmetric.ResourceMetrics, pmetric.Metric] {
	fileHandle, err := os.Open("test-resources/e2e-test-volumes/otlp-sink/metrics.jsonl")
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, metricsJsonMaxLineLength), metricsJsonMaxLineLength)

	matchResults := newMatchResultList[pmetric.ResourceMetrics, pmetric.Metric]()

	// read file line by line
	for scanner.Scan() {
		resourceMetricBytes := scanner.Bytes()
		metrics, err := metricsUnmarshaller.UnmarshalMetrics(resourceMetricBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		hasMatchingMetrics(
			metrics,
			resourceMatchFn,
			metricMatchFn,
			&matchResults,
		)
		if matchResults.hasMatch(g) {
			break
		}
	}

	g.Expect(scanner.Err()).NotTo(HaveOccurred())

	return matchResults
}

//nolint:all
func hasMatchingMetrics(
	metrics pmetric.Metrics,
	resourceMatchFn func(pmetric.ResourceMetrics, *ResourceMatchResult[pmetric.ResourceMetrics]),
	metricMatchFn func(pmetric.Metric, *ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric]),
	allMatchResults *MatchResultList[pmetric.ResourceMetrics, pmetric.Metric],
) {
	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		resourceMetric := metrics.ResourceMetrics().At(i)
		resourceMatchResult := newResourceMatchResult(resourceMetric)
		resourceMatchFn(resourceMetric, &resourceMatchResult)

		for j := 0; j < resourceMetric.ScopeMetrics().Len(); j++ {
			scopeMetric := resourceMetric.ScopeMetrics().At(j)
			for k := 0; k < scopeMetric.Metrics().Len(); k++ {
				metric := scopeMetric.Metrics().At(k)
				metricMatchResult := newObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric](
					metric.Name(),
					resourceMetric,
					resourceMatchResult,
					metric,
				)
				metricMatchFn(metric, &metricMatchResult)
				allMatchResults.addResultForObject(metricMatchResult)
			}
		}
	}
}

func verifySelfMonitoringMetrics(g Gomega) {
	resourceMatchFn := func(
		resourceMetrics pmetric.ResourceMetrics,
		matchResult *ResourceMatchResult[pmetric.ResourceMetrics],
	) {
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
			// telemetry does not go through the k8sattributes processor, in fact it does neithr go through the
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

	metricMatchFn := func(
		metric pmetric.Metric,
		matchResult *ObjectMatchResult[pmetric.ResourceMetrics, pmetric.Metric],
	) {
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

	allMatchResults := fileHasMatchingMetrics(
		g,
		resourceMatchFn,
		metricMatchFn,
	)
	allMatchResults.expectAtLeastOneMatch(
		g,
		"expected to find at least one matching self-monitoring metric",
	)
}

func resourceAttributeMatcher(
	matchConfig metricsResourceMatchConfig,
) func(pmetric.ResourceMetrics, *ResourceMatchResult[pmetric.ResourceMetrics]) {
	return func(resourceMetrics pmetric.ResourceMetrics, matchResult *ResourceMatchResult[pmetric.ResourceMetrics]) {
		attributes := resourceMetrics.Resource().Attributes()

		namespace, namespaceIsSet := attributes.Get(namespaceNameKey)
		if namespaceIsSet {
			// Make sure we only collect metrics from monitored namespaces (or only non-namespace-scoped metrics if no
			// namespace is monitored). If the metric has a namespace resource attribute, it needs to be the only
			// namespaces that has a Dash0Monitoring resource. We allow metrics that do not have any namespace resource
			// attribute, like all node-related metrics.
			//
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
			if matchConfig.allowNamespaceScopedMetrics {
				Expect(namespace.Str()).To(Equal(applicationUnderTestNamespace),
					fmt.Sprintf("Found at least one metric (%s) from a non-monitored namespace (%s); the operator's "+
						"collectors should only collect metrics from monitored namespaces and metrics that are not "+
						"namespace-scoped (like Kubernetes node metrics).",
						metricName,
						namespace.Str()),
				)
			} else {
				Expect(namespaceIsSet).To(BeFalse(),
					fmt.Sprintf("Found at least one metric (%s) that has k8s.namespace.name set (%s); the operator's "+
						"collectors in the current configuration should only collect metrics which are not "+
						"namespace-scoped (like Kubernetes node metrics).",
						metricName,
						namespace.Str()),
				)
			}
		}

		if matchConfig.expectedDeploymentName != "" {
			actualDeploymentName, deploymentNameIsSet := attributes.Get(deploymentNameKey)
			if !deploymentNameIsSet {
				matchResult.addFailedAssertion(
					deploymentNameKey,
					fmt.Sprintf(
						"expected %s but the metric has no such resource attribute",
						matchConfig.expectedDeploymentName,
					),
				)
			} else if actualDeploymentName.Str() != matchConfig.expectedDeploymentName {
				matchResult.addFailedAssertion(
					deploymentNameKey,
					fmt.Sprintf(
						"expected %s but it was %s",
						matchConfig.expectedDeploymentName,
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

		checkPodUid(attributes, matchConfig.expectPodUid, matchResult)
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

func parseMetricNameList(metricNameListRaw string) []string {
	return strings.Split(metricNameListRaw, "\n")
}
