// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	activeKubeCtxIsKindCluster *bool
	kindClusterName            string
	kindClusterIngressIp       string
)

func isKindCluster() bool {
	if activeKubeCtxIsKindCluster != nil {
		return *activeKubeCtxIsKindCluster
	}

	activeKubeCtxIsKindCluster = ptr.To(false)

	// check if kind is installed
	err := runAndIgnoreOutput(
		exec.Command("command",
			"-v",
			"kind",
		))
	if err != nil {
		return *activeKubeCtxIsKindCluster
	}

	// kind is installed, check if the name of the current kubernetes context matches an existing kind cluster
	kubernetesContext, err := run(exec.Command("kubectl", "config", "current-context"))
	Expect(err).NotTo(HaveOccurred())
	kubernetesContext = strings.TrimSpace(kubernetesContext)
	kindClusters, err := run(exec.Command("kind", "get", "clusters"))
	Expect(err).NotTo(HaveOccurred())
	for _, clusterName := range getNonEmptyLines(kindClusters) {
		if kubernetesContext == fmt.Sprintf("kind-%s", clusterName) {
			kindClusterName = clusterName
			activeKubeCtxIsKindCluster = ptr.To(true)
			break
		}
	}

	if *activeKubeCtxIsKindCluster {
		deployIngressController()
	}

	return *activeKubeCtxIsKindCluster
}

func deployIngressController() {
	e2ePrint("Note: The test application is running on a kind cluster, make sure cloud-provider-kind is running.\n")
	By("deploying ingress-nginx")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-f",
		"https://kind.sigs.k8s.io/examples/ingress/deploy-ingress-nginx.yaml",
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

func undeployNginxIngressController() {
	By("removing nginx ingress controller")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"-f",
		"https://kind.sigs.k8s.io/examples/ingress/deploy-ingress-nginx.yaml",
		"--ignore-not-found",
	))).To(Succeed())
}
