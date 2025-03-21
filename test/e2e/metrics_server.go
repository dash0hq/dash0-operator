// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func ensureMetricsServerIsInstalled() bool {
	err := runAndIgnoreOutput(
		exec.Command("kubectl", "--namespace", "kube-system", "get", "deployments", "metrics-server"))
	if err != nil {
		By("installing the metrics-server")
		e2ePrint(
			"Hint: To get a faster feedback cycle on e2e tests, deploy metrics-server once via the following commands:\n" +
				"  helm repo add metrics-server https://kubernetes-sigs.github.io/metrics-server/ && \n" +
				"  helm upgrade --install --set args={--kubelet-insecure-tls} " +
				"metrics-server metrics-server/metrics-server --namespace kube-system\n" +
				"If the e2e tests find an existing metrics-server installation, they " +
				"will not deploy metrics-server and they will also not undeploy it after running the test suite.\n",
		)
		installMetricsServer()
		return true
	} else {
		e2ePrint("The metrics-server deployment exists, assuming metrics-server has been deployed already.\n")
	}
	return false
}

func installMetricsServer() {
	repoList, err := run(exec.Command("helm", "repo", "list"))
	Expect(err).ToNot(HaveOccurred())

	if !strings.Contains(repoList, "metrics-server") {
		e2ePrint("The helm repo for metrics-server has not been found, adding it now.\n")
		Expect(runAndIgnoreOutput(
			exec.Command(
				"helm",
				"repo",
				"add",
				"metrics-server",
				"https://kubernetes-sigs.github.io/metrics-server/",
				"--force-update",
			))).To(Succeed())
		e2ePrint("Running helm repo update.\n")
		Expect(runAndIgnoreOutput(exec.Command("helm", "repo", "update"))).To(Succeed())
	}

	Expect(runAndIgnoreOutput(exec.Command(
		"helm",
		"upgrade",
		"--install",
		"--set", "args={--kubelet-insecure-tls}",
		"metrics-server",
		"metrics-server/metrics-server",
		"--namespace",
		"kube-system",
		"--timeout",
		"1m",
	))).To(Succeed())
	Expect(runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"deployment.apps/metrics-server",
			"--for",
			"condition=Available",
			"--namespace",
			"kube-system",
			"--timeout",
			"60s",
		))).To(Succeed())
}

func uninstallMetricsServerIfApplicable(metricsServerHasBeenInstalled bool) {
	if metricsServerHasBeenInstalled {
		By("uninstalling the metrics-server helm chart")
		uninstallMetricsServer()
	} else {
		e2ePrint(
			"Note: The e2e test suite did not install metrics-server, thus it will also not uninstall it.\n",
		)
	}
}

func uninstallMetricsServer() {
	if err := runAndIgnoreOutput(exec.Command(
		"helm", "uninstall", "--namespace", "kube-system", "metrics-server", "--ignore-not-found",
	)); err != nil {
		e2ePrint("warning: %v\n", err)
	}
}
