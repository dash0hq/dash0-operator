// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/dash0hq/dash0-operator/internal/agent0connector/a0cresources"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// determinePseudoClusterUid returns the UID of the kube-system namespace, which the agent0-connector uses as its client
// ID when subscribing to the outbound-connector.
func determinePseudoClusterUid() string {
	By("determining the pseudo cluster UID (the UID of the kube-system namespace)")
	clusterUid, err := run(exec.Command(
		"kubectl",
		"get", "namespace", "kube-system",
		"-o", "jsonpath={.metadata.uid}",
	), false)
	Expect(err).ToNot(HaveOccurred())
	pseudoClusterUid := strings.TrimSpace(clusterUid)
	Expect(pseudoClusterUid).ToNot(BeEmpty())
	return pseudoClusterUid
}

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

// verifyAgent0ConnectorCommandIsAllowedByRbac triggers a kubectl command request via the outbound-connector mock and
// verifies that the Kubernetes API server accepted the request, and that stdout contains the expected substring.
func verifyAgent0ConnectorCommandIsAllowedByRbac(
	pseudoClusterUid string,
	arguments []string,
	expectedStdoutSubstring string,
) {
	commandLine := "kubectl " + strings.Join(arguments, " ")
	By(fmt.Sprintf("verifying the agent0-connector is allowed to execute \"%s\"", commandLine))
	var requestId string
	Eventually(func(g Gomega) {
		requestId = triggerOutboundConnectorMockCommandRequest(g, pseudoClusterUid, "kubectl", arguments)
	}, 30*time.Second, pollingInterval).Should(Succeed())
	Eventually(func(g Gomega) {
		response := findOutboundConnectorMockCommandResponse(g, requestId)
		g.Expect(response.ExitCode).To(
			BeEquivalentTo(0),
			"\"%s\" should have succeeded; stderr was: %s", commandLine, response.Stderr)
		g.Expect(response.Stderr).ToNot(ContainSubstring("is forbidden"))
		g.Expect(response.Stdout).To(ContainSubstring(expectedStdoutSubstring))
	}, 90*time.Second, pollingInterval).Should(Succeed())
}

// verifyAgent0ConnectorCommandIsForbiddenByRbac triggers a kubectl command request via the outbound-connector mock and
// verifies that the Kubernetes API server rejected the request for the given resource type.
func verifyAgent0ConnectorCommandIsForbiddenByRbac(
	pseudoClusterUid string,
	arguments []string,
	forbiddenResourceType string,
) {
	commandLine := "kubectl " + strings.Join(arguments, " ")
	By(fmt.Sprintf("verifying the agent0-connector is not allowed to execute \"%s\"", commandLine))
	var requestId string
	Eventually(func(g Gomega) {
		requestId = triggerOutboundConnectorMockCommandRequest(g, pseudoClusterUid, "kubectl", arguments)
	}, 30*time.Second, pollingInterval).Should(Succeed())
	Eventually(func(g Gomega) {
		response := findOutboundConnectorMockCommandResponse(g, requestId)
		g.Expect(response.ExitCode).ToNot(
			BeEquivalentTo(0),
			"\"%s\" should have failed; stdout was: %s", commandLine, response.Stdout)
		g.Expect(response.Stderr).To(
			ContainSubstring("%s is forbidden", forbiddenResourceType),
			"the request for %s should have been rejected by RBAC; stderr was: %s",
			forbiddenResourceType,
			response.Stderr)
	}, 90*time.Second, pollingInterval).Should(Succeed())
}
