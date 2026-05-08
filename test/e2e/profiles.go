// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/test/e2e/pkg/shared"
)

const (
	ebpfProfilerHelmReleaseName = "ebpf-profiler"
	ebpfProfilerHelmChartPath   = "test-resources/ebpf-profiler/helm-chart"
)

// Note: we need to hardcode the k8s.namespace.name here so the e2e tests work on kind, where the ebpf profiler can't
// correctly set the container.id and therefore the k8sattributes processor does not correctly populate the attribute.
// This means that all profiles will have the hardcoded namespace, but that's good enough to check whether the profiles
// arrive at the otlp sink.
func deployEbpfProfiler(operatorNs string, targetNamespace string) {
	By("deploying eBPF profiler")
	collectorServiceEndpoint := fmt.Sprintf(
		"%s-opentelemetry-collector-service.%s.svc.cluster.local:4317",
		operatorHelmReleaseName,
		operatorNs,
	)

	output, err := run(exec.Command(
		"helm", "install",
		"--namespace", operatorNs,
		"--wait",
		"--timeout", "300s",
		"--set", fmt.Sprintf("collector.endpoint=%s", collectorServiceEndpoint),
		"--set", fmt.Sprintf("namespaceOverride=%s", targetNamespace),
		ebpfProfilerHelmReleaseName,
		ebpfProfilerHelmChartPath,
	))
	if err != nil {
		Fail(fmt.Sprintf("failed to deploy eBPF profiler: %s, output: %s", err, output))
	}
}

func teardownEbpfProfiler(operatorNs string) {
	By("tearing down eBPF profiler")
	_, _ = run(exec.Command(
		"helm", "uninstall", ebpfProfilerHelmReleaseName,
		"--namespace", operatorNs,
		"--ignore-not-found",
	))
}

func verifyProfiles(
	g Gomega,
	timestampLowerBound time.Time,
	expectedNamespace string,
) {
	requestUrl := compileTelemetryMatcherUrlForProfiles(
		shared.ExpectAtLeastOne,
		timestampLowerBound,
		expectedNamespace,
	)
	executeTelemetryMatcherRequest(g, requestUrl)
}

func compileTelemetryMatcherUrlForProfiles(
	expectationMode shared.ExpectationMode,
	timestampLowerBound time.Time,
	expectedNamespace string,
) string {
	baseUrl := fmt.Sprintf("%s/matching-profiles", telemetryMatcherBaseUrl)
	params := url.Values{}
	params.Add(shared.QueryParamExpectationMode, string(expectationMode))
	params.Add(shared.QueryParamTimestampLowerBoundStr, strconv.FormatInt(timestampLowerBound.UnixNano(), 10))
	params.Add(shared.QueryParamNamespace, expectedNamespace)
	return baseUrl + "?" + params.Encode()
}
