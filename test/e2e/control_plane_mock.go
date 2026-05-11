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
	controlPlaneMockChartPath   = "test/e2e/control-plane-mock/helm-chart"
	controlPlaneMockReleaseName = "control-plane-mock"

	controlPlaneMockNamespace   = "control-plane-mock"
	controlPlaneMockServiceName = "control-plane-mock-service"
	controlPlaneMockServicePort = 8002
)

var (
	// controlPlaneMockServiceBaseUrl is the in-cluster URL the operator points
	// the IE collector at via Dash0IntelligentEdge.spec.controlPlaneApiEndpoint.
	controlPlaneMockServiceBaseUrl = fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:%d",
		controlPlaneMockServiceName,
		controlPlaneMockNamespace,
		controlPlaneMockServicePort,
	)

	// controlPlaneMockServerBaseUrl is the URL the test runner uses to fetch
	// captured requests via the nginx ingress on the host.
	controlPlaneMockServerBaseUrl    string
	controlPlaneMockServerHttpClient *http.Client

	controlPlaneMockImage ImageSpec
)

func init() {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	controlPlaneMockServerHttpClient = &http.Client{Transport: t}
}

func determineControlPlaneMockBaseUrl(port string) {
	controlPlaneMockServerBaseUrl = fmt.Sprintf("http://localhost:%s/control-plane-mock", port)
}

func determineControlPlaneMockImage() {
	repositoryPrefix, imageTag, pullPolicy := determineTestAppImageDefaults()
	controlPlaneMockImage =
		determineContainerImage(
			"CONTROL_PLANE_MOCK",
			repositoryPrefix,
			"control-plane-mock",
			imageTag,
			pullPolicy,
		)
}

func installControlPlaneMock() {
	//nolint:prealloc
	helmArgs := []string{"install",
		"--namespace",
		controlPlaneMockNamespace,
		"--create-namespace",
		"--wait",
		"--timeout",
		"60s",
		controlPlaneMockReleaseName,
		controlPlaneMockChartPath,
	}
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.repository=%s", controlPlaneMockImage.repository))
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.tag=%s", controlPlaneMockImage.tag))
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.pullPolicy=%s", controlPlaneMockImage.pullPolicy))
	Expect(runAndIgnoreOutput(exec.Command("helm", helmArgs...))).To(Succeed())
}

func uninstallControlPlaneMock() {
	Expect(runAndIgnoreOutput(
		exec.Command(
			"helm",
			"uninstall",
			controlPlaneMockReleaseName,
			"--namespace",
			controlPlaneMockNamespace,
			"--ignore-not-found",
		))).To(Succeed())
	Expect(runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"ns",
			controlPlaneMockNamespace,
			"--wait",
			"--ignore-not-found",
		))).To(Succeed())
}

func getStoredControlPlaneRequests(g Gomega) *StoredRequests {
	url := fmt.Sprintf("%s/requests", controlPlaneMockServerBaseUrl)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	g.Expect(err).NotTo(HaveOccurred())
	res, err := controlPlaneMockServerHttpClient.Do(req)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = res.Body.Close()
	}()
	body, err := io.ReadAll(res.Body)
	g.Expect(err).NotTo(HaveOccurred())
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		g.Expect(fmt.Errorf(
			"unexpected status %d from %s, body: %s",
			res.StatusCode, url, string(body))).ToNot(HaveOccurred())
	}
	var stored StoredRequests
	g.Expect(json.Unmarshal(body, &stored)).To(Succeed())
	return &stored
}
