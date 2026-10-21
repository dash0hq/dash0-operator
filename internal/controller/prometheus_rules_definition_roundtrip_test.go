// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	. "github.com/dash0hq/dash0-operator/test/util"
)

const fullyPopulatedPrometheusRuleYaml = `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: full-rules
  annotations:
    dash0-threshold-critical: "10"
    team: platform
spec:
  groups:
  - name: group
    interval: 1m
    rules:
    - alert: alert
      expr: up == 0
      for: 5m
      keep_firing_for: 10m
      labels:
        severity: critical
      annotations:
        summary: summary
        description: description
        folderPath: /folder/path
        sharing: team:team_01abc
        dash0-threshold-degraded: "5"
        dash0.com/enabled: "false"
        dash0.com/folder-path: /infrastructure/hosts
        dash0.com/sharing: user:alice@example.com
        dash0.com/baseline-direction: above
        dash0.com/baseline-spread-floor: "0.1"
        dash0.com/volume-floor: "2"
        dash0.com/change-gate-comparison: relative_factor
        dash0.com/change-gate-value: "1.5"
        dash0.com/change-gate-baseline-window: 1h
        team: checkout
        custom: value
    - record: job:up:sum
      expr: sum(up) by (job)
      labels:
        env: production
  - name: group without interval
    rules:
    - alert: alert without labels and annotations
      expr: vector(1)
    - record: job:up:avg
      expr: avg(up)
`

/*
The payloads below are the ones the raw-HTTP implementation sent, with two deliberate differences: keep_firing_for is
sent with the field name of the Dash0 API spec instead of the legacy "keepFiringFor" (the Dash0 API accepts both), and
a nil labels map is omitted instead of being sent as null.
*/
var _ = Describe("Converting a PrometheusRule to the API client's check rule and recording rule types", func() {
	It("loses no field of the alerting and recording rules", func() {
		ctx := context.Background()
		logger := logd.FromContext(ctx)
		apiClientPool, recorder := urlRecordingApiClientPool()
		prometheusRuleReconciler := &PrometheusRuleReconciler{
			pseudoClusterUid: "cluster-uid",
			apiClientPool:    apiClientPool,
		}
		resource := map[string]any{}
		Expect(yaml.Unmarshal([]byte(fullyPopulatedPrometheusRuleYaml), &resource)).To(Succeed())

		result := prometheusRuleReconciler.MapResourceToHttpRequests(
			&preconditionValidationResult{k8sNamespace: "namespace", k8sName: "full-rules", resource: resource},
			ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: "default", Token: AuthorizationTokenTest},
			upsertAction,
			logger,
		)
		Expect(result.ValidationIssues).To(BeEmpty())
		Expect(result.SynchronizationErrors).To(BeEmpty())
		Expect(result.ApiRequests).To(HaveLen(4))
		for _, apiRequest := range result.ApiRequests {
			_, _ = apiRequest.ApiClientCall.Execute(ctx)
		}
		Expect(recorder.bodies).To(HaveLen(4))
		payloads := make([]map[string]any, 0, len(recorder.bodies))
		for _, body := range recorder.bodies {
			payload := map[string]any{}
			Expect(json.Unmarshal([]byte(body), &payload)).To(Succeed())
			payloads = append(payloads, payload)
		}

		Expect(payloads[0]).To(Equal(map[string]any{
			"name":            "group - alert",
			"expression":      "up == 0",
			"for":             "5m",
			"interval":        "1m",
			"keep_firing_for": "10m",
			"annotations": map[string]any{
				"summary":                               "summary",
				"description":                           "description",
				"folderPath":                            "/folder/path",
				"sharing":                               "team:team_01abc",
				"dash0-threshold-degraded":              "5",
				"dash0-threshold-critical":              "10",
				"dash0.com/enabled":                     "false",
				"dash0.com/folder-path":                 "/infrastructure/hosts",
				"dash0.com/sharing":                     "user:alice@example.com",
				"dash0.com/baseline-direction":          "above",
				"dash0.com/baseline-spread-floor":       "0.1",
				"dash0.com/volume-floor":                "2",
				"dash0.com/change-gate-comparison":      "relative_factor",
				"dash0.com/change-gate-value":           "1.5",
				"dash0.com/change-gate-baseline-window": "1h",
				"team":                                  "checkout",
				"custom":                                "value",
			},
			"labels": map[string]any{
				"severity": "critical",
			},
		}))
		Expect(payloads[1]).To(Equal(map[string]any{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "PrometheusRule",
			"metadata": map[string]any{
				"name": "group - job:up:sum",
			},
			"spec": map[string]any{
				"groups": []any{
					map[string]any{
						"name":     "group",
						"interval": "1m",
						"rules": []any{
							map[string]any{
								"record": "job:up:sum",
								"expr":   "sum(up) by (job)",
								"labels": map[string]any{
									"env": "production",
								},
							},
						},
					},
				},
			},
		}))
		Expect(payloads[2]).To(Equal(map[string]any{
			"name":       "group without interval - alert without labels and annotations",
			"expression": "vector(1)",
			"annotations": map[string]any{
				"dash0-threshold-critical": "10",
				"team":                     "platform",
			},
		}))
		Expect(payloads[3]).To(Equal(map[string]any{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "PrometheusRule",
			"metadata": map[string]any{
				"name": "group without interval - job:up:avg",
			},
			"spec": map[string]any{
				"groups": []any{
					map[string]any{
						"name":     "group without interval",
						"interval": "",
						"rules": []any{
							map[string]any{
								"record": "job:up:avg",
								"expr":   "avg(up)",
							},
						},
					},
				},
			},
		}))
	})
})
