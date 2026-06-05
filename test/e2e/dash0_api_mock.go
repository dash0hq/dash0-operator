// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
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

// apiMockResponseOverride configures the Dash0 API mock to respond to the first Times requests with HTTP method Method
// whose URL contains RouteSubstring with the given StatusCode (e.g. to simulate a transient HTTP 503/429 failure for a
// specific rule, dashboard or spam filter), after which it responds with HTTP 200 again.
type apiMockResponseOverride struct {
	// Method is the HTTP method (e.g. "PUT") the override applies to.
	Method string `json:"method"`
	// RouteSubstring is matched against the request URL: the override applies to requests whose URL contains this
	// substring (e.g. the name of a specific rule, dashboard or spam filter).
	RouteSubstring string `json:"routeSubstring"`
	StatusCode     int    `json:"statusCode"`
	Times          int    `json:"times"`
}

const (
	dash0ApiMockChartPath   = "test/e2e/dash0-api-mock/helm-chart"
	dash0ApiMockReleaseName = "dash0-api-mock"

	dash0ApiMockNamespace   = "dash0-api"
	dash0ApiMockServiceName = "dash0-api-mock-service"
	dash0ApiMockServicePort = 8001

	// apiMockFailTimesForOneSyncAttempt is the number of times the API mock must reject a request in order to make a single
	// synchronization attempt by the operator fail completely. It equals the number of HTTP requests the operator's API
	// client transport issues per sync attempt: the initial request plus the retries configured via
	// WithTransportMaxRetries(2) in internal/startup/operator_manager_startup.go (1 + 2 = 3). Failing exactly this many
	// requests makes the first sync attempt fail (so the periodic retry kicks in) while letting the first request of the
	// subsequent retry attempt succeed.
	apiMockFailTimesForOneSyncAttempt = 3
)

var (
	dash0ApiMockServiceBaseUrl = fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:%d",
		dash0ApiMockServiceName,
		dash0ApiMockNamespace,
		dash0ApiMockServicePort,
	)

	dash0ApiMockServerHttpClient *http.Client
	dash0ApiMockServerBaseUrl    string

	dash0ApiMockImage ImageSpec
)

func init() {
	// disable keep-alive
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	dash0ApiMockServerHttpClient = &http.Client{Transport: t}
}

func determineDash0ApiMockBaseUrl(port string) {
	dash0ApiMockServerBaseUrl = fmt.Sprintf("http://localhost:%s/dash0-api-mock", port)
}

func determineDash0ApiMockImage() {
	repositoryPrefix, imageTag, pullPolicy := determineTestAppImageDefaults()
	dash0ApiMockImage =
		determineContainerImage(
			"DASH0_API_MOCK",
			repositoryPrefix,
			"dash0-api-mock",
			imageTag,
			pullPolicy,
		)
}

func installDash0ApiMock() {
	//nolint:prealloc
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
	fetchStoredRequestsUrl := fmt.Sprintf("%s/requests", dash0ApiMockServerBaseUrl)
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

// configureApiMockResponseOverrides instructs the Dash0 API mock to respond with specific status codes for matching PUT
// requests (see apiMockResponseOverride). It replaces any previously configured overrides.
func configureApiMockResponseOverrides(overrides ...apiMockResponseOverride) {
	By(fmt.Sprintf("configuring response overrides on the Dash0 API mock server: %v", overrides))
	payload, err := json.Marshal(overrides)
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		req, err := http.NewRequest(
			http.MethodPut,
			fmt.Sprintf("%s/control/response-overrides", dash0ApiMockServerBaseUrl),
			bytes.NewReader(payload),
		)
		g.Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")
		res, err := dash0ApiMockServerHttpClient.Do(req)
		g.Expect(err).NotTo(HaveOccurred())
		defer func() {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}()
		g.Expect(res.StatusCode).To(BeNumerically("<", http.StatusMultipleChoices))
		g.Expect(res.StatusCode).To(BeNumerically(">=", http.StatusOK))
	}, 2*time.Second, 500*time.Millisecond).Should(Succeed())
}

// clearApiMockResponseOverrides removes all response overrides previously configured via
// configureApiMockResponseOverrides.
func clearApiMockResponseOverrides() {
	By("clearing response overrides from the Dash0 API mock server")
	Eventually(func(g Gomega) {
		req, err := http.NewRequest(
			http.MethodDelete,
			fmt.Sprintf("%s/control/response-overrides", dash0ApiMockServerBaseUrl),
			nil,
		)
		g.Expect(err).NotTo(HaveOccurred())
		res, err := dash0ApiMockServerHttpClient.Do(req)
		g.Expect(err).NotTo(HaveOccurred())
		defer func() {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}()
		g.Expect(res.StatusCode).To(BeNumerically("<", http.StatusMultipleChoices))
		g.Expect(res.StatusCode).To(BeNumerically(">=", http.StatusOK))
	}, 2*time.Second, 500*time.Millisecond).Should(Succeed())
}

// countCapturedApiRequests returns how many captured requests have the given HTTP method and a URL matching urlRegex.
func countCapturedApiRequests(g Gomega, method string, urlRegex string) int {
	storedRequests := getStoredApiRequests(g)
	g.Expect(storedRequests).NotTo(BeNil())
	count := 0
	for _, req := range storedRequests.Requests {
		if req.Method != method {
			continue
		}
		matched, err := regexp.MatchString(urlRegex, req.Url)
		g.Expect(err).NotTo(HaveOccurred())
		if matched {
			count++
		}
	}
	return count
}

func cleanupStoredApiRequests() {
	By("cleaning up stored requests from the Dash0 API mock server")
	Eventually(func(g Gomega) {
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/requests", dash0ApiMockServerBaseUrl), nil)
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
