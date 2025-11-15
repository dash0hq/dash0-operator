// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func ensureNginxIngressControllerIsInstalled(cleanupSteps *neccessaryCleanupSteps) {
	if os.Getenv("DEPLOY_NGINX_INGRESS") == "false" {
		return
	}
	By("checking if ingress-nginx is already running")
	if err := runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"--namespace",
			"ingress-nginx",
			"--for=condition=ready",
			"pod",
			"--selector=app.kubernetes.io/component=controller",
			"--timeout=5s",
		)); err == nil {
		e2ePrint("ingress-nginx is already running\n")
		return
	}

	e2ePrint(
		"Hint: To get a faster feedback cycle on e2e tests, deploy the ingress-nginx once via the following commands:\n" +
			"  kubectl apply -k test-resources/nginx\n" +
			"If the e2e tests find an existing ingress-nginx installation, they will not deploy the ingress-nginx and " +
			"they will also not undeploy it after running the test suite.\n",
	)
	installNginxIngressController(cleanupSteps)
}

func installNginxIngressController(cleanupSteps *neccessaryCleanupSteps) {
	By("deploying ingress-nginx")
	cleanupSteps.removeIngressNginx = true
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl", "apply", "-k", "test-resources/nginx",
	))).To(Succeed())

	By("waiting for ingress-nginx")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"wait",
		"--namespace",
		"ingress-nginx",
		"--for=condition=ready",
		"pod",
		"--selector=app.kubernetes.io/component=controller",
		"--timeout=90s",
	))).To(Succeed())
}

func undeployNginxIngressController(cleanupSteps *neccessaryCleanupSteps) {
	if !cleanupSteps.removeIngressNginx {
		return
	}
	By("removing nginx ingress controller")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl", "delete", "-k", "test-resources/nginx", "--ignore-not-found",
	))).To(Succeed())
}
