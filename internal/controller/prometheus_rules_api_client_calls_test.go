// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	legacyCheckRuleApiPath     = "api/alerting/check-rules/"
	legacyRecordingRuleApiPath = "api/recording-rules/"
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
			result := prometheusRuleReconciler.MapResourceToHttpRequests(
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
		result := prometheusRuleReconciler.MapResourceToHttpRequests(
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

			deleteRequests, synchronizationErrors := prometheusRuleReconciler.CreateDeleteRequests(
				apiConfig,
				[]string{orphanOrigin},
				nil,
				logger,
			)
			Expect(synchronizationErrors).To(BeEmpty())
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
})
