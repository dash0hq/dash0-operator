// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/google/uuid"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/test/e2e/pkg/shared"
)

var (
	verboseHttp          bool
	e2eKubernetesContext string
	testAppBaseUrl       string
)

func determineTestAppBaseUrl(port string) {
	testAppBaseUrl = fmt.Sprintf("http://localhost:%s", port)
}

func determineTestAppImageDefaults() (string, string, string) {
	repositoryPrefix := getEnvOrDefault(
		"TEST_IMAGE_REPOSITORY_PREFIX",
		getEnvOrDefault("IMAGE_REPOSITORY_PREFIX", defaultImageRepositoryPrefix),
	)
	imageTag := getEnvOrDefault("TEST_IMAGE_TAG", defaultImageTag)
	pullPolicy := getEnvOrDefault("TEST_IMAGE_PULL_POLICY",
		getEnvOrDefault("PULL_POLICY", defaultPullPolicy),
	)
	return repositoryPrefix, imageTag, pullPolicy
}

func e2ePrint(format string, a ...any) {
	fmt.Fprintf(GinkgoWriter, "%v: "+format, slices.Concat([]interface{}{time.Now()}, a)...)
}

type neccessaryCleanupSteps struct {
	removeMetricsServer            bool
	removeTestApplicationNamespace bool
	removeOtlpSink                 bool
	removeThirdPartyCrds           bool
	removePrometheusCrds           bool
	removeIngressNginx             bool
	stopOOMDetection               bool
	removeTestApplications         bool
}

type workloadType struct {
	workloadTypeString          string
	workloadTypeStringCamelCase string
	isBatch                     bool
}

type runtimeType struct {
	runtimeTypeLabel string
	workloadName     string
	helmChartPath    string
	helmReleaseName  string
}

// getProjectDir returns the repository's root directory
func getProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	wd = strings.Replace(wd, "/test/e2e", "", -1)
	return wd, nil
}

func generateNewTestId(runtime runtimeType, workloadType workloadType) string {
	testIdUuid := uuid.New()
	testId := testIdUuid.String()
	By(fmt.Sprintf("%s %s: test ID: %s", runtime.runtimeTypeLabel, workloadType.workloadTypeString, testId))
	return testId
}

type testIdMap = map[string]string

func getTestIdFromMap(m testIdMap, runtime runtimeType, workload workloadType) string {
	return m[getTestIdMapKey(runtime, workload)]
}

func getTestIdMapKey(runtime runtimeType, workload workloadType) string {
	return fmt.Sprintf(
		"%s-%s",
		runtime.runtimeTypeLabel,
		workload.workloadTypeString,
	)
}

func workloadName(runtime runtimeType, workloadType workloadType) string {
	return fmt.Sprintf("%s-%s", runtime.workloadName, workloadType.workloadTypeString)
}

func sendRequest(g Gomega, runtime runtimeType, workloadType workloadType, route string, query string) {
	httpPathWithQuery := fmt.Sprintf("%s?%s", route, query)
	executeTestAppHttpRequest(
		g,
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString,
		httpPathWithQuery,
		200,
		"We make Observability easy for every developer.",
	)
}

func sendReadyProbe(g Gomega, runtime runtimeType, workloadType workloadType) {
	executeTestAppHttpRequest(
		g,
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString,
		"/ready",
		204,
		"",
	)
}

