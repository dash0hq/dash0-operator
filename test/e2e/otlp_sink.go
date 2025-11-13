// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type expectationMode string

const (
	expectAtLeastOne expectationMode = "at-least-one"
	expectExactlyOne expectationMode = "exactly-one"
	expectNoMatches  expectationMode = "no-matches"
)

type logResourceMatcherMode string

const (
	logResourceMatcherWorkload                          logResourceMatcherMode = "workload"
	logResourceMatcherSelfMonitoringLogsOperatorManager logResourceMatcherMode = "self-monitoring-logs-operator-manager"
	logResourceMatcherSelfMonitoringLogsCollector       logResourceMatcherMode = "self-monitoring-logs-collector"
)

type metricsMatchMode string

const (
	metricsMatchModeWorkload                      metricsMatchMode = "metrics-match-mode-worklaod"
	metricsMatchModeSelfMonitoringOperatorManager metricsMatchMode = "metrics-match-mode-self-monitoring-operator-manager"
	metricsMatchModeSelfMonitoringCollector       metricsMatchMode = "metrics-match-mode-self-monitoring-collector"
	metricsMatchModeMatchAll                      metricsMatchMode = "metrics-match-mode-match-all"
)

type metricNameList string

const (
	kubeletStatsReceiverMetricNameList metricNameList = "kubelet-stats-receiver"
	k8sClusterReceiverMetricNameList   metricNameList = "k8s-cluster-receiver"
	prometheusReceiverMetricNameList   metricNameList = "prometheus-receiver"
)

const (
	otlpSinkChartPath   = "test/e2e/otlp-sink/helm-chart"
	otlpSinkReleaseName = "otlp-sink"

	otlpSinkNamespace = "otlp-sink"

	// common telemetry-matcher query parameters
	queryParamExpectationMode        = "expectation-mode"
	queryParamRuntime                = "runtime"
	queryParamRuntimeWorkloadName    = "runtime-workload-name"
	queryParamWorkloadType           = "workload-type"
	queryParamTimestampLowerBoundStr = "timestamp-lower-bound"
	queryParamClusterName            = "cluster"

	// query parameters for matching spans
	queryParamCheckResourceAttributes = "check-resource-attributes"
	queryParamRoute                   = "route"
	queryParamQuery                   = "query"
	queryParamTarget                  = "target"

	// query parameters for matching logs
	queryParamLogsResourceMatcherMode = "logs-resource-matcher"
	queryParamOperatorNamespace       = "operator-namespace"
	queryParamServiceVersion          = "service-version"
	queryParamLogBodyEquals           = "log-body-equals"
	queryParamLogBodyContains         = "log-body-contains"

	// query parameters for matching metrics
	queryParamMetricsMatchMode            = "metrics-match-mode"
	queryParamDeploymentName              = "deployment-name"
	queryParamExpectPodUid                = "expect-pod-uid"
	queryParamFailOnNamespaceOtherThan    = "fail-on-namespace-other-than"
	queryParamFailOnNamespaceScopedMetric = "fail-on-namespace-scoped-metric"
	queryParamMetricNameList              = "metric-name-list"
)

type ExpectationResult struct {
	Success     bool   `json:"success"`
	Description string `json:"description,omitempty"`
}

var (
	telemetryMatcherBaseUrl    = "http://localhost:8002"
	telemetryMatcherHttpClient *http.Client

	telemetryMatcherImage ImageSpec
)

func init() {
	// disable keep-alive
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	telemetryMatcherHttpClient = &http.Client{Transport: t}
}

func determineTelemetryMatcherImage() {
	repositoryPrefix := getEnvOrDefault("TEST_IMAGE_REPOSITORY_PREFIX", defaultImageRepositoryPrefix)
	imageTag := getEnvOrDefault("TEST_IMAGE_TAG", defaultImageTag)
	pullPolicy := getEnvOrDefault("TEST_IMAGE_PULL_POLICY", defaultPullPolicy)
	telemetryMatcherImage =
		determineContainerImage(
			"TELEMETRY_MATCHER",
			repositoryPrefix,
			"telemetry-matcher",
			imageTag,
			pullPolicy,
		)
}

