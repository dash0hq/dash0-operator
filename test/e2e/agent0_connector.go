// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"os/exec"
	"time"

	"github.com/dash0hq/dash0-operator/internal/agent0connector/a0cresources"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// waitForAgent0ConnectorDeploymentToBecomeAvailable waits for the agent0-connector deployment the operator manages to
// report the Available condition.
func waitForAgent0ConnectorDeploymentToBecomeAvailable() {
	By("waiting for the agent0-connector deployment to become available")
	Eventually(func(g Gomega) {
		g.Expect(runAndIgnoreOutput(exec.Command(
			"kubectl",
			"-n", operatorNamespace,
			"wait", "--for=condition=Available",
			"deployment/"+a0cresources.DeploymentName(operatorHelmReleaseName),
			"--timeout=30s",
		))).To(Succeed())
	}, 120*time.Second, 2*time.Second).Should(Succeed())
}

// verifyAgent0ConnectorResourcesDoNotExist verifies that the operator has removed every Kubernetes resource it manages
// for the agent0-connector, not only the deployment. The cluster role and the cluster role binding matter most: they
// are what grants the agent0-connector its read access to the cluster.
func verifyAgent0ConnectorResourcesDoNotExist() {
	By("verifying that the agent0-connector Kubernetes resources have been removed")
	namespacedResources := map[string]string{
		"deployment":     a0cresources.DeploymentName(operatorHelmReleaseName),
		"serviceaccount": a0cresources.ServiceAccountName(operatorHelmReleaseName),
	}
	clusterScopedResources := map[string]string{
		"clusterrole":        a0cresources.ClusterRoleName(operatorHelmReleaseName),
		"clusterrolebinding": a0cresources.ClusterRoleBindingName(operatorHelmReleaseName),
	}
	Eventually(func(g Gomega) {
		for resourceType, resourceName := range namespacedResources {
			_, err := run(exec.Command(
				"kubectl", "-n", operatorNamespace, "get", resourceType, resourceName,
			), false)
			g.Expect(err).To(HaveOccurred(), "the agent0-connector %s %s still exists", resourceType, resourceName)
		}
		for resourceType, resourceName := range clusterScopedResources {
			_, err := run(exec.Command("kubectl", "get", resourceType, resourceName), false)
			g.Expect(err).To(HaveOccurred(), "the agent0-connector %s %s still exists", resourceType, resourceName)
		}
	}, 60*time.Second, pollingInterval).Should(Succeed())
}
