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
	dash0ApiMockChartPath   = "test/e2e/dash0-api-mock/dash0-api-mock"
	dash0ApiMockReleaseName = "dash0-api-mock"

	dash0ApiMockNamespace   = "dash0-api"
	dash0ApiMockServiceName = "dash0-api-mock-service"
	dash0ApiMockServicePort = 8001
)

var (
	dash0ApiMockServiceBaseUrl = fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:%d",
		dash0ApiMockServiceName,
		dash0ApiMockNamespace,
		dash0ApiMockServicePort,
	)

	dash0ApiMockServerExternalBaseUrl = fmt.Sprintf("http://localhost:8080/dash0-api-mock")
	dash0ApiMockServerHttpClient      *http.Client

	dash0ApiMockImage ImageSpec
)

func init() {
	// disable keep-alive
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	dash0ApiMockServerHttpClient = &http.Client{Transport: t}
}

func determineDash0ApiMockImage() {
	repositoryPrefix := getEnvOrDefault("TEST_IMAGE_REPOSITORY_PREFIX", defaultImageRepositoryPrefix)
	imageTag := getEnvOrDefault("TEST_IMAGE_TAG", defaultImageTag)
	pullPolicy := getEnvOrDefault("TEST_IMAGE_PULL_POLICY", defaultPullPolicy)
	dash0ApiMockImage =
		determineContainerImage(
			"DASH0_API_MOCK",
			repositoryPrefix,
			"dash0-api-mock",
			imageTag,
			pullPolicy,
		)
}

func rebuildDash0ApiMockImage() {
	if testImageBuildsShouldBeSkipped() {
		e2ePrint("Skipping make dash0-api-mock-image (SKIP_TEST_APP_IMAGE_BUILDS=true)\n")
		return
	}
	By(fmt.Sprintf("building the %v image", dash0ApiMockImage))
	Expect(
		runAndIgnoreOutput(
			exec.Command("make", "dash0-api-mock-image"))).To(Succeed())

	loadImageToKindClusterIfRequired(dash0ApiMockImage, nil)
}

func installDash0ApiMock() {
	helmArgs := []string{"install",
		"--namespace",
		dash0ApiMockNamespace,
		"--create-namespace",
		"--wait",
		"--timeout",
		"60s",
		dash0ApiMockReleaseName,
		dash0ApiMockChartPath,
	}
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.repository=%s", dash0ApiMockImage.repository))
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.tag=%s", dash0ApiMockImage.tag))
	helmArgs = append(helmArgs, "--set", fmt.Sprintf("image.pullPolicy=%s", dash0ApiMockImage.pullPolicy))
	Expect(runAndIgnoreOutput(exec.Command("helm", helmArgs...))).To(Succeed())
}

func uninstallDash0ApiMock() {
	Expect(runAndIgnoreOutput(
		exec.Command(
			"helm",
			"uninstall",
			dash0ApiMockReleaseName,
			"--namespace",
			dash0ApiMockNamespace,
			"--ignore-not-found",
		))).To(Succeed())
	Expect(runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"ns",
			dash0ApiMockNamespace,
			"--wait",
			"--ignore-not-found",
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
	if !isKindCluster() {
		// TODO Get rid of isKindCluster() here, either make the ingress port configurable or make the
		// ingress-nginx-controller use port 8080 in between Docker Desktop as well.
		dash0ApiMockServerExternalBaseUrl = fmt.Sprintf("http://localhost/dash0-api-mock")
	}
	fetchStoredRequestsUrl := fmt.Sprintf("%s/requests", dash0ApiMockServerExternalBaseUrl)
	req, err := http.NewRequest(http.MethodGet, fetchStoredRequestsUrl, nil)
	g.Expect(err).NotTo(HaveOccurred())
	res, err := dash0ApiMockServerHttpClient.Do(req)
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
	By("cleaning up stored requests from the Dash0 API mock server")
	Eventually(func(g Gomega) {
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/requests", dash0ApiMockServerExternalBaseUrl), nil)
		g.Expect(err).NotTo(HaveOccurred())
		res, err := dash0ApiMockServerHttpClient.Do(req)
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