func rebuildTelemetryMatcherImage() {
	if testImageBuildsShouldBeSkipped() {
		e2ePrint("Skipping make telemetry-matcher-image (SKIP_TEST_APP_IMAGE_BUILDS=true)\n")
		return
	}
	By(fmt.Sprintf("building the %v image", telemetryMatcherImage))
	Expect(
		runAndIgnoreOutput(
			exec.Command("make", "telemetry-matcher-image"))).To(Succeed())

	loadImageToKindClusterIfRequired(telemetryMatcherImage, nil)
}

func deployOtlpSink(workingDir string, cleanupSteps *neccessaryCleanupSteps) {
	createDirAndDeleteOldExportedTelemetry()

	helmArgs := []string{"install",
		"--namespace",
		otlpSinkNamespace,
		"--create-namespace",
		"--wait",
		"--timeout",
		"60s",
		otlpSinkReleaseName,
		otlpSinkChartPath,
	}

	if !isKindCluster() {
		e2eTestExportDir := fmt.Sprintf(
			"%s/test-resources/e2e/volumes/otlp-sink",
			workingDir,
		)
		helmArgs =
			append(helmArgs, "--set", fmt.Sprintf("collector.telemetryFileExportVolume.path=%s", e2eTestExportDir))
	}
	helmArgs = append(
		helmArgs,
		"--set",
		fmt.Sprintf("telemetryMatcher.image.repository=%s", telemetryMatcherImage.repository),
	)
	helmArgs = append(
		helmArgs,
		"--set",
		fmt.Sprintf("telemetryMatcher.image.tag=%s", telemetryMatcherImage.tag),
	)
	helmArgs = append(
		helmArgs,
		"--set",
		fmt.Sprintf("telemetryMatcher.image.pullPolicy=%s", telemetryMatcherImage.pullPolicy),
	)
	cleanupSteps.removeOtlpSink = true
	Expect(runAndIgnoreOutput(exec.Command("helm", helmArgs...))).To(Succeed())
}

func createDirAndDeleteOldExportedTelemetry() {
	_ = os.MkdirAll("test-resources/e2e/volumes/otlp-sink", 0755)
	By("deleting old telemetry files")
	_ = os.Remove("test-resources/e2e/volumes/otlp-sink/traces.jsonl")
	_ = os.Remove("test-resources/e2e/volumes/otlp-sink/metrics.jsonl")
	_ = os.Remove("test-resources/e2e/volumes/otlp-sink/logs.jsonl")
	By("creating telemetry dump files")
	_, _ = os.Create("test-resources/e2e/volumes/otlp-sink/traces.jsonl")
	_, _ = os.Create("test-resources/e2e/volumes/otlp-sink/metrics.jsonl")
	_, _ = os.Create("test-resources/e2e/volumes/otlp-sink/logs.jsonl")
}

func uninstallOtlpSink(cleanupSteps *neccessaryCleanupSteps) {
	if !cleanupSteps.removeOtlpSink {
		return
	}
	By("removing otlp-sink")
	Expect(runAndIgnoreOutput(
		exec.Command(
			"helm",
			"uninstall",
			otlpSinkReleaseName,
			"--namespace",
			otlpSinkNamespace,
			"--ignore-not-found",
		))).To(Succeed())
	Expect(runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"ns",
			otlpSinkNamespace,
			"--wait",
			"--ignore-not-found",
		))).To(Succeed())
}

func updateTelemetryMatcherUrlForKind() {
	if isKindCluster() {
		telemetryMatcherBaseUrl = fmt.Sprintf("http://%s/telemetry-matcher", kindClusterIngressIp)
	}
}
