// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	originalKubeContext       string
	kubeContextHasBeenChanged bool
)

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

func ensureNamespaceExists(namespace string) {
	output, err := run(exec.Command("kubectl", "get", "ns", namespace))
	if err != nil {
		if strings.Contains(output, "(NotFound)") {
			Expect(
				runAndIgnoreOutput(exec.Command("kubectl", "create", "ns", namespace))).To(Succeed())
		} else {
			Fail(fmt.Sprintf("kubectl get ns %s failed with unexpected error: %v", namespace, err))
		}
	}
}
