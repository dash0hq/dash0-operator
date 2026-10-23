// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	legacyCheckRuleApiPath         = "api/alerting/check-rules/"
	legacyRecordingRuleApiPath     = "api/recording-rules/"
	legacyCheckRuleListApiPath     = "api/alerting/check-rules"
	legacyRecordingRuleListApiPath = "api/recording-rules"
	checkRuleListPath              = "/api/alerting/check-rules"
	recordingRuleListPath          = "/api/recording-rules"
)

/*
legacyRuleOrigins is a frozen copy of how the raw-HTTP implementation of the Prometheus rule synchronization rendered
the origin of a rule (first return value) and the origin in the request path (second return value), including its
handling of duplicate rule names. It serves as the reference for the requests that the Dash0 API client sends.
*/
func legacyRuleOrigins(
	dataset string,
	duplicateOrigins map[string]int,
	groupName string,
	ruleName string,
) (string, string) {
	prefix := func(datasetInPrefix string) string {
		return fmt.Sprintf("dash0-operator_cluster-uid_%s_namespace_name_", datasetInPrefix)
	}
	sanitize := func(name string) string {
		name = strings.ReplaceAll(name, "/", "|")
		name = strings.ReplaceAll(name, "%", "|")
		return strings.ReplaceAll(name, ":", "|")
	}
	if ruleName == "" {
		ruleName = "unnamed rule"
	}
	originNotUrlEncoded := fmt.Sprintf("%s%s_%s", prefix(dataset), sanitize(groupName), sanitize(ruleName))
	originUrlEncoded := fmt.Sprintf(
		"%s%s_%s",
		prefix(url.PathEscape(dataset)),
		url.PathEscape(sanitize(groupName)),
		url.PathEscape(sanitize(ruleName)),
	)
	if duplicationIndexForThisOrigin, isDuplicate := duplicateOrigins[originNotUrlEncoded]; isDuplicate {
		duplicateOrigins[originNotUrlEncoded]++
		originNotUrlEncoded = originNotUrlEncoded + "_" + strconv.Itoa(duplicationIndexForThisOrigin)
		originUrlEncoded = originNotUrlEncoded + "_" + strconv.Itoa(duplicationIndexForThisOrigin)
	} else {
		duplicateOrigins[originNotUrlEncoded] = 1
	}
	return originNotUrlEncoded, originUrlEncoded
}

/*
legacyRequestUrl returns the URL of the request that the raw-HTTP implementation sent for the given (partially
URL-encoded) origin in the request path.
*/
func legacyRequestUrl(apiPath string, originInPath string, dataset string) *url.URL {
	GinkgoHelper()
	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("%s%s%s?dataset=%s", ApiEndpointStandardizedTest, apiPath, originInPath, url.QueryEscape(dataset)),
		nil,
	)
	Expect(err).ToNot(HaveOccurred())
	return req.URL
}

/*
expectUrlToBeIdentical expects that the URL of a request issued by the Dash0 API client is byte-identical to the URL
of the request that the raw-HTTP implementation sent, including the escaping of the path and the query.
*/
func expectUrlToBeIdentical(actual *url.URL, legacy *url.URL) {
	GinkgoHelper()
	Expect(actual.Host).To(Equal(legacy.Host))
	Expect(actual.EscapedPath()).To(Equal(legacy.EscapedPath()))
	Expect(actual.RawQuery).To(Equal(legacy.RawQuery))
}

/*
legacyRuleListUrl returns the URL of the request that the raw-HTTP implementation sent to list the rules of one
PrometheusRule resource, or of all PrometheusRule resources in the namespace if name is empty. The raw-HTTP
implementation rendered the query-escaped dataset into the origin prefix, and did not escape the origin prefix itself.
*/
func legacyRuleListUrl(apiPath string, dataset string, namespace string, name string) *url.URL {
	GinkgoHelper()
	originPrefix := fmt.Sprintf("dash0-operator_cluster-uid_%s_%s_", url.QueryEscape(dataset), namespace)
	if name != "" {
		originPrefix = fmt.Sprintf("%s%s_", originPrefix, name)
	}
	req, err := http.NewRequest(
		http.MethodGet,
		fmt.Sprintf(
			"%s%s?dataset=%s&originPrefix=%s",
			ApiEndpointStandardizedTest,
			apiPath,
			url.QueryEscape(dataset),
			originPrefix,
		),
		nil,
	)
	Expect(err).ToNot(HaveOccurred())
	return req.URL
}

