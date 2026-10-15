// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	. "github.com/onsi/gomega"
)

const (
	outboundConnectorMockChartPath   = "test/e2e/outbound-connector-mock/helm-chart"
	outboundConnectorMockReleaseName = "outbound-connector-mock"

	outboundConnectorMockNamespace   = "outbound-connector-mock"
	outboundConnectorMockServiceName = "outbound-connector-mock-service"
	outboundConnectorMockGrpcPort    = 8022
	outboundConnectorMockDebugPort   = 8024
)

// outboundConnectorMockClientInfo mirrors the JSON returned by the mock's GET /clients endpoint: one entry per client
// currently subscribed via SubscribeToCommandRequests, including the gRPC metadata it announced itself with.
type outboundConnectorMockClientInfo struct {
	ClientID      string    `json:"clientId"`
	Authorization string    `json:"authorization"`
	ConnectedAt   time.Time `json:"connectedAt"`
}

// outboundConnectorMockCommandResponse mirrors the JSON returned by the mock's GET /command-responses endpoint: one
// entry per CommandResponse the mock has received back from a client.
type outboundConnectorMockCommandResponse struct {
	RequestID string `json:"requestId"`
	ExitCode  int32  `json:"exitCode"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Timeout   bool   `json:"timeout"`
}

// outboundConnectorMockSyntheticsWorker mirrors the JSON returned by the mock's GET /synthetics-workers endpoint: one
// entry per synthetics-worker currently subscribed via SyntheticTaskService/Subscribe, including the gRPC metadata and
// the Hello it announced itself with.
type outboundConnectorMockSyntheticsWorker struct {
	WorkerID      string    `json:"workerId"`
	LocationID    string    `json:"locationId"`
	WorkerVersion string    `json:"workerVersion"`
	Authorization string    `json:"authorization"`
	ConnectedAt   time.Time `json:"connectedAt"`
	Heartbeats    int       `json:"heartbeats"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
}

// outboundConnectorMockSyntheticsTaskResult mirrors the JSON returned by the mock's GET /synthetics-task-results
// endpoint: one entry per TaskResult the mock has received back from a synthetics-worker.
type outboundConnectorMockSyntheticsTaskResult struct {
	TaskID         string          `json:"taskId"`
	WorkerID       string          `json:"workerId"`
	ApiVersion     string          `json:"apiVersion"`
	Result         json.RawMessage `json:"result"`
	FailureReason  string          `json:"failureReason"`
	FailureMessage string          `json:"failureMessage"`
}

var (
	// outboundConnectorMockGrpcEndpoint is the in-cluster gRPC endpoint that the agent0-connector and the
	// synthetics-worker are pointed at via the operator.agent0Connector.serverAddress and
	// operator.syntheticsWorker.serverAddress Helm values.
	outboundConnectorMockGrpcEndpoint = fmt.Sprintf(
		"%s.%s.svc.cluster.local:%d",
		outboundConnectorMockServiceName,
		outboundConnectorMockNamespace,
		outboundConnectorMockGrpcPort,
	)

	// outboundConnectorMockReadyUrl is the in-cluster URL of the mock's readiness endpoint, which answers with 204. It
	// serves as the target of the HTTP checks the mock dispatches to the synthetics-worker.
	outboundConnectorMockReadyUrl = fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:%d/ready",
		outboundConnectorMockServiceName,
		outboundConnectorMockNamespace,
		outboundConnectorMockDebugPort,
	)

	// outboundConnectorMockServerBaseUrl is the URL the test runner uses to query the HTTP control/debug endpoint via
	// the nginx ingress on the host.
	outboundConnectorMockServerBaseUrl    string
	outboundConnectorMockServerHttpClient *http.Client

	outboundConnectorMockImage ImageSpec
)

func init() {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	outboundConnectorMockServerHttpClient = &http.Client{Transport: t}
}

func determineOutboundConnectorMockBaseUrl(port string) {
	outboundConnectorMockServerBaseUrl = fmt.Sprintf("http://localhost:%s/outbound-connector-mock", port)
}

func determineOutboundConnectorMockImage() {
	repositoryPrefix, imageTag, pullPolicy := determineTestAppImageDefaults()
	outboundConnectorMockImage =
		determineContainerImage(
			"OUTBOUND_CONNECTOR_MOCK",
			repositoryPrefix,
			"outbound-connector-mock",
			imageTag,
			pullPolicy,
		)
}

func installOutboundConnectorMock() {
	//nolint:prealloc
	helmArgs := []string{"install",
		"--namespace",
		outboundConnectorMockNamespace,
		"--create-namespace",
		"--wait",
		"--timeout",
		"60s",
		outboundConnectorMockReleaseName,
		outboundConnectorMockChartPath,
	}
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.repository=%s", outboundConnectorMockImage.repository))
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.tag=%s", outboundConnectorMockImage.tag))
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.pullPolicy=%s", outboundConnectorMockImage.pullPolicy))
	Expect(runAndIgnoreOutput(exec.Command("helm", helmArgs...))).To(Succeed())
}

func uninstallOutboundConnectorMock() {
	Expect(runAndIgnoreOutput(
		exec.Command(
			"helm",
			"uninstall",
			outboundConnectorMockReleaseName,
			"--namespace",
			outboundConnectorMockNamespace,
			"--ignore-not-found",
		))).To(Succeed())
	Expect(runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"ns",
			outboundConnectorMockNamespace,
			"--wait",
			"--ignore-not-found",
		))).To(Succeed())
}

