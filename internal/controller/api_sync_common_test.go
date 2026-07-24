// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
})
