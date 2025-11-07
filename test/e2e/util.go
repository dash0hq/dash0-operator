// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
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
	"go.opentelemetry.io/collector/pdata/pcommon"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	verboseHttp          bool
	e2eKubernetesContext string
)

func e2ePrint(format string, a ...any) {
	fmt.Fprintf(GinkgoWriter, "%v: "+format, slices.Concat([]interface{}{time.Now()}, a)...)
}

type neccessaryCleanupSteps struct {
	removeMetricsServer            bool
	removeTestApplicationNamespace bool
	removeApiMockNamespace         bool
	removeOtlpSink                 bool
	removeThirdPartyCrds           bool
	removeIngressNginx             bool
	stopOOMDetection               bool
	removeTestApplications         bool
}

type workloadType struct {
	workloadTypeString string
	basePort           int
	isBatch            bool
}

type runtimeType struct {
	runtimeTypeLabel string
	portOffset       int
	workloadName     string
	helmChartPath    string
	helmReleaseName  string
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
	executeHttpRequest(
		g,
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString,
		getPort(runtime, workloadType),
		httpPathWithQuery,
		200,
		"We make Observability easy for every developer.",
	)
}

func sendReadyProbe(g Gomega, runtime runtimeType, workloadType workloadType) {
	executeHttpRequest(
		g,
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString,
		getPort(runtime, workloadType),
		"/ready",
		204,
		"",
	)
}

func executeHttpRequest(
	g Gomega,
	runtimeTypeLabel string,
	workloadTypeString string,
	port int,
	httpPathWithQuery string,
	expectedStatus int,
	expectedBody string,
) {
	url := fmt.Sprintf("http://localhost:%d%s", port, httpPathWithQuery)
	if isKindCluster() {
		url = fmt.Sprintf(
			"http://%s/%s/%s%s",
			kindClusterIngressIp,
			workloadTypeString,
			strings.ToLower(runtimeTypeLabel),
			httpPathWithQuery,
		)
	}
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

func getPort(runtime runtimeType, workloadType workloadType) int {
	return workloadType.basePort + runtime.portOffset
}

func testImageBuildsShouldBeSkipped() bool {
	return os.Getenv("SKIP_TEST_APP_IMAGE_BUILDS") == "true"
}

func verifyResourceAttributeEquals[T any](
	resourceAttributes pcommon.Map,
	key string,
	expectedValue string,
	matchResult *ResourceMatchResult[T],
) {
	actualValue, hasValue := resourceAttributes.Get(key)
	if hasValue {
		if actualValue.Str() == expectedValue {
			matchResult.addPassedAssertion(key)
		} else {
			matchResult.addFailedAssertion(
				key,
				fmt.Sprintf("expected %s but it was %s", expectedValue, actualValue.Str()),
			)
		}
	} else {
		matchResult.addFailedAssertion(
			key,
			fmt.Sprintf("expected %s but the object had no such resource attribute", expectedValue),
		)
	}
}

func verifyResourceAttributeStartsWith[T any](
	resourceAttributes pcommon.Map,
	key string,
	expectedPrefix string,
	matchResult *ResourceMatchResult[T],
) {
	actualValue, hasValue := resourceAttributes.Get(key)
	if hasValue {
		if strings.HasPrefix(actualValue.Str(), expectedPrefix) {
			matchResult.addPassedAssertion(key)
		} else {
			matchResult.addFailedAssertion(
				key,
				fmt.Sprintf("expected a value starting with %s but it was %s", expectedPrefix, actualValue.Str()),
			)
		}
	} else {
		matchResult.addFailedAssertion(
			key,
			fmt.Sprintf(
				"expected a values starting with %s but the object had no such resource attribute",
				expectedPrefix,
			),
		)
	}
}

func verifyResourceAttributeExists[T any](
	resourceAttributes pcommon.Map,
	key string,
	matchResult *ResourceMatchResult[T],
) {
	_, hasValue := resourceAttributes.Get(key)
	if hasValue {
		matchResult.addPassedAssertion(key)
	} else {
		matchResult.addFailedAssertion(
			key,
			"expected any value but the object had no such resource attribute",
		)
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
