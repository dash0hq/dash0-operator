// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"net/url"

	. "github.com/onsi/ginkgo/v2"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The signal-to-metrics API client calls", func() {
	DescribeTable("address the same signal-to-metrics rule in Dash0 as the hand-built URL",
		func(dataset string) {
			apiClientPool, recorder := urlRecordingApiClientPool()
			signalToMetricsReconciler := &SignalToMetricsReconciler{
				pseudoClusterUid: "cluster-uid",
				apiClientPool:    apiClientPool,
			}
			apiConfig := ApiConfig{
				Endpoint: ApiEndpointStandardizedTest,
				Dataset:  dataset,
				Token:    AuthorizationTokenTest,
			}
			expectApiClientCallsToAddressUrl(
				signalToMetricsReconciler,
				recorder,
				&preconditionValidationResult{k8sNamespace: "namespace", k8sName: "name"},
				func() map[string]any {
					return map[string]any{
						"kind":     "Dash0SignalToMetrics",
						"metadata": map[string]any{"name": "name"},
						"spec": map[string]any{
							"enabled": true,
							"display": map[string]any{"name": "rule"},
							"match": map[string]any{
								"signal":  "spans",
								"filters": []any{map[string]any{"key": "service.name", "operator": "is", "value": "x"}},
							},
							"output": map[string]any{"name": "metric", "interval": "60s"},
						},
					}
				},
				apiConfig,
				fmt.Sprintf(
					"%sapi/signal-to-metrics/dash0-operator_cluster-uid_%s_namespace_name?dataset=%s",
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
