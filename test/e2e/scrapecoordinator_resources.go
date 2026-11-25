// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
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
	scrapeCoordinatorName = fmt.Sprintf(
		"%s-scrape-coordinator-deployment",
		operatorHelmReleaseName,
	)
)

func waitForScrapeCoordinatorToStart(operatorNamespace string) {
	By("validating that the scrape coordinator has been created and is running as expected")
	verifyScrapeCoordinatorIsUp := func(g Gomega) {
		g.Expect(runAndIgnoreOutput(
			exec.Command("kubectl",
				"rollout",
				"status",
				"deployment",
				scrapeCoordinatorName,
				"--namespace",
				operatorNamespace,
				"--timeout",
				"20s",
			))).To(Succeed())
	}

	Eventually(verifyScrapeCoordinatorIsUp, 60*time.Second, time.Second).Should(Succeed())
}

func verifyThatScrapeCoordinatorIsNotDeployed(operatorNamespace string) {
	By("validating that the scrape coordinator deployment does not exist")
	verifyScrapeCoordinatorDeploymentDoesNotExist := func(g Gomega) {
		g.Expect(runAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"get",
				"deployment",
				"--namespace",
				operatorNamespace,
				scrapeCoordinatorName,
			), false, false, false)).ToNot(Succeed())
	}
	Consistently(verifyScrapeCoordinatorDeploymentDoesNotExist, 20*time.Second, time.Second).Should(Succeed())
}