func executeTestAppHttpRequest(
	g Gomega,
	runtimeTypeLabel string,
	workloadTypeString string,
	httpPathWithQuery string,
	expectedStatus int,
	expectedBody string,
) {
	url := fmt.Sprintf(
		"%s/%s/%s%s",
		testAppBaseUrl,
		workloadTypeString,
		strings.ToLower(runtimeTypeLabel),
		httpPathWithQuery,
	)
	httpClient := http.Client{
		Timeout: 500 * time.Millisecond,
	}
	if verboseHttp {
		e2ePrint("%s: sending HTTP GET request\n", url)
	}
	response, err := httpClient.Get(url)
	if verboseHttp {
		if err != nil {
			e2ePrint("%s: sent    HTTP GET request: error: %v\n", url, err)
		} else {
			e2ePrint("%s: sent    HTTP GET request:  success\n", url)
		}
	}
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = response.Body.Close()
	}()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		e2ePrint("could not read http response from %s: %s\n", url, err.Error())
	}
	g.Expect(err).NotTo(HaveOccurred())
	status := response.StatusCode
	if expectedBody != "" {
		if verboseHttp {
			body := string(responseBody)
			if !strings.Contains(body, expectedBody) {
				e2ePrint("%s: sent    HTTP GET request: unexpected response body: %s\n", url, body)
			}
		}
		g.Expect(
			string(responseBody)).To(
			ContainSubstring(expectedBody),
			fmt.Sprintf("unexpected response body for workload type %s at %s, HTTP %d", workloadTypeString, url, status),
		)
	}
	if expectedStatus > 0 {
		if verboseHttp {
			if status != expectedStatus {
				e2ePrint("%s: sent    HTTP GET request: unexpected response status: %d\n", url, status)
			}
		}
		g.Expect(status).To(
			Equal(expectedStatus),
			fmt.Sprintf("unexpected status for workload type %s at %s", workloadTypeString, url),
		)
	}
}

func executeTelemetryMatcherRequest(g Gomega, requestUrl string) {
	req, err := http.NewRequest(http.MethodGet, requestUrl, nil)
	g.Expect(err).NotTo(HaveOccurred())
	res, err := telemetryMatcherHttpClient.Do(req)
	g.Expect(err).NotTo(HaveOccurred())
	body, err := io.ReadAll(res.Body)
	defer func() {
		_ = res.Body.Close()
	}()
	if err != nil {
		g.Expect(fmt.Errorf("unable to read the telemetry-matcher response payload, status code was %d, after "+
			"executing the HTTP request to %s", res.StatusCode, req.URL.String()))
	}
	var expectationResult shared.ExpectationResult
	unmarshalErr := json.Unmarshal(body, &expectationResult)

	// Note: HTTP status 404 is used if we do not find any matching telemetry object, hence we do not handle that status
	// as an "unexpected" status, but handle it below via Expect(expectationResult.Success).To(BeTrue()).
	if (res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices) &&
		res.StatusCode != http.StatusNotFound {
		if unmarshalErr != nil {
			// Unexpected HTTP status code, plus the response is not in the expected ExpectationResult shape. Log
			// the raw response body.
			g.Expect(fmt.Errorf("unexpected status code %d when executing the HTTP request to %s, response body is %s",
				res.StatusCode,
				req.URL.String(),
				string(body),
			)).ToNot(HaveOccurred())
		} else {
			// Unexpected HTTP status code, but at least the response is in the expected ExpectationResult shape.
			// Using expectationResult.Description directly in the test failure gives us a pretty-print of the
			// (potentially lengthy) description attribute, which should usually contain the error message.
			g.Expect(fmt.Errorf("unexpected status code %d when executing the HTTP request to %s, reason: %v",
				res.StatusCode,
				req.URL.String(),
				expectationResult.Description,
			)).ToNot(HaveOccurred())
		}
	}

	// At this point, we know that telemetry-matcher responded with one of the expected status codes, we need to
	// belatedly make sure that we were also able to unmarshall the response body.
	g.Expect(unmarshalErr).ToNot(
		HaveOccurred(),
		fmt.Sprintf(
			"cannot unmarshal telemetry-matcher response from %s: %s",
			req.URL.String(),
			string(body),
		))

	if expectationResult.Description != "" {
		g.Expect(expectationResult.Success).To(BeTrue(), expectationResult.Description)
	} else {
		g.Expect(expectationResult.Success).To(BeTrue())
	}
}

func initTemplateOnce(tpl *template.Template, source string, name string) *template.Template {
	if tpl == nil {
		tpl =
			template.Must(
				template.
					New(name).
					Funcs(sprig.FuncMap()).
					Parse(source))
	}
	return tpl
}

func renderResourceTemplate(tpl *template.Template, values any, filePrefix string) string {
	var resourceContent bytes.Buffer
	Expect(tpl.Execute(&resourceContent, values)).To(Succeed())

	renderedResourceFile, err := os.CreateTemp(os.TempDir(), filePrefix+"-*.yaml")
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(renderedResourceFile.Name(), resourceContent.Bytes(), 0644)).To(Succeed())

	return renderedResourceFile.Name()
}
