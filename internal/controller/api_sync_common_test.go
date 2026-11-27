// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The API Sync", Ordered, func() {

	type cleanUpMetadataTestConfig struct {
		resource map[string]any
		expected map[string]any
	}

	DescribeTable("cleans up resource metadata", func(testConfig cleanUpMetadataTestConfig) {
		cleanUpMetadata(testConfig.resource)
		Expect(testConfig.resource).To(Equal(testConfig.expected))
	},
		Entry("does nothing on empty resource", cleanUpMetadataTestConfig{
			resource: map[string]any{},
			expected: map[string]any{},
		}),

		Entry("removes managedFields", cleanUpMetadataTestConfig{
			resource: map[string]any{
				"metadata": map[string]any{
					"managedFields": []map[string]any{},
					"annotations": map[string]any{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]any{
						"label": "value",
					},
				},
				"spec": map[string]any{
					"key": "value",
				},
			},
			expected: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]any{
						"label": "value",
					},
				},
				"spec": map[string]any{
					"key": "value",
				},
			},
		}),

		Entry("removes last-applied-configuration annotation", cleanUpMetadataTestConfig{
			resource: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"kubectl.kubernetes.io/last-applied-configuration": "{}",
						"dash0.com/folder-path":                            "/folder",
					},
					"labels": map[string]any{
						"label": "value",
					},
				},
				"spec": map[string]any{
					"key": "value",
				},
			},
			expected: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]any{
						"label": "value",
					},
				},
				"spec": map[string]any{
					"key": "value",
				},
			},
		}),

		Entry("removes dash0.com labels", cleanUpMetadataTestConfig{
			resource: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]any{
						"label":             "value",
						"dash0.com/dataset": "default",
						"dash0.com/id":      "14cdf74a-3b1c-48a3-ab6a-b97910853760",
						"dash0.com/source":  "userdefined",
						"dash0.com/version": "1",
					},
				},
				"spec": map[string]any{
					"key": "value",
				},
			},
			expected: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]any{
						"label": "value",
					},
				},
				"spec": map[string]any{
					"key": "value",
				},
			},
		}),
	)

	Describe("stripKubernetesOnlyMetadataFields", func() {
		It("removes ObjectMeta fields the Dash0 API does not consume", func() {
			resource := map[string]any{
				"metadata": map[string]any{
					"name":                       "some-name",
					"namespace":                  "some-namespace",
					"resourceVersion":            "12345",
					"uid":                        "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
					"generation":                 int64(3),
					"creationTimestamp":          "2026-07-01T00:00:00Z",
					"deletionTimestamp":          "2026-07-02T00:00:00Z",
					"deletionGracePeriodSeconds": int64(30),
					"ownerReferences": []map[string]any{
						{"apiVersion": "v1", "kind": "Pod", "name": "owner"},
					},
					"finalizers": []string{"dash0.com/finalizer"},
					"selfLink":   "/api/v1/namespaces/some-namespace/objects/some-name",
					"labels": map[string]any{
						"team": "backend",
					},
				},
				"spec": map[string]any{
					"key": "value",
				},
			}

			stripKubernetesOnlyMetadataFields(resource)

			Expect(resource).To(Equal(map[string]any{
				"metadata": map[string]any{
					"name": "some-name",
					"labels": map[string]any{
						"team": "backend",
					},
				},
				"spec": map[string]any{
					"key": "value",
				},
			}))
		})

		It("is a no-op when metadata is missing", func() {
			resource := map[string]any{"spec": map[string]any{"key": "value"}}
			stripKubernetesOnlyMetadataFields(resource)
			Expect(resource).To(Equal(map[string]any{"spec": map[string]any{"key": "value"}}))
		})

		It("is a no-op when metadata is not a map", func() {
			resource := map[string]any{"metadata": "not-a-map"}
			stripKubernetesOnlyMetadataFields(resource)
			Expect(resource).To(Equal(map[string]any{"metadata": "not-a-map"}))
		})
	})

	Describe("resourceSyncStatus", func() {
		It("returns successful when all configs succeed and there are no validation issues", func() {
			results := synchronizationResults{
				alertingRulesTotal: 2,
				resultsPerApiConfig: []synchronizationResultPerApiConfig{
					{
						apiConfig: ApiConfig{Endpoint: "ep1"},
						successfullySynchronized: []SuccessfulSynchronizationResult{
							{ItemName: "item1"},
						},
						resourceToRequestsResult: &ResourceToRequestsResult{},
					},
					{
						apiConfig: ApiConfig{Endpoint: "ep2"},
						successfullySynchronized: []SuccessfulSynchronizationResult{
							{ItemName: "item1"},
						},
						resourceToRequestsResult: &ResourceToRequestsResult{},
					},
				},
			}
			Expect(results.resourceSyncStatus()).To(Equal(
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
			))
		})

		It("returns partially-successful when some configs succeed and some have sync errors", func() {
			results := synchronizationResults{
				alertingRulesTotal: 2,
				resultsPerApiConfig: []synchronizationResultPerApiConfig{
					{
						apiConfig: ApiConfig{Endpoint: "ep1"},
						successfullySynchronized: []SuccessfulSynchronizationResult{
							{ItemName: "item1"},
						},
						resourceToRequestsResult: &ResourceToRequestsResult{},
					},
					{
						apiConfig: ApiConfig{Endpoint: "ep2"},
						resourceToRequestsResult: &ResourceToRequestsResult{
							SynchronizationErrors: map[string]string{
								"item1": "connection refused",
							},
						},
					},
				},
			}
			Expect(results.resourceSyncStatus()).To(Equal(
				dash0common.Dash0ApiResourceSynchronizationStatusPartiallySuccessful,
			))
		})

		It("returns partially-successful when some configs succeed but there are validation issues", func() {
			results := synchronizationResults{
				alertingRulesTotal: 2,
				validationIssues: map[string][]string{
					"item2": {"missing field X"},
				},
				resultsPerApiConfig: []synchronizationResultPerApiConfig{
					{
						apiConfig: ApiConfig{Endpoint: "ep1"},
						successfullySynchronized: []SuccessfulSynchronizationResult{
							{ItemName: "item1"},
						},
						resourceToRequestsResult: &ResourceToRequestsResult{},
					},
				},
			}
			Expect(results.resourceSyncStatus()).To(Equal(
				dash0common.Dash0ApiResourceSynchronizationStatusPartiallySuccessful,
			))
		})

		It("returns failed when no configs succeed", func() {
			results := synchronizationResults{
				alertingRulesTotal: 2,
				resultsPerApiConfig: []synchronizationResultPerApiConfig{
					{
						apiConfig: ApiConfig{Endpoint: "ep1"},
						resourceToRequestsResult: &ResourceToRequestsResult{
							SynchronizationErrors: map[string]string{
								"item1": "connection refused",
							},
						},
					},
					{
						apiConfig: ApiConfig{Endpoint: "ep2"},
						resourceToRequestsResult: &ResourceToRequestsResult{
							SynchronizationErrors: map[string]string{
								"item1": "timeout",
							},
						},
					},
				},
			}
			Expect(results.resourceSyncStatus()).To(Equal(
				dash0common.Dash0ApiResourceSynchronizationStatusFailed,
			))
		})
	})

	Describe("writeCommonStatus", func() {
		type commonStatus struct {
			synchronizationStatus        dash0common.Dash0ApiResourceSynchronizationStatus
			synchronizedAt               metav1.Time
			observedGeneration           int64
			synchronizationSkippedReason string
			validationIssues             []string
		}

		var status commonStatus
		var pointers ownedResourceCommonStatus

		BeforeEach(func() {
			status = commonStatus{
				synchronizationStatus:        dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				observedGeneration:           1,
				synchronizationSkippedReason: "previous reason",
				validationIssues:             []string{"previous issue"},
			}
			pointers = ownedResourceCommonStatus{
				synchronizationStatus:        &status.synchronizationStatus,
				synchronizedAt:               &status.synchronizedAt,
				observedGeneration:           &status.observedGeneration,
				synchronizationSkippedReason: &status.synchronizationSkippedReason,
				validationIssues:             &status.validationIssues,
			}
		})

		It("writes the result of a synchronization and asks to replace the results per endpoint", func() {
			results := synchronizationResults{
				resultsPerApiConfig: []synchronizationResultPerApiConfig{
					{
						apiConfig: ApiConfig{Endpoint: "ep1"},
						successfullySynchronized: []SuccessfulSynchronizationResult{
							{ItemName: "item1"},
						},
						resourceToRequestsResult: &ResourceToRequestsResult{},
					},
				},
			}
			testStartedAt := time.Now()
			Expect(results.writeCommonStatus(3, pointers)).To(BeTrue())
			Expect(status.synchronizationStatus).To(Equal(dash0common.Dash0ApiResourceSynchronizationStatusSuccessful))
			Expect(status.observedGeneration).To(Equal(int64(3)))
			Expect(status.synchronizedAt.Time).To(BeTemporally(">=", testStartedAt))
			Expect(status.synchronizationSkippedReason).To(BeEmpty())
			Expect(status.validationIssues).To(BeNil())
		})

		It("writes a skipped synchronization and asks to keep the results per endpoint", func() {
			results := newNotSynchronizedResult("There is no Dash0 monitoring resource in namespace test.")
			Expect(results.writeCommonStatus(3, pointers)).To(BeFalse())
			Expect(status.synchronizationStatus).To(Equal(dash0common.Dash0ApiResourceSynchronizationStatusNotSynchronized))
			Expect(status.observedGeneration).To(Equal(int64(3)))
			Expect(status.synchronizationSkippedReason).To(
				Equal("There is no Dash0 monitoring resource in namespace test."))
			Expect(status.validationIssues).To(BeNil())
		})

		It("writes validation issues of the resource as failed and asks to keep the results per endpoint", func() {
			results := synchronizationResults{resourceValidationIssues: []string{"The resource has no spec."}}
			Expect(results.writeCommonStatus(3, pointers)).To(BeFalse())
			Expect(status.synchronizationStatus).To(Equal(dash0common.Dash0ApiResourceSynchronizationStatusFailed))
			Expect(status.observedGeneration).To(Equal(int64(3)))
			Expect(status.synchronizationSkippedReason).To(BeEmpty())
			Expect(status.validationIssues).To(Equal([]string{"The resource has no spec."}))
		})
	})
})

