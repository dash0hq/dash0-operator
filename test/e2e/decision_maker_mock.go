// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"

	. "github.com/onsi/gomega"
)

const (
	decisionMakerMockChartPath   = "test/e2e/decision-maker-mock/helm-chart"
	decisionMakerMockReleaseName = "decision-maker-mock"

	decisionMakerMockNamespace   = "decision-maker-mock"
	decisionMakerMockServiceName = "decision-maker-mock-service"
	decisionMakerMockGrpcPort    = 8011
)

var (
	// decisionMakerMockGrpcEndpoint is the in-cluster gRPC endpoint that the
	// IE Barker is pointed at via Dash0IntelligentEdge.spec.sampling.decisionMakerEndpoint.
	decisionMakerMockGrpcEndpoint = fmt.Sprintf(
		"%s.%s.svc.cluster.local:%d",
		decisionMakerMockServiceName,
		decisionMakerMockNamespace,
		decisionMakerMockGrpcPort,
	)

	// decisionMakerMockServerBaseUrl is the URL the test runner uses to query
	// the HTTP debug endpoint via the nginx ingress on the host.
	decisionMakerMockServerBaseUrl    string
	decisionMakerMockServerHttpClient *http.Client

	decisionMakerMockImage ImageSpec
)

func init() {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	decisionMakerMockServerHttpClient = &http.Client{Transport: t}
}

func determineDecisionMakerMockBaseUrl(port string) {
	decisionMakerMockServerBaseUrl = fmt.Sprintf("http://localhost:%s/decision-maker-mock", port)
}

func determineDecisionMakerMockImage() {
	repositoryPrefix, imageTag, pullPolicy := determineTestAppImageDefaults()
	decisionMakerMockImage =
		determineContainerImage(
			"DECISION_MAKER_MOCK",
			repositoryPrefix,
			"decision-maker-mock",
			imageTag,
			pullPolicy,
		)
}

func installDecisionMakerMock() {
	//nolint:prealloc
	helmArgs := []string{"install",
		"--namespace",
		decisionMakerMockNamespace,
		"--create-namespace",
		"--wait",
		"--timeout",
		"60s",
		decisionMakerMockReleaseName,
		decisionMakerMockChartPath,
	}
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.repository=%s", decisionMakerMockImage.repository))
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.tag=%s", decisionMakerMockImage.tag))
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.pullPolicy=%s", decisionMakerMockImage.pullPolicy))
	Expect(runAndIgnoreOutput(exec.Command("helm", helmArgs...))).To(Succeed())
}

func uninstallDecisionMakerMock() {
	Expect(runAndIgnoreOutput(
		exec.Command(
			"helm",
			"uninstall",
			decisionMakerMockReleaseName,
			"--namespace",
			decisionMakerMockNamespace,
			"--ignore-not-found",
		))).To(Succeed())
	Expect(runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"ns",
			decisionMakerMockNamespace,
			"--wait",
			"--ignore-not-found",
		))).To(Succeed())
}

// fetchDecisionMakerGrpcCallCounts returns the per-RPC invocation counters
// observed by the mock. Used by IE wiring tests to assert that Barker has
// established an upstream connection.
func fetchDecisionMakerGrpcCallCounts(g Gomega) map[string]int64 {
	url := fmt.Sprintf("%s/grpc-calls", decisionMakerMockServerBaseUrl)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	g.Expect(err).NotTo(HaveOccurred())
	res, err := decisionMakerMockServerHttpClient.Do(req)
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
	var counts map[string]int64
	g.Expect(json.Unmarshal(body, &counts)).To(Succeed())
	return counts
}
