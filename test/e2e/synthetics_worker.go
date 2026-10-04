// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os/exec"
	"time"

	"github.com/dash0hq/dash0-operator/internal/syntheticsworker/swresources"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// waitForSyntheticsWorkerPodToStart waits for the synthetics-worker pod the operator manages for the given location
// ID to reach the Running phase. It deliberately does not wait for the Deployment's Available condition or the pod's
// Ready condition: the synthetics-worker's readiness probe only turns healthy once it holds an open stream to its
// configured server address, which the e2e cluster's sandboxed network cannot reach.
func waitForSyntheticsWorkerPodToStart(locationId string) {
	By("waiting for the synthetics-worker pod to start")
	Eventually(func(g Gomega) {
		output, err := run(exec.Command(
			"kubectl",
			"-n", operatorNamespace,
			"get", "pods",
			"-l", fmt.Sprintf(
				"app.kubernetes.io/name=dash0-synthetics-worker,synthetics-worker.dash0.com/location-id=%s",
				locationId,
			),
			"-o", "jsonpath={.items[0].status.phase}",
		), false)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(output).To(Equal("Running"))
	}, 60*time.Second, pollingInterval).Should(Succeed())
}

// verifySyntheticsWorkerResourcesDoNotExist verifies that the operator has removed every Kubernetes resource it
// manages for the synthetics-worker instance with the given location ID. Unlike the agent0-connector, there is no
// ClusterRole/ClusterRoleBinding, the synthetics-worker only dials outbound and needs no cluster-wide read access.
func verifySyntheticsWorkerResourcesDoNotExist(locationId string) {
	By("verifying that the synthetics-worker Kubernetes resources have been removed")
	namespacedResources := map[string]string{
		"deployment":     swresources.DeploymentName(operatorHelmReleaseName, locationId),
		"serviceaccount": swresources.ServiceAccountName(operatorHelmReleaseName, locationId),
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
