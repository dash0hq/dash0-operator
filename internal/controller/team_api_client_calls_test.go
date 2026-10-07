// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The team API client calls", func() {
	DescribeTable("address the same team in Dash0 as the hand-built URL",
		func(namespace string, name string) {
			apiClientPool, recorder := urlRecordingApiClientPool()
			teamReconciler := &TeamReconciler{
				pseudoClusterUid: "cluster-uid",
				apiClientPool:    apiClientPool,
			}
			apiConfig := ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: DatasetCustomTest, Token: AuthorizationTokenTest}
			expectApiClientCallsToAddressUrl(
				teamReconciler,
				recorder,
				&preconditionValidationResult{k8sNamespace: namespace, k8sName: name},
				func() map[string]any {
					return map[string]any{
						"kind":     "Dash0Team",
						"metadata": map[string]any{"name": name},
						"spec": map[string]any{
							"display": map[string]any{
								"name":  "Team",
								"color": map[string]any{"from": "#6366F1", "to": "#8B5CF6"},
							},
							"members": []any{"alice@example.com"},
						},
					}
				},
				apiConfig,
				fmt.Sprintf("%sapi/teams/dash0-operator_cluster-uid_%s_%s", apiConfig.Endpoint, namespace, name),
			)
		},
		Entry("plain namespace and name", "namespace", "name"),
		Entry("namespace and name with dots", "my.namespace", "my.team"),
	)
})
