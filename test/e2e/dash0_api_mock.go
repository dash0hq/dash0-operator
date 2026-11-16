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

	// dash0ApiClientTimeout is the default timeout of the operator's Dash0 API client (dash0-api-client-go), i.e. the
	// time after which a synchronization attempt fails if the Dash0 API (mock) does not respond.
	dash0ApiClientTimeout = 30 * time.Second

	// iacPeriodicRetryInterval is used as the Helm value operator.iac.periodicRetry.interval for the operator deployment
	// that is shared by all specs which run against an existing operator deployment, including the "synchronizing Dash0
	// API resources" specs.
	iacPeriodicRetryInterval = 10 * time.Second

	// capturedApiRequestsTimeout is long enough for a synchronization attempt that fails with a transport error after
	// dash0ApiClientTimeout to be retried once by the operator's periodic retry (given iacPeriodicRetryInterval), even if
	// the retried request is slow to arrive itself. It is not long enough for a second failed attempt to be retried.
	capturedApiRequestsTimeout = 2 * (dash0ApiClientTimeout + iacPeriodicRetryInterval)

	capturedApiRequestsQuietPeriod = 3 * time.Second
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

func fetchCapturedApiRequest(urlRegex string, idx int) StoredRequest {
	return fetchCapturedApiRequests(urlRegex, idx, 1)[0]
}

// fetchCapturedApiRequests waits until the Dash0 API mock has captured at least idx+count requests with a URL matching
// urlRegex, and returns count of these requests, starting at index idx. Captured requests with other URLs are ignored,
// so that requests for resources of other tests do not interfere.
func fetchCapturedApiRequests(urlRegex string, idx int, count int) []StoredRequest {
	var requests []StoredRequest
	Eventually(func(g Gomega) {
		matchingRequests := getStoredApiRequestsMatching(g, urlRegex)
		g.Expect(
			len(matchingRequests) >= idx+count).To(
			BeTrue(),
			"expected at least %d requests matching %s, but got %d: %v",
			idx+count,
			urlRegex,
			len(matchingRequests),
			matchingRequests,
		)
		requests = matchingRequests[idx:(idx + count)]
	}, capturedApiRequestsTimeout, pollingInterval).Should(Succeed())
	return requests
}

// getStoredApiRequestsMatching returns the requests captured by the Dash0 API mock with a URL matching urlRegex, in the
// order in which the mock received them.
func getStoredApiRequestsMatching(g Gomega, urlRegex string) []StoredRequest {
	urlMatcher, err := regexp.Compile(urlRegex)
	g.Expect(err).NotTo(HaveOccurred())
	storedRequests := getStoredApiRequests(g)
	g.Expect(storedRequests).NotTo(BeNil())
	matchingRequests := make([]StoredRequest, 0, len(storedRequests.Requests))
	for _, req := range storedRequests.Requests {
		if urlMatcher.MatchString(req.Url) {
			matchingRequests = append(matchingRequests, req)
		}
	}
	return matchingRequests
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
	count := 0
	for _, req := range getStoredApiRequestsMatching(g, urlRegex) {
		if req.Method == method {
			count++
		}
	}
	return count
}

// waitForCapturedApiRequestsToSettle waits until the Dash0 API mock has not captured any new request for
// capturedApiRequestsQuietPeriod. Deleting a Kubernetes resource returns before the operator has sent the resulting
// DELETE requests, so this needs to be called after removing resources and before cleaning up the captured requests.
func waitForCapturedApiRequestsToSettle() {
	By("waiting until the Dash0 API mock server does not receive any more requests")
	lastCount := -1
	var lastChange time.Time
	Eventually(func(g Gomega) {
		storedRequests := getStoredApiRequests(g)
		g.Expect(storedRequests).NotTo(BeNil())
		if count := len(storedRequests.Requests); count != lastCount {
			lastCount = count
			lastChange = time.Now()
		}
		g.Expect(time.Since(lastChange)).To(BeNumerically(">=", capturedApiRequestsQuietPeriod))
	}, 30*time.Second, pollingInterval).Should(Succeed())
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
