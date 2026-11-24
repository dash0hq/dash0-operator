// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"net/url"

	. "github.com/onsi/ginkgo/v2"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The generative AI evaluation API client calls", func() {
	DescribeTable("address the same generative AI evaluation in Dash0 as the hand-built URL",
		func(dataset string) {
			apiClientPool, recorder := urlRecordingApiClientPool()
			generativeAiEvaluationReconciler := &GenerativeAiEvaluationReconciler{
				pseudoClusterUid: "cluster-uid",
				apiClientPool:    apiClientPool,
			}
			apiConfig := ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: dataset, Token: AuthorizationTokenTest}
			expectApiClientCallsToAddressUrl(
				generativeAiEvaluationReconciler,
				recorder,
				&preconditionValidationResult{k8sNamespace: "namespace", k8sName: "name"},
				func() map[string]any {
					return map[string]any{
						"kind":     "Dash0GenerativeAiEvaluation",
						"metadata": map[string]any{"name": "name"},
						"spec": map[string]any{
							"enabled": true,
							"display": map[string]any{"name": "Answer relevance"},
							"match": map[string]any{
								"filters": []any{map[string]any{"key": "gen_ai.operation.name", "operator": "is_set"}},
							},
							"sample": map[string]any{"rate": 0.1},
							"judge": map[string]any{
								"model":        "light",
								"systemPrompt": "You review answers.",
								"userPrompt":   "{{ gen_ai.output.messages }}",
								"output":       map[string]any{"kind": "boolean"},
							},
						},
					}
				},
				apiConfig,
				fmt.Sprintf(
					"%sapi/generative-ai-evaluations/dash0-operator_cluster-uid_%s_namespace_name?dataset=%s",
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
