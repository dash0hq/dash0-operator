// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"time"

	"github.com/dash0hq/dash0-operator/internal/syntheticsworker/swresources"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	syntheticsWorkerLocationId = "e2e-test-location"
	syntheticsWorkerToken      = "auth_e2e-synthetics-worker-dummy-token"
)

// waitForSyntheticsWorkerDeploymentToBecomeAvailable waits for the synthetics-worker deployment the operator manages
// for the given location ID to report the Available condition. The synthetics-worker's readiness probe only turns
// healthy once it holds an open stream to its configured server address, so this requires the worker to have
// connected to the outbound-connector mock.
func waitForSyntheticsWorkerDeploymentToBecomeAvailable(locationId string) {
	By("waiting for the synthetics-worker deployment to become available")
	Eventually(func(g Gomega) {
		g.Expect(runAndIgnoreOutput(exec.Command(
			"kubectl",
			"-n", operatorNamespace,
			"wait", "--for=condition=Available",
			"deployment/"+swresources.DeploymentName(operatorHelmReleaseName, locationId),
			"--timeout=30s",
		))).To(Succeed())
	}, 120*time.Second, 2*time.Second).Should(Succeed())
}

// verifySyntheticsWorkerIsConnectedToOutboundConnectorMock verifies that a synthetics-worker for the given location ID
// has subscribed to the outbound-connector mock with the expected authorization header and has sent at least one
// heartbeat. A worker with the ID previousWorkerId is ignored, which allows verifying that a newly deployed worker has
// connected. It returns the ID of the connected worker.
func verifySyntheticsWorkerIsConnectedToOutboundConnectorMock(
	locationId string,
	expectedToken string,
	previousWorkerId string,
) string {
	By("verifying that the synthetics-worker has connected to the outbound-connector mock")
	var workerId string
	Eventually(func(g Gomega) {
		var worker *outboundConnectorMockSyntheticsWorker
		workers := fetchOutboundConnectorMockSyntheticsWorkers(g)
		for i := range workers {
			if workers[i].LocationID == locationId && workers[i].WorkerID != previousWorkerId {
				worker = &workers[i]
				break
			}
		}
		g.Expect(worker).ToNot(
			BeNil(),
			"expected a synthetics-worker for location %s to be connected, got %v", locationId, workers)
		g.Expect(worker.WorkerID).ToNot(BeEmpty())
		g.Expect(worker.Authorization).To(Equal("Bearer " + expectedToken))
		g.Expect(worker.Heartbeats).To(
			BeNumerically(">", 0), "expected the synthetics-worker %s to have sent a heartbeat", worker.WorkerID)
		workerId = worker.WorkerID
	}, 60*time.Second, pollingInterval).Should(Succeed())
	return workerId
}

// verifySyntheticsWorkerIsNotConnectedToOutboundConnectorMock verifies that no synthetics-worker for the given location
// ID is subscribed to the outbound-connector mock.
func verifySyntheticsWorkerIsNotConnectedToOutboundConnectorMock(locationId string) {
	By("verifying that the synthetics-worker has disconnected from the outbound-connector mock")
	Eventually(func(g Gomega) {
		for _, worker := range fetchOutboundConnectorMockSyntheticsWorkers(g) {
			g.Expect(worker.LocationID).ToNot(
				Equal(locationId),
				"the synthetics-worker %s for location %s is still connected", worker.WorkerID, locationId)
		}
	}, 60*time.Second, pollingInterval).Should(Succeed())
}

// verifySyntheticsWorkerExecutesHttpCheck lets the outbound-connector mock dispatch an HTTP check against the mock's
// own readiness endpoint to the synthetics-worker with the given worker ID, and verifies that the worker executes it
// and reports a result with a passed status code assertion back.
func verifySyntheticsWorkerExecutesHttpCheck(locationId string, workerId string) {
	By("dispatching an HTTP check to the synthetics-worker")
	var taskId string
	Eventually(func(g Gomega) {
		var dispatchedToWorkerId string
		taskId, dispatchedToWorkerId = triggerOutboundConnectorMockSyntheticsTaskRequest(
			g,
			locationId,
			outboundConnectorMockReadyUrl,
		)
		g.Expect(dispatchedToWorkerId).To(Equal(workerId))
	}, 30*time.Second, pollingInterval).Should(Succeed())

	By("verifying that the synthetics-worker has executed the HTTP check")
	Eventually(func(g Gomega) {
		taskResult := findOutboundConnectorMockSyntheticsTaskResult(g, taskId)
		g.Expect(taskResult.WorkerID).To(Equal(workerId))
		g.Expect(taskResult.FailureReason).To(
			BeEmpty(), "the synthetics-worker could not execute the task: %s", taskResult.FailureMessage)
		g.Expect(taskResult.ApiVersion).To(Equal("dash0.com/v1alpha1"))

		var httpCheckResult struct {
			Response *struct {
				Status int `json:"status"`
			} `json:"response"`
			Error                    string            `json:"error"`
			FailedCriticalAssertions []json.RawMessage `json:"failedCriticalAssertions"`
			PassedCriticalAssertions []json.RawMessage `json:"passedCriticalAssertions"`
		}
		g.Expect(json.Unmarshal(taskResult.Result, &httpCheckResult)).To(Succeed())
		g.Expect(httpCheckResult.Error).To(
			BeEmpty(), "the HTTP check has failed, the result was: %s", string(taskResult.Result))
		g.Expect(httpCheckResult.Response).ToNot(BeNil())
		g.Expect(httpCheckResult.Response.Status).To(Equal(http.StatusNoContent))
		g.Expect(httpCheckResult.FailedCriticalAssertions).To(BeEmpty())
		g.Expect(httpCheckResult.PassedCriticalAssertions).To(HaveLen(1))
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