type fakeRuleListApiFailure struct {
	statusCode int
	body       string
}

/*
fakeRuleListApi is a fake of the list endpoints of the Dash0 check rule and recording rule APIs. It records the URLs of
the requests it receives and, like the Dash0 API, responds with the rules whose origin starts with the decoded
originPrefix query parameter, in the response format of the respective API. An entry in failures makes the endpoint
with that path respond with the given status code and body instead.
*/
type fakeRuleListApi struct {
	urls                 []*url.URL
	checkRuleOrigins     []string
	recordingRuleOrigins []string
	failures             map[string]fakeRuleListApiFailure
}

func (f *fakeRuleListApi) RoundTrip(req *http.Request) (*http.Response, error) {
	f.urls = append(f.urls, req.URL)
	statusCode := http.StatusOK
	var body []byte
	if failure, ok := f.failures[req.URL.Path]; ok {
		statusCode = failure.statusCode
		body = []byte(failure.body)
	} else {
		originPrefix := req.URL.Query().Get("originPrefix")
		items := make([]any, 0)
		switch req.URL.Path {
		case checkRuleListPath:
			for _, origin := range f.checkRuleOrigins {
				if strings.HasPrefix(origin, originPrefix) {
					items = append(items, map[string]any{
						"id":      origin,
						"dataset": req.URL.Query().Get("dataset"),
						"origin":  origin,
					})
				}
			}
		case recordingRuleListPath:
			for _, origin := range f.recordingRuleOrigins {
				if strings.HasPrefix(origin, originPrefix) {
					items = append(items, map[string]any{
						"apiVersion": "monitoring.coreos.com/v1",
						"kind":       "PrometheusRule",
						"metadata": map[string]any{
							"name":   origin,
							"labels": map[string]string{"dash0.com/origin": origin},
						},
						"spec": map[string]any{"groups": []any{}},
					})
				}
			}
		}
		var err error
		if body, err = json.Marshal(items); err != nil {
			return nil, err
		}
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

func (f *fakeRuleListApi) prometheusRuleReconciler() *PrometheusRuleReconciler {
	return &PrometheusRuleReconciler{
		pseudoClusterUid: "cluster-uid",
		apiClientPool: NewApiClientPool(
			dash0apiclient.NewTransport(dash0apiclient.WithBaseTransport(f), dash0apiclient.WithTransportMaxRetries(0)),
			"test",
		),
	}
}

type ruleUnderTest struct {
	alert  string
	record string
}

var _ = Describe("The Prometheus rule API client calls", func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	prometheusRuleResource := func(groupName string, rules []ruleUnderTest) map[string]any {
		rulesRaw := make([]any, 0, len(rules))
		for _, rule := range rules {
			if rule.record != "" {
				rulesRaw = append(rulesRaw, map[string]any{"record": rule.record, "expr": "sum(up)"})
			} else {
				rulesRaw = append(rulesRaw, map[string]any{"alert": rule.alert, "expr": "up == 0"})
			}
		}
		return map[string]any{
			"metadata": map[string]any{"name": "name"},
			"spec": map[string]any{
				"groups": []any{
					map[string]any{
						"name":  groupName,
						"rules": rulesRaw,
					},
				},
			},
		}
	}

	expectSameRequestsAsRawHttpImplementation := func(dataset string, groupName string, rules []ruleUnderTest) {
		GinkgoHelper()
		apiClientPool, recorder := urlRecordingApiClientPool()
		prometheusRuleReconciler := &PrometheusRuleReconciler{
			pseudoClusterUid: "cluster-uid",
			apiClientPool:    apiClientPool,
		}
		apiConfig := ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: dataset, Token: AuthorizationTokenTest}

		duplicateOrigins := map[string]int{}
		expectedOrigins := make([]string, 0, len(rules))
		expectedUrls := make([]*url.URL, 0, len(rules))
		for _, rule := range rules {
			apiPath := legacyCheckRuleApiPath
			name := rule.alert
			if rule.record != "" {
				apiPath = legacyRecordingRuleApiPath
				name = rule.record
			}
			origin, originInPath := legacyRuleOrigins(dataset, duplicateOrigins, groupName, name)
			expectedOrigins = append(expectedOrigins, origin)
			expectedUrls = append(expectedUrls, legacyRequestUrl(apiPath, originInPath, dataset))
		}

		for _, action := range []apiAction{upsertAction, deleteAction} {
			recorder.urls = nil
			result := prometheusRuleReconciler.MapResourceToApiCalls(
				&preconditionValidationResult{
					k8sNamespace: "namespace",
					k8sName:      "name",
					resource:     prometheusRuleResource(groupName, rules),
				},
				apiConfig,
				action,
				logger,
			)
			Expect(result.ValidationIssues).To(BeEmpty())
			Expect(result.SynchronizationErrors).To(BeEmpty())
			Expect(result.OriginsInResource).To(Equal(expectedOrigins))
			Expect(result.ApiRequests).To(HaveLen(len(rules)))
			for i, apiRequest := range result.ApiRequests {
				Expect(apiRequest.Origin).To(Equal(expectedOrigins[i]))
				_, _ = apiRequest.ApiClientCall.Execute(ctx)
			}
			Expect(recorder.urls).To(HaveLen(len(rules)))
			for i, recordedUrl := range recorder.urls {
				expectUrlToBeIdentical(recordedUrl, expectedUrls[i])
				Expect(result.ApiRequests[i].ApiClientCall.Url).To(Equal(recordedUrl.String()))
			}
		}
	}

	It("renders the same origin and URL as before for names with special characters", func() {
		origin, originInPath := legacyRuleOrigins("default", map[string]int{}, "dash0/group 1", "Pod: crash 100% ü+&")
		Expect(origin).To(Equal("dash0-operator_cluster-uid_default_namespace_name_dash0|group 1_Pod| crash 100| ü+&"))
		Expect(legacyRequestUrl(legacyCheckRuleApiPath, originInPath, "default").String()).To(Equal(
			ApiEndpointStandardizedTest +
				"api/alerting/check-rules/" +
				"dash0-operator_cluster-uid_default_namespace_name_dash0%7Cgroup%201_Pod%7C%20crash%20100%7C%20%C3%BC+&" +
				"?dataset=default",
		))

		apiClientPool, recorder := urlRecordingApiClientPool()
		prometheusRuleReconciler := &PrometheusRuleReconciler{pseudoClusterUid: "cluster-uid", apiClientPool: apiClientPool}
		result := prometheusRuleReconciler.MapResourceToApiCalls(
			&preconditionValidationResult{
				k8sNamespace: "namespace",
				k8sName:      "name",
				resource:     prometheusRuleResource("dash0/group 1", []ruleUnderTest{{alert: "Pod: crash 100% ü+&"}}),
			},
			ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: "default", Token: AuthorizationTokenTest},
			upsertAction,
			logger,
		)
		Expect(result.ApiRequests).To(HaveLen(1))
		_, _ = result.ApiRequests[0].ApiClientCall.Execute(ctx)
		Expect(recorder.urls).To(HaveLen(1))
		Expect(recorder.urls[0].String()).To(Equal(
			ApiEndpointStandardizedTest +
				"api/alerting/check-rules/" +
				"dash0-operator_cluster-uid_default_namespace_name_dash0%7Cgroup%201_Pod%7C%20crash%20100%7C%20%C3%BC+&" +
				"?dataset=default",
		))
	})

	DescribeTable("address the same rules in Dash0 as the raw-HTTP implementation",
		func(dataset string) {
			expectSameRequestsAsRawHttpImplementation(
				dataset,
				"dash0/group 1",
				[]ruleUnderTest{
					{alert: "Pod: crash/looping 100% ü+&"},
					{record: "job:http_requests:rate5m"},
					{alert: "a;b,c=d@e$f?g#h"},
				},
			)
		},
		Entry("plain dataset", "default"),
		Entry("dataset with a space", "my dataset"),
		Entry("dataset with a slash", "a/b"),
		Entry("dataset with a percent sign", "100%"),
		Entry("dataset with non-ASCII and reserved characters", "ä+ö&x=y"),
	)

	DescribeTable("address the same rules in Dash0 as the raw-HTTP implementation for duplicate rule names",
		func(groupName string, alertName string, recordName string) {
			expectSameRequestsAsRawHttpImplementation(
				"default",
				groupName,
				[]ruleUnderTest{
					{alert: alertName},
					{record: recordName},
					{alert: alertName},
					{record: recordName},
					{alert: alertName},
					{record: alertName},
				},
			)
		},
		Entry("plain names", "group", "alert", "job:up:sum"),
		Entry(
			"names with spaces, slashes, percent signs, colons and non-ASCII characters",
			"dash0/group 1",
			"Pod: crash/looping 100% ü+&",
			"job:http_requests:rate5m",
		),
	)

	DescribeTable("delete orphaned rules by their origin",
		func(dataset string, orphanOrigin string, expectedEscapedOrigin string) {
			apiClientPool, recorder := urlRecordingApiClientPool()
			prometheusRuleReconciler := &PrometheusRuleReconciler{
				pseudoClusterUid: "cluster-uid",
				apiClientPool:    apiClientPool,
			}
			apiConfig := ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: dataset, Token: AuthorizationTokenTest}

			deleteRequests := prometheusRuleReconciler.CreateDeleteRequests(
				apiConfig,
				[]string{orphanOrigin},
				nil,
			)
			Expect(deleteRequests).To(HaveLen(2))
			for _, deleteRequest := range deleteRequests {
				Expect(deleteRequest.Origin).To(Equal(orphanOrigin))
				Expect(deleteRequest.ItemName).To(Equal(orphanOrigin + " (deleted)"))
				Expect(deleteRequest.ApiClientCall.Method).To(Equal(http.MethodDelete))
				_, _ = deleteRequest.ApiClientCall.Execute(ctx)
			}

			Expect(recorder.urls).To(HaveLen(2))
			Expect(recorder.urls[0].Path).To(Equal(checkRuleApiBasePath + orphanOrigin))
			Expect(recorder.urls[0].EscapedPath()).To(Equal(checkRuleApiBasePath + expectedEscapedOrigin))
			expectUrlToBeIdentical(recorder.urls[0], legacyRequestUrl(legacyCheckRuleApiPath, orphanOrigin, dataset))
			Expect(recorder.urls[1].Path).To(Equal(recordingRuleApiBasePath + orphanOrigin))
			Expect(recorder.urls[1].EscapedPath()).To(Equal(recordingRuleApiBasePath + expectedEscapedOrigin))
			expectUrlToBeIdentical(recorder.urls[1], legacyRequestUrl(legacyRecordingRuleApiPath, orphanOrigin, dataset))
			for i, recordedUrl := range recorder.urls {
				Expect(recordedUrl.RawQuery).To(Equal("dataset=" + url.QueryEscape(dataset)))
				Expect(deleteRequests[i].ApiClientCall.Url).To(Equal(recordedUrl.String()))
			}
		},
		Entry(
			"plain dataset",
			"default",
			"dash0-operator_cluster-uid_default_namespace_name_dash0|group 1_Pod| crash|looping 100| ü+&",
			"dash0-operator_cluster-uid_default_namespace_name_dash0%7Cgroup%201_Pod%7C%20crash%7Clooping%20100%7C%20%C3%BC+&",
		),
		Entry(
			"dataset with a space",
			"my dataset",
			"dash0-operator_cluster-uid_my dataset_namespace_name_group_alert",
			"dash0-operator_cluster-uid_my%20dataset_namespace_name_group_alert",
		),
		Entry(
			"dataset with non-ASCII and reserved characters",
			"ä+ö&x=y",
			"dash0-operator_cluster-uid_ä+ö&x=y_namespace_name_group_alert",
			"dash0-operator_cluster-uid_%C3%A4+%C3%B6&x=y_namespace_name_group_alert",
		),
		Entry(
			"duplicate rule",
			"default",
			"dash0-operator_cluster-uid_default_namespace_name_group_alert_1",
			"dash0-operator_cluster-uid_default_namespace_name_group_alert_1",
		),
	)

	DescribeTable("list the existing origins with the same URLs as the raw-HTTP implementation",
		func(dataset string, namespace string, name string) {
			fakeApi := &fakeRuleListApi{}
			prometheusRuleReconciler := fakeApi.prometheusRuleReconciler()
			apiConfig := ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: dataset, Token: AuthorizationTokenTest}

			origins, err := prometheusRuleReconciler.FetchExistingResourceOrigins(
				ctx,
				&preconditionValidationResult{k8sNamespace: namespace, k8sName: name},
				apiConfig,
				logger,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(origins).To(BeEmpty())
			origins, err = prometheusRuleReconciler.FetchExistingNamespaceOrigins(ctx, namespace, apiConfig, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(origins).To(BeEmpty())

			Expect(fakeApi.urls).To(HaveLen(4))
			expectUrlToBeIdentical(fakeApi.urls[0], legacyRuleListUrl(legacyCheckRuleListApiPath, dataset, namespace, name))
			expectUrlToBeIdentical(
				fakeApi.urls[1],
				legacyRuleListUrl(legacyRecordingRuleListApiPath, dataset, namespace, name),
			)
			expectUrlToBeIdentical(fakeApi.urls[2], legacyRuleListUrl(legacyCheckRuleListApiPath, dataset, namespace, ""))
			expectUrlToBeIdentical(fakeApi.urls[3], legacyRuleListUrl(legacyRecordingRuleListApiPath, dataset, namespace, ""))

			resourceOriginPrefix := fmt.Sprintf("dash0-operator_cluster-uid_%s_%s_%s_", dataset, namespace, name)
			namespaceOriginPrefix := fmt.Sprintf("dash0-operator_cluster-uid_%s_%s_", dataset, namespace)
			for i, expectedOriginPrefix := range []string{
				resourceOriginPrefix,
				resourceOriginPrefix,
				namespaceOriginPrefix,
				namespaceOriginPrefix,
			} {
				Expect(fakeApi.urls[i].Query()).To(Equal(url.Values{
					"dataset":      {dataset},
					"originPrefix": {expectedOriginPrefix},
				}))
			}
			Expect(prometheusRuleReconciler.renderCheckRuleListUrl(resourceOriginPrefix, apiConfig.Endpoint, dataset)).
				To(Equal(fakeApi.urls[0].String()))
			Expect(prometheusRuleReconciler.renderRecordingRuleListUrl(resourceOriginPrefix, apiConfig.Endpoint, dataset)).
				To(Equal(fakeApi.urls[1].String()))
		},
		Entry("plain dataset", "default", "namespace", "name"),
		Entry("dataset with a space", "my dataset", "namespace", "name"),
		Entry("dataset with a slash", "a/b", "namespace", "name"),
		Entry("dataset with a percent sign", "100%", "namespace", "name"),
		Entry("dataset with non-ASCII and reserved characters", "ä+ö&x=y", "namespace", "name"),
		Entry("namespace and name with dashes and dots", "default", "team-a-prod", "rules.v1-2"),
		Entry(
			"dataset with reserved characters, namespace and name with dashes and dots",
			"ä+ö&x=y _%",
			"team-a-prod",
			"rules.v1-2",
		),
	)

	DescribeTable("return exactly the origins that IsOriginOfResource matches",
		func(dataset string) {
			fakeApi := &fakeRuleListApi{}
			prometheusRuleReconciler := fakeApi.prometheusRuleReconciler()
			apiConfig := ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: dataset, Token: AuthorizationTokenTest}
			rules := []ruleUnderTest{
				{alert: "Pod: crash/looping 100% ü+&"},
				{record: "job:http_requests:rate5m"},
				{alert: "a;b,c=d@e$f?g#h"},
			}
			newResource := func(namespace string, name string) *preconditionValidationResult {
				return &preconditionValidationResult{
					k8sNamespace: namespace,
					k8sName:      name,
					resource:     prometheusRuleResource("dash0/group 1", rules),
				}
			}
			resource := newResource("namespace", "name")
			otherResourceInNamespace := newResource("namespace", "name-2")
			resourceInOtherNamespace := newResource("namespace-2", "name")
			originsPerResource := make(map[*preconditionValidationResult][]string)
			for _, candidate := range []*preconditionValidationResult{
				resource,
				otherResourceInNamespace,
				resourceInOtherNamespace,
			} {
				result := prometheusRuleReconciler.MapResourceToApiCalls(candidate, apiConfig, upsertAction, logger)
				Expect(result.OriginsInResource).To(HaveLen(len(rules)))
				for i, rule := range rules {
					if rule.record != "" {
						fakeApi.recordingRuleOrigins = append(fakeApi.recordingRuleOrigins, result.OriginsInResource[i])
					} else {
						fakeApi.checkRuleOrigins = append(fakeApi.checkRuleOrigins, result.OriginsInResource[i])
					}
				}
				originsPerResource[candidate] = result.OriginsInResource
			}

			origins, err := prometheusRuleReconciler.FetchExistingResourceOrigins(ctx, resource, apiConfig, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(origins).To(ConsistOf(originsPerResource[resource]))
			for candidate, originsOfCandidate := range originsPerResource {
				for _, origin := range originsOfCandidate {
					Expect(prometheusRuleReconciler.IsOriginOfResource(origin, resource, apiConfig)).
						To(Equal(candidate == resource))
				}
			}

			origins, err = prometheusRuleReconciler.FetchExistingNamespaceOrigins(ctx, "namespace", apiConfig, logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(origins).To(ConsistOf(
				slices.Concat(originsPerResource[resource], originsPerResource[otherResourceInNamespace]),
			))
		},
		Entry("plain dataset", "default"),
		Entry("dataset with a space", "my dataset"),
		Entry("dataset with a slash", "a/b"),
		Entry("dataset with a percent sign", "100%"),
		Entry("dataset with non-ASCII and reserved characters", "ä+ö&x=y"),
	)

	DescribeTable("return an error and no origins if listing the rules fails",
		func(
			failures map[string]fakeRuleListApiFailure,
			token string,
			expectedRequests int,
			expectedStatusCode int,
			expectedError string,
		) {
			fetchFunctions := map[string]func(*PrometheusRuleReconciler, ApiConfig) ([]string, error){
				"resource": func(prometheusRuleReconciler *PrometheusRuleReconciler, apiConfig ApiConfig) ([]string, error) {
					return prometheusRuleReconciler.FetchExistingResourceOrigins(
						ctx,
						&preconditionValidationResult{k8sNamespace: "namespace", k8sName: "name"},
						apiConfig,
						logger,
					)
				},
				"namespace": func(prometheusRuleReconciler *PrometheusRuleReconciler, apiConfig ApiConfig) ([]string, error) {
					return prometheusRuleReconciler.FetchExistingNamespaceOrigins(ctx, "namespace", apiConfig, logger)
				},
			}
			for fetchType, fetch := range fetchFunctions {
				By(fetchType)
				fakeApi := &fakeRuleListApi{
					checkRuleOrigins:     []string{"dash0-operator_cluster-uid_default_namespace_name_group_alert"},
					recordingRuleOrigins: []string{"dash0-operator_cluster-uid_default_namespace_name_group_record"},
					failures:             failures,
				}
				origins, err := fetch(
					fakeApi.prometheusRuleReconciler(),
					ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: "default", Token: token},
				)
				Expect(err).To(MatchError(MatchRegexp(expectedError)))
				Expect(origins).To(BeNil())
				Expect(httpStatusCodeFromError(err)).To(Equal(expectedStatusCode))
				Expect(fakeApi.urls).To(HaveLen(expectedRequests))
			}
		},
		Entry(
			"the check rule list fails",
			map[string]fakeRuleListApiFailure{
				checkRuleListPath: {statusCode: http.StatusForbidden, body: `{"error":{"code":403,"message":"forbidden"}}`},
			},
			AuthorizationTokenTest,
			1,
			http.StatusForbidden,
			"^unexpected status code 403 when trying to fetch existing origins: GET "+
				"https://api\\.dash0\\.com/api/alerting/check-rules\\?dataset=default&"+
				"originPrefix=dash0-operator_cluster-uid_default_namespace_",
		),
		Entry(
			"the recording rule list fails after the check rule list has succeeded",
			map[string]fakeRuleListApiFailure{
				recordingRuleListPath: {
					statusCode: http.StatusServiceUnavailable,
					body:       `{"error":{"code":503,"message":"unavailable"}}`,
				},
			},
			AuthorizationTokenTest,
			2,
			http.StatusServiceUnavailable,
			"^unexpected status code 503 when trying to fetch existing origins: GET "+
				"https://api\\.dash0\\.com/api/recording-rules\\?dataset=default&"+
				"originPrefix=dash0-operator_cluster-uid_default_namespace_",
		),
		Entry(
			"the check rule list response cannot be parsed",
			map[string]fakeRuleListApiFailure{
				checkRuleListPath: {statusCode: http.StatusOK, body: `{"not":"a list"}`},
			},
			AuthorizationTokenTest,
			1,
			0,
			"list check rules failed",
		),
		Entry(
			"the recording rule list response cannot be parsed after the check rule list has succeeded",
			map[string]fakeRuleListApiFailure{
				recordingRuleListPath: {statusCode: http.StatusOK, body: `[{"metadata":"not an object"}]`},
			},
			AuthorizationTokenTest,
			2,
			0,
			"list recording rules failed",
		),
		Entry(
			"the auth token is invalid",
			nil,
			"invalid-token",
			0,
			http.StatusUnauthorized,
			"^unable to fetch existing origins: GET .*: the Dash0 auth token is invalid",
		),
	)
})
