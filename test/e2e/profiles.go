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

var (
	ebpfProfilerDaemonSetName = fmt.Sprintf("%s-ebpf-profiler", operatorHelmReleaseName)
)

func waitForEbpfProfilerToStart(operatorNamespace string) {
	By("validating that the eBPF profiler has been created and is running as expected")
	Eventually(func(g Gomega) {
		// kubectl rollout status fails outright if the daemonset has not been created yet, hence the Eventually block.
		g.Expect(runAndIgnoreOutput(
			exec.Command("kubectl",
				"rollout",
				"status",
				"daemonset",
				ebpfProfilerDaemonSetName,
				"--namespace",
				operatorNamespace,
				"--timeout",
				"20s",
			))).To(Succeed())
	}, 120*time.Second, time.Second).Should(Succeed())
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
