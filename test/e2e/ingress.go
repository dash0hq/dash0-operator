// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func deployIngressController(cleanupSteps *neccessaryCleanupSteps) {
	By("deploying ingress-nginx")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-f",
		"https://kind.sigs.k8s.io/examples/ingress/deploy-ingress-nginx.yaml",
	))).To(Succeed())
	cleanupSteps.removeIngressNginx = true

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
		"kubectl",
		"delete",
		"-f",
		"https://kind.sigs.k8s.io/examples/ingress/deploy-ingress-nginx.yaml",
		"--ignore-not-found",
	))).To(Succeed())
}
