// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"net/url"

	. "github.com/onsi/ginkgo/v2"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The spam filter API client calls", func() {
	DescribeTable("address the same spam filter in Dash0 as the hand-built URL",
		func(dataset string) {
			apiClientPool, recorder := urlRecordingApiClientPool()
			spamFilterReconciler := &SpamFilterReconciler{
				pseudoClusterUid: "cluster-uid",
				apiClientPool:    apiClientPool,
			}
			apiConfig := ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: dataset, Token: AuthorizationTokenTest}
			expectApiClientCallsToAddressUrl(
				spamFilterReconciler,
				recorder,
				&preconditionValidationResult{k8sNamespace: "namespace", k8sName: "name"},
				func() map[string]any {
					return map[string]any{
						"kind":     "Dash0SpamFilter",
						"metadata": map[string]any{"name": "name"},
						"spec": map[string]any{
							"contexts": []any{"log"},
							"filter":   []any{map[string]any{"key": "k8s.namespace.name", "operator": "is", "value": "x"}},
						},
					}
				},
				apiConfig,
				fmt.Sprintf(
					"%sapi/spam-filters/dash0-operator_cluster-uid_%s_namespace_name?dataset=%s",
					apiConfig.Endpoint,
					url.QueryEscape(dataset),
					url.QueryEscape(dataset),
				),
			)
		},
		Entry("plain dataset", "default"),
		Entry("dataset with a space", "my dataset"),
		Entry("dataset with a slash", "a/b"),
		Entry("dataset with a percent sign", "100%"),
		Entry("dataset with non-ASCII and reserved characters", "ä+ö&x=y"),
	)
})
