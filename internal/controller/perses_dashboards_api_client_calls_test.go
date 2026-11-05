// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The Perses dashboard API client calls", func() {
	DescribeTable("address the same dashboard in Dash0 as the hand-built URL",
		func(dataset string) {
			apiClientPool, recorder := urlRecordingApiClientPool()
			persesDashboardReconciler := &PersesDashboardReconciler{
				pseudoClusterUid: "cluster-uid",
				apiClientPool:    apiClientPool,
			}
			apiConfig := ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: dataset, Token: AuthorizationTokenTest}
			expectApiClientCallsToAddressUrl(
				persesDashboardReconciler,
				recorder,
				&preconditionValidationResult{k8sNamespace: "namespace", k8sName: "name"},
				func() map[string]any {
					return map[string]any{
						"metadata": map[string]any{"name": "name"},
						"spec":     map[string]any{},
					}
				},
				apiConfig,
				fmt.Sprintf(
					"%sapi/dashboards/dash0-operator_cluster-uid_%s_namespace_name?dataset=%s",
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

	DescribeTable("send panel annotations to the Dash0 API unchanged",
		func(dashboardYaml string) {
			dashboard := map[string]any{}
			Expect(yaml.Unmarshal([]byte(dashboardYaml), &dashboard)).To(Succeed())
			apiClientPool, recorder := urlRecordingApiClientPool()
			persesDashboardReconciler := &PersesDashboardReconciler{
				pseudoClusterUid: "cluster-uid",
				apiClientPool:    apiClientPool,
			}
			ctx := context.Background()
			result := persesDashboardReconciler.MapResourceToApiCalls(
				&preconditionValidationResult{k8sNamespace: "namespace", k8sName: "name", resource: dashboard},
				ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: "default", Token: AuthorizationTokenTest},
				upsertAction,
				logd.FromContext(ctx),
			)
			Expect(result.ApiRequests).To(HaveLen(1))
			_, _ = result.ApiRequests[0].ApiClientCall.Execute(ctx)

			Expect(recorder.bodies).To(HaveLen(1))
			requestBody := map[string]any{}
			Expect(json.Unmarshal([]byte(recorder.bodies[0]), &requestBody)).To(Succeed())
			Expect(ReadFromMap(requestBody, []string{"spec", "panels", "errors", "spec", "annotations"})).To(Equal([]any{
				map[string]any{
					"display": map[string]any{
						"name":        "Error logs",
						"description": "Error logs of the checkout service",
						"color":       "#ff0000",
						"hidden":      false,
					},
					"plugin": map[string]any{
						"kind": "LogsAnnotation",
						"spec": map[string]any{
							"filter":       "service.name = checkout",
							"unknownField": map[string]any{"nested": []any{"a", "b"}},
						},
					},
				},
			}))
		},
		Entry("v1alpha1", `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: name
spec:
  display:
    name: Checkout
  panels:
    errors:
      kind: Panel
      spec:
        display:
          name: Errors
        plugin:
          kind: TimeSeriesChart
          spec: {}
        annotations:
          - display:
              name: Error logs
              description: Error logs of the checkout service
              color: "#ff0000"
              hidden: false
            plugin:
              kind: LogsAnnotation
              spec:
                filter: service.name = checkout
                unknownField:
                  nested: [a, b]
`),
		Entry("v1alpha2", `
apiVersion: perses.dev/v1alpha2
kind: PersesDashboard
metadata:
  name: name
spec:
  config:
    display:
      name: Checkout
    panels:
      errors:
        kind: Panel
        spec:
          display:
            name: Errors
          plugin:
            kind: TimeSeriesChart
            spec: {}
          annotations:
            - display:
                name: Error logs
                description: Error logs of the checkout service
                color: "#ff0000"
                hidden: false
              plugin:
                kind: LogsAnnotation
                spec:
                  filter: service.name = checkout
                  unknownField:
                    nested: [a, b]
`),
	)
})
