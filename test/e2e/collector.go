// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

var (
	collectorDaemonSetName          = fmt.Sprintf("%s-opentelemetry-collector-agent", operatorHelmReleaseName)
	collectorDaemonSetNameQualified = fmt.Sprintf("daemonset/%s", collectorDaemonSetName)
	collectorConfigMapName          = fmt.Sprintf("%s-opentelemetry-collector-agent", operatorHelmReleaseName)
	collectorConfigMapNameQualified = fmt.Sprintf("configmap/%s", collectorConfigMapName)
)

func verifyThatCollectorIsRunning(operatorNamespace string) {
	By("validating that the OpenTelemetry collector has been created and is running as expected")
	verifyCollectorIsUp := func(g Gomega) {
		// Even though this command comes with its own timeout, we still have to wrap it in an Eventually block, since
		// it will fail outright if the daemonset has not been created yet.
		g.Expect(runAndIgnoreOutput(
			exec.Command("kubectl",
				"rollout",
				"status",
				"daemonset",
				collectorDaemonSetName,
				"--namespace",
				operatorNamespace,
				"--timeout",
				"20s",
			))).To(Succeed())
	}

	Eventually(verifyCollectorIsUp, 60*time.Second, time.Second).Should(Succeed())
}

func verifyThatCollectorHasBeenRemoved(operatorNamespace string) {
	By("validating that the OpenTelemetry collector has been removed")
	verifyCollectorIsGone := func(g Gomega) {
		g.Expect(runAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"get",
				"daemonset",
				"--namespace",
				operatorNamespace,
				collectorDaemonSetName,
			))).ToNot(Succeed())
	}
	Eventually(verifyCollectorIsGone, 60*time.Second, time.Second).Should(Succeed())
}

func verifyConfigMapContainsString(operatorNamespace string, s string) {
	verifyCommandOutputContainsStrings(
		exec.Command(
			"kubectl",
			"get",
			"-n",
			operatorNamespace,
			collectorConfigMapNameQualified,
			"-o",
			"json",
		),
		s,
	)
}

func verifyCollectorContainerLogContainsStrings(
	operatorNamespace string,
	containerName string,
	needles ...string,
) {
	verifyCommandOutputContainsStrings(
		exec.Command(
			"kubectl",
			"logs",
			"-n",
			operatorNamespace,
			collectorDaemonSetNameQualified,
			"-c",
			containerName,
		),
		needles...,
	)
}
