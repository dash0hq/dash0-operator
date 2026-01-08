// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The API Sync", Ordered, func() {

	type cleanUpMetadataTestConfig struct {
		resource map[string]interface{}
		expected map[string]interface{}
	}

	DescribeTable("cleans up resource metadata", func(testConfig cleanUpMetadataTestConfig) {
		cleanUpMetadata(testConfig.resource)
		Expect(testConfig.resource).To(Equal(testConfig.expected))
	},
		Entry("does nothing on empty resource", cleanUpMetadataTestConfig{
			resource: map[string]interface{}{},
			expected: map[string]interface{}{},
		}),

		Entry("removes managedFields", cleanUpMetadataTestConfig{
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"managedFields": []map[string]interface{}{},
					"annotations": map[string]interface{}{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]interface{}{
						"label": "value",
					},
				},
				"spec": map[string]interface{}{
					"key": "value",
				},
			},
			expected: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]interface{}{
						"label": "value",
					},
				},
				"spec": map[string]interface{}{
					"key": "value",
				},
			},
		}),

		Entry("removes last-applied-configuration annotation", cleanUpMetadataTestConfig{
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"kubectl.kubernetes.io/last-applied-configuration": "{}",
						"dash0.com/folder-path":                            "/folder",
					},
					"labels": map[string]interface{}{
						"label": "value",
					},
				},
				"spec": map[string]interface{}{
					"key": "value",
				},
			},
			expected: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]interface{}{
						"label": "value",
					},
				},
				"spec": map[string]interface{}{
					"key": "value",
				},
			},
		}),

		Entry("removes dash0.com labels", cleanUpMetadataTestConfig{
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]interface{}{
						"label":             "value",
						"dash0.com/dataset": "default",
						"dash0.com/id":      "14cdf74a-3b1c-48a3-ab6a-b97910853760",
						"dash0.com/source":  "userdefined",
						"dash0.com/version": "1",
					},
				},
				"spec": map[string]interface{}{
					"key": "value",
				},
			},
			expected: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"dash0.com/folder-path": "/folder",
					},
					"labels": map[string]interface{}{
						"label": "value",
					},
				},
				"spec": map[string]interface{}{
					"key": "value",
				},
			},
		}),
	)
})
