// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"os/exec"
	"time"

	"github.com/dash0hq/dash0-operator/internal/syntheticsworker/swresources"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// waitForSyntheticsWorkerDeploymentToBecomeAvailable waits for the synthetics-worker deployment the operator manages
// to report the Available condition.
func waitForSyntheticsWorkerDeploymentToBecomeAvailable() {
	By("waiting for the synthetics-worker deployment to become available")
	Eventually(func(g Gomega) {
		g.Expect(runAndIgnoreOutput(exec.Command(
			"kubectl",
			"-n", operatorNamespace,
			"wait", "--for=condition=Available",
			"deployment/"+swresources.DeploymentName(operatorHelmReleaseName),
			"--timeout=30s",
		))).To(Succeed())
	}, 120*time.Second, 2*time.Second).Should(Succeed())
}

// verifySyntheticsWorkerResourcesDoNotExist verifies that the operator has removed every Kubernetes resource it
// manages for the synthetics-worker. Unlike the agent0-connector, there is no ClusterRole/ClusterRoleBinding, the
// synthetics-worker only dials outbound and needs no cluster-wide read access.
func verifySyntheticsWorkerResourcesDoNotExist() {
	By("verifying that the synthetics-worker Kubernetes resources have been removed")
	namespacedResources := map[string]string{
		"deployment":     swresources.DeploymentName(operatorHelmReleaseName),
		"serviceaccount": swresources.ServiceAccountName(operatorHelmReleaseName),
	}
	Eventually(func(g Gomega) {
		for resourceType, resourceName := range namespacedResources {
			_, err := run(exec.Command(
				"kubectl", "-n", operatorNamespace, "get", resourceType, resourceName,
			), false)
			g.Expect(err).To(HaveOccurred(), "the synthetics-worker %s %s still exists", resourceType, resourceName)
		}
	}, 60*time.Second, pollingInterval).Should(Succeed())
}
