// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	targetAllocatorName = fmt.Sprintf(
		"%s-opentelemetry-target-allocator-deployment",
		operatorHelmReleaseName,
	)
)

func deployPrometheusCrds(cleanupSteps *neccessaryCleanupSteps) {
	By("deploying Prometheus ServiceMonitor, PodMonitor and ScrapeConfig")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"--server-side",
		"-f",
		"test/util/crds/monitoring.coreos.com_podmonitors.yaml",
	))).To(Succeed())
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"--server-side",
		"-f",
		"test/util/crds/monitoring.coreos.com_servicemonitors.yaml",
	))).To(Succeed())
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"--server-side",
		"-f",
		"test/util/crds/monitoring.coreos.com_scrapeconfigs.yaml",
	))).To(Succeed())
	cleanupSteps.removePrometheusCrds = true
}

func removePrometheusCrds(cleanupSteps *neccessaryCleanupSteps) {
	if !cleanupSteps.removePrometheusCrds {
		return
	}
	By("removing PersesDashboard and PrometheusRule CRDs")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-f",
		"test/util/crds/monitoring.coreos.com_podmonitors.yaml",
	))).To(Succeed())
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-f",
		"test/util/crds/monitoring.coreos.com_servicemonitors.yaml",
	))).To(Succeed())
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-f",
		"test/util/crds/monitoring.coreos.com_scrapeconfigs.yaml",
	))).To(Succeed())
}

func waitForTargetAllocatorToStart(operatorNamespace string) {
	By("validating that the OpenTelemetry target-allocator has been created and is running as expected")
	verifyTargetAllocatorIsUp := func(g Gomega) {
		g.Expect(runAndIgnoreOutput(
			exec.Command("kubectl",
				"rollout",
				"status",
				"deployment",
				targetAllocatorName,
				"--namespace",
				operatorNamespace,
				"--timeout",
				"20s",
			))).To(Succeed())
	}

	Eventually(verifyTargetAllocatorIsUp, 60*time.Second, time.Second).Should(Succeed())
}

func verifyThatTargetAllocatorIsNotDeployed(operatorNamespace string) {
	By("validating that the OpenTelemetry target-allocator deployment does not exist")
	verifyTargetAllocatorDeploymentDoesNotExist := func(g Gomega) {
		g.Expect(runAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"get",
				"deployment",
				"--namespace",
				operatorNamespace,
				targetAllocatorName,
			))).ToNot(Succeed())
	}
	Eventually(verifyTargetAllocatorDeploymentDoesNotExist, 60*time.Second, time.Second).Should(Succeed())
}
