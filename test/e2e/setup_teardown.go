// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	originalKubeContext       string
	kubeContextHasBeenChanged bool

	requiredPorts = []int{
		// Node.js workloads
		1205, 1206, 1207, 1208, 1209, 1210, 1211, //
		// JVM workloads
		1306, 1307, 1309, 1310, 1311, //
		// .NET workloads
		1406, 1407, 1409, 1410, 1411, //
		// Python workloads
		1506, 1507, 1509, 1510, 1511, //
		// OTel Collector
		4317, 4318,
		// Dash0 API mock server
		8001,
	}
)

func checkIfRequiredPortsAreBlocked() {
	portsCurrentlyInUseByKubernetesServices, err := run(
		exec.Command(
			"kubectl",
			"get",
			"svc",
			"--all-namespaces",
			"-o",
			"go-template='{{range .items}}{{range.spec.ports}}{{if .port}}{{.port}}{{\"\\n\"}}{{end}}{{end}}{{end}}'",
		))
	Expect(err).NotTo(HaveOccurred())
	portsCurrentlyInUseArray := getNonEmptyLines(portsCurrentlyInUseByKubernetesServices)
	messages := make([]string, 0)
	foundBlockedPort := false
	for _, usedPortStr := range portsCurrentlyInUseArray {
		usedPort, err := strconv.Atoi(usedPortStr)
		if err != nil {
			continue
		}
		for _, requiredPort := range requiredPorts {
			if usedPort == requiredPort {
				messages = append(messages,
					fmt.Sprintf(
						"Port %d is required by the test suite, but it is already in use by a Kubernetes "+
							"service. Please check for conflicting deployed serivces.",
						requiredPort,
					))
				foundBlockedPort = true
			}
		}
	}
	if foundBlockedPort {
		messages = append(messages,
			"Note: If you have used the scripts for manual testing in test-resources, running "+
				"test-resources/bin/test-cleanup.sh might help removing all left-over Kubernetes objects.")
		Fail(strings.Join(messages, "\n"))
	}
}

func renderTemplates() {
	By("rendering yaml templates via render-templates.sh")
	Expect(runAndIgnoreOutput(exec.Command("test-resources/bin/render-templates.sh"))).To(Succeed())
}

func setKubernetesContext(kubernetesContextForTest string) (bool, string) {
	By("reading current Kubernetes context")
	kubectlCurrentContextOutput, err := run(exec.Command("kubectl", "config", "current-context"))
	var originalCtx string
	if err != nil {
		if strings.Contains(err.Error(), "error: current-context is not set") {
			originalCtx = ""
		} else {
			Expect(err).NotTo(HaveOccurred())
		}
	} else {
		originalCtx = strings.TrimSpace(kubectlCurrentContextOutput)
	}

	if originalCtx != kubernetesContextForTest {
		if originalCtx != "" {
			By(fmt.Sprintf(
				"switching to Kubernetes context %s, previous context %s will be restored later",
				kubernetesContextForTest,
				originalCtx,
			))
		} else {
			By(fmt.Sprintf("switching to Kubernetes context %s (current-context was unset before)",
				kubernetesContextForTest))
		}
		Expect(
			runAndIgnoreOutput(
				exec.Command(
					"kubectl",
					"config",
					"use-context",
					kubernetesContextForTest,
				))).To(Succeed())
		return true, originalCtx
	} else {
		// We are already in the correct context.
		By(fmt.Sprintf("running in Kubernetes context %s", originalCtx))
		return false, originalCtx
	}
}

func revertKubernetesContext(originalCtx string) {
	if originalCtx != "" {
		By("switching back to original Kubernetes context " + originalCtx)
		output, err := run(exec.Command("kubectl", "config", "use-context", originalCtx))
		if err != nil {
			_, _ = fmt.Fprint(GinkgoWriter, err.Error())
		}
		_, _ = fmt.Fprint(GinkgoWriter, output)
	} else {
		By("no Kubernetes context was active before running the e2e tests, thus unsetting current-context")
		output, err := run(exec.Command("kubectl", "config", "unset", "current-context"))
		if err != nil {
			_, _ = fmt.Fprint(GinkgoWriter, err.Error())
		}
		_, _ = fmt.Fprint(GinkgoWriter, output)
	}
}

func recreateNamespace(namespace string) {
	By(fmt.Sprintf("(re)creating namespace %s", namespace))
	output, err := run(exec.Command("kubectl", "get", "ns", namespace))
	if err != nil {
		if strings.Contains(output, "(NotFound)") {
			// The namespace does not exist, that's fine, we will create it further down.
		} else {
			Fail(fmt.Sprintf("kubectl get ns %s failed with unexpected error: %v", namespace, err))
		}
	} else {
		Expect(
			runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace))).To(Succeed())
		Expect(
			runAndIgnoreOutput(
				exec.Command("kubectl", "wait", "--for=delete", "ns", namespace, "--timeout=60s"))).To(Succeed())
	}

	Expect(
		runAndIgnoreOutput(exec.Command("kubectl", "create", "ns", namespace))).To(Succeed())
}
