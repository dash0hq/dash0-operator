// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func deployIngressController(cleanupSteps *neccessaryCleanupSteps) {
	e2ePrint("Note: The test application is running on a kind cluster, make sure cloud-provider-kind is running.\n")
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

	By("retrieving the ingress IP")
	var err error
	rawKindClusterIngressIp, err := run(exec.Command(
		"kubectl",
		"get",
		"services",
		"--namespace",
		"ingress-nginx",
		"ingress-nginx-controller",
		"--output",
		"jsonpath='{.status.loadBalancer.ingress[0].ip}'",
	))
	Expect(err).NotTo(HaveOccurred())
	kindClusterIngressIp = strings.TrimSpace(rawKindClusterIngressIp)
	kindClusterIngressIp = strings.Trim(kindClusterIngressIp, "'")
	Expect(kindClusterIngressIp).
		NotTo(
			BeEmpty(),
			"could not retrieve the ingress IP - is cloud-provider-kind running?",
		)
	e2ePrint("cluster ingress IP: %s\n", kindClusterIngressIp)
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