var _ = Describe("Converting API client errors", func() {
	const actionLabel = "synchronize the view \"v\": PUT https://api.dash0.com/api/views/o?dataset=d"

	It("treats 404 for a delete as success", func() {
		err := convertApiClientError(&dash0apiclient.APIError{StatusCode: http.StatusNotFound}, actionLabel, true)
		Expect(err).ToNot(HaveOccurred())
	})

	DescribeTable("keeps the status code and renders the operator's error message",
		func(statusCode int, isDelete bool) {
			err := convertApiClientError(
				&dash0apiclient.APIError{StatusCode: statusCode, Body: `{"error":"x"}`},
				actionLabel,
				isDelete,
			)
			Expect(err).To(HaveOccurred())
			Expect(httpStatusCodeFromError(err)).To(Equal(statusCode))
			Expect(err.Error()).To(Equal(fmt.Sprintf(
				`unexpected status code %d when trying to %s, response body is {"error":"x"}`,
				statusCode,
				actionLabel,
			)))
		},
		Entry("404 for a put", http.StatusNotFound, false),
		Entry("400", http.StatusBadRequest, false),
		Entry("429", http.StatusTooManyRequests, false),
		Entry("503", http.StatusServiceUnavailable, false),
		Entry("500 for a delete", http.StatusInternalServerError, true),
	)

	It("maps an invalid auth token to status code 401, which is not retried", func() {
		_, poolErr := testApiClientPool().Get(ApiEndpointStandardizedTest, "not-a-dash0-token")
		err := convertApiClientError(poolErr, actionLabel, false)
		Expect(err).To(HaveOccurred())
		Expect(httpStatusCodeFromError(err)).To(Equal(http.StatusUnauthorized))
		Expect(isRetryableHttpStatusCode(httpStatusCodeFromError(err))).To(BeFalse())
		Expect(err.Error()).To(Equal(
			"unable to " + actionLabel + `: the Dash0 auth token is invalid, it must start with "auth_" or "dash0_at_"`,
		))
	})

	It("maps errors without an HTTP response to status code 0", func() {
		transportErr := errors.New("connection refused")
		err := convertApiClientError(transportErr, actionLabel, false)
		Expect(err).To(HaveOccurred())
		Expect(httpStatusCodeFromError(err)).To(Equal(0))
		Expect(errors.Is(err, transportErr)).To(BeTrue())
		Expect(isRetryableHttpStatusCode(httpStatusCodeFromError(err))).To(BeTrue())
	})
})

