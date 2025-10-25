// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type StoredRequest struct {
	Method string  `json:"method"`
	Url    string  `json:"url"`
	Body   *[]byte `json:"body,omitempty"`
}

type StoredRequests struct {
	Requests []StoredRequest `json:"requests"`
}

const (
	dash0ApiMockDirectory = "test/e2e/dash0-api-mock"
	dash0ApiMockManifest  = dash0ApiMockDirectory + "/dash0-api-mock.yaml"

	dash0ApiMockImageName = "dash0-api-mock"

	dash0ApiMockNamespace      = "dash0-api"
	dash0ApiMockDeploymentName = "dash0-api-mock"
	dash0ApiMockServiceName    = "dash0-api-mock-service"
	dash0ApiMockServicePort    = 8001
)

var (
	dash0ApiMockServiceBaseUrl = fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:%d",
		dash0ApiMockServiceName,
		dash0ApiMockNamespace,
		dash0ApiMockServicePort,
	)

	dash0ApiMockServerExternalBaseUrl = "http://localhost:8001"
	dash0ApiMockServerClient          *http.Client
)

func init() {
	// disable keep-alive
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	dash0ApiMockServerClient = &http.Client{Transport: t}
}

func rebuildDash0ApiMockImage() {
	By(fmt.Sprintf("building the %s image", dash0ApiMockImageName))
	Expect(
		runAndIgnoreOutput(
			exec.Command("make", "dash0-api-mock-container-image"))).To(Succeed())

	loadImageToKindClusterIfRequired(
		ImageSpec{
			repository: dash0ApiMockImageName,
			tag:        "latest",
		}, nil,
	)
}

func installDash0ApiMock() {
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"--namespace",
		dash0ApiMockNamespace,
		"-f",
		dash0ApiMockManifest,
	))).To(Succeed())

	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"wait",
		fmt.Sprintf("deployment.apps/%s", dash0ApiMockDeploymentName),
		"--for",
		"condition=Available",
		"--namespace",
		dash0ApiMockNamespace,
		"--timeout",
		"60s",
	))).To(Succeed())
}

func uninstallDash0ApiMock() {
	Expect(runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			dash0ApiMockNamespace,
			"--ignore-not-found",
			"-f",
			dash0ApiMockManifest,
		))).To(Succeed())
}

func fetchCapturedApiRequest(idx int) StoredRequest {
	return fetchCapturedApiRequests(idx, 1)[0]
}

func fetchCapturedApiRequests(idx int, count int) []StoredRequest {
	var requests []StoredRequest
	Eventually(func(g Gomega) {
		storedRequests := getStoredApiRequests(g)
		g.Expect(storedRequests).NotTo(BeNil())
		g.Expect(
			len(storedRequests.Requests) >= idx+count).To(
			BeTrue(),
			"expected at least %d requests, but got %d: %v",
			idx+count,
			len(storedRequests.Requests),
			storedRequests.Requests,
		)
		requests = storedRequests.Requests[idx:(idx + count)]
	}, 30*time.Second, pollingInterval).Should(Succeed())
	return requests
}

func getStoredApiRequests(g Gomega) *StoredRequests {
	updateUrlForKind()
	fetchStoredRequestsUrl := fmt.Sprintf("%s/requests", dash0ApiMockServerExternalBaseUrl)
	req, err := http.NewRequest(http.MethodGet, fetchStoredRequestsUrl, nil)
	g.Expect(err).NotTo(HaveOccurred())
	res, err := dash0ApiMockServerClient.Do(req)
	g.Expect(err).NotTo(HaveOccurred())
	body, err := io.ReadAll(res.Body)
	defer func() {
		_ = res.Body.Close()
	}()
	if err != nil {
		g.Expect(fmt.Errorf("unable to read the API response payload, status code was %d, after executing"+
			" the HTTP request to %s", res.StatusCode, req.URL.String()))
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		g.Expect(fmt.Errorf("unexpected status code %d when executing the HTTP request to %s, response body is %s",
			res.StatusCode,
			req.URL.String(),
			string(body),
		)).ToNot(HaveOccurred())
	}

	var storedRequests StoredRequests
	g.Expect(json.Unmarshal(body, &storedRequests)).To(Succeed())
	return &storedRequests
}

func cleanupStoredApiRequests() {
	updateUrlForKind()
	By("cleaning up stored requests from the Dash0 API mock server")
	Eventually(func(g Gomega) {
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/requests", dash0ApiMockServerExternalBaseUrl), nil)
		g.Expect(err).NotTo(HaveOccurred())
		res, err := dash0ApiMockServerClient.Do(req)
		g.Expect(err).NotTo(HaveOccurred())
		if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
			defer func() {
				_ = res.Body.Close()
			}()
			errorResponseBody, readErr := io.ReadAll(res.Body)
			if readErr != nil {
				g.Expect(fmt.Errorf("unable to read the API response payload after receiving status code %d when executing"+
					" the HTTP request to %s", res.StatusCode, req.URL.String())).To(Not(HaveOccurred()))
			}
			g.Expect(fmt.Errorf("unexpected status code %d when executing the HTTP request to %s, response body is %s",
				res.StatusCode,
				req.URL.String(),
				string(errorResponseBody),
			)).To(Not(HaveOccurred()))
		}
		defer func() {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}()
	}, 2*time.Second, 500*time.Millisecond).Should(Succeed())
}

func updateUrlForKind() {
	if isKindCluster() {
		// note: this has not been tested yet, retest next time when running the e2e tests with kind
		dash0ApiMockServerExternalBaseUrl = fmt.Sprintf("http://%s", kindClusterIngressIp)
	}
}