// fetchOutboundConnectorMockClients returns the clients currently subscribed to the mock. Used to assert that the
// agent0-connector has established its connection with the expected gRPC metadata.
func fetchOutboundConnectorMockClients(g Gomega) []outboundConnectorMockClientInfo {
	url := fmt.Sprintf("%s/clients", outboundConnectorMockServerBaseUrl)
	var clients []outboundConnectorMockClientInfo
	doOutboundConnectorMockJsonRequest(g, http.MethodGet, url, nil, &clients)
	return clients
}

// fetchOutboundConnectorMockCommandResponses returns the command responses the mock has received back from clients.
func fetchOutboundConnectorMockCommandResponses(g Gomega) []outboundConnectorMockCommandResponse {
	url := fmt.Sprintf("%s/command-responses", outboundConnectorMockServerBaseUrl)
	var responses []outboundConnectorMockCommandResponse
	doOutboundConnectorMockJsonRequest(g, http.MethodGet, url, nil, &responses)
	return responses
}

// findOutboundConnectorMockCommandResponse returns the command response the mock has received for the given request ID,
// failing the assertion if no response for that request has arrived yet.
func findOutboundConnectorMockCommandResponse(g Gomega, requestId string) *outboundConnectorMockCommandResponse {
	responses := fetchOutboundConnectorMockCommandResponses(g)
	var response *outboundConnectorMockCommandResponse
	for i := range responses {
		if responses[i].RequestID == requestId {
			response = &responses[i]
			break
		}
	}
	g.Expect(response).ToNot(BeNil(), "expected a command response for request ID %s, got %v", requestId, responses)
	return response
}

// triggerOutboundConnectorMockCommandRequest instructs the mock to push a command request down the stream of the client
// with the given client ID. It returns the request ID generated by the mock, which can be used to correlate the
// eventual command response.
//
//nolint:unparam
func triggerOutboundConnectorMockCommandRequest(g Gomega, clientID string, command string, arguments []string) string {
	url := fmt.Sprintf("%s/command-requests", outboundConnectorMockServerBaseUrl)
	payload, err := json.Marshal(map[string]any{
		"clientId":  clientID,
		"command":   command,
		"arguments": arguments,
	})
	g.Expect(err).NotTo(HaveOccurred())
	var response struct {
		RequestID string `json:"requestId"`
	}
	doOutboundConnectorMockJsonRequest(g, http.MethodPost, url, payload, &response)
	g.Expect(response.RequestID).NotTo(BeEmpty())
	return response.RequestID
}

// fetchOutboundConnectorMockSyntheticsWorkers returns the synthetics-workers currently subscribed to the mock.
func fetchOutboundConnectorMockSyntheticsWorkers(g Gomega) []outboundConnectorMockSyntheticsWorker {
	url := fmt.Sprintf("%s/synthetics-workers", outboundConnectorMockServerBaseUrl)
	var workers []outboundConnectorMockSyntheticsWorker
	doOutboundConnectorMockJsonRequest(g, http.MethodGet, url, nil, &workers)
	return workers
}

// findOutboundConnectorMockSyntheticsTaskResult returns the task result the mock has received for the given task ID,
// failing the assertion if no result for that task has arrived yet.
func findOutboundConnectorMockSyntheticsTaskResult(
	g Gomega,
	taskId string,
) *outboundConnectorMockSyntheticsTaskResult {
	url := fmt.Sprintf("%s/synthetics-task-results", outboundConnectorMockServerBaseUrl)
	var results []outboundConnectorMockSyntheticsTaskResult
	doOutboundConnectorMockJsonRequest(g, http.MethodGet, url, nil, &results)
	var result *outboundConnectorMockSyntheticsTaskResult
	for i := range results {
		if results[i].TaskID == taskId {
			result = &results[i]
			break
		}
	}
	g.Expect(result).ToNot(BeNil(), "expected a task result for task ID %s, got %v", taskId, results)
	return result
}

// triggerOutboundConnectorMockSyntheticsTaskRequest instructs the mock to push an HTTP check task for the given URL
// down the stream of the synthetics-worker connected for the given location ID. The check expects the status code 204.
// It returns the task ID generated by the mock and the ID of the worker the task has been sent to.
func triggerOutboundConnectorMockSyntheticsTaskRequest(g Gomega, locationId string, url string) (string, string) {
	payload, err := json.Marshal(map[string]any{
		"locationId": locationId,
		"url":        url,
	})
	g.Expect(err).NotTo(HaveOccurred())
	var response struct {
		TaskID   string `json:"taskId"`
		WorkerID string `json:"workerId"`
	}
	doOutboundConnectorMockJsonRequest(
		g,
		http.MethodPost,
		fmt.Sprintf("%s/synthetics-task-requests", outboundConnectorMockServerBaseUrl),
		payload,
		&response,
	)
	g.Expect(response.TaskID).NotTo(BeEmpty())
	return response.TaskID, response.WorkerID
}

func doOutboundConnectorMockJsonRequest(g Gomega, method string, url string, requestBody []byte, target any) {
	var bodyReader io.Reader
	if requestBody != nil {
		bodyReader = bytes.NewReader(requestBody)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	g.Expect(err).NotTo(HaveOccurred())
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := outboundConnectorMockServerHttpClient.Do(req)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()
	body, err := io.ReadAll(res.Body)
	g.Expect(err).NotTo(HaveOccurred())
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		g.Expect(fmt.Errorf("unexpected status code %d when executing the HTTP request to %s, response body is %s",
			res.StatusCode,
			url,
			string(body),
		)).ToNot(HaveOccurred())
	}
	g.Expect(json.Unmarshal(body, target)).To(Succeed())
}