var _ = Describe("The dataset in the origin", func() {
	DescribeTable("is the path-unescaped form of the query-escaped dataset",
		func(dataset string) {
			expected, err := url.PathUnescape(url.QueryEscape(dataset))
			Expect(err).ToNot(HaveOccurred())
			Expect(datasetInOrigin(dataset)).To(Equal(expected))
		},
		Entry("plain dataset", "default"),
		Entry("dataset with a space", "my dataset"),
		Entry("dataset with a slash", "a/b"),
		Entry("dataset with a percent sign", "100%"),
		Entry("dataset with non-ASCII and reserved characters", "ä+ö&x=y;z,?#"),
	)
})

// verifyOwnedResourceIsNotSynchronized verifies that the status of the given resource reports that the current
// generation of the resource has not been synchronized to the Dash0 API, for a reason containing expectedReason. The
// given object only needs to have its namespace and name set; its type determines the resource type.
func verifyOwnedResourceIsNotSynchronized(
	ctx context.Context,
	k8sClient client.Client,
	resource client.Object,
	expectedReason string,
) {
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), resource)).To(Succeed())
		unstructuredResource, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource)
		g.Expect(err).NotTo(HaveOccurred())
		status, _, _ := unstructured.NestedMap(unstructuredResource, "status")
		g.Expect(status["synchronizationStatus"]).To(
			Equal(string(dash0common.Dash0ApiResourceSynchronizationStatusNotSynchronized)))
		g.Expect(status["synchronizationSkippedReason"]).To(ContainSubstring(expectedReason))
		g.Expect(status["observedGeneration"]).To(Equal(resource.GetGeneration()))
	}).Should(Succeed())
}
