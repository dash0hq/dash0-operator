// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	openslov1 "github.com/dash0hq/dash0-operator/api/openslo/v1"
)

var _ = Describe("Converting an SLO to the API client's SloDefinition", func() {
	It("loses no field of the SLO spec", func() {
		unstructuredSLO, err := structToMap(fullyPopulatedSLO())
		Expect(err).ToNot(HaveOccurred())

		sloDefinition, err := mapToSLODefinition(unstructuredSLO.Object)
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(sloDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())

		Expect(roundTripped["apiVersion"]).To(Equal("openslo.com/v1"))
		Expect(roundTripped["kind"]).To(Equal("SLO"))
		Expect(roundTripped["spec"]).To(Equal(map[string]any{
			"description":     "99 percent of checkout requests succeed.",
			"service":         "shop/checkout",
			"budgetingMethod": "Occurrences",
			"timeWindow": []any{
				map[string]any{
					"duration":  "28d",
					"isRolling": true,
				},
			},
			"indicator": map[string]any{
				"metadata": map[string]any{
					"name":        "checkout-success-ratio",
					"displayName": "Checkout success ratio",
				},
				"spec": map[string]any{
					"ratioMetric": map[string]any{
						"counter": true,
						"good": map[string]any{
							"metricSource": map[string]any{
								"type": "Prometheus",
								"spec": map[string]any{"query": "good_requests"},
							},
						},
						"total": map[string]any{
							"metricSource": map[string]any{
								"type": "Prometheus",
								"spec": map[string]any{"query": "all_requests"},
							},
						},
					},
				},
			},
			"objectives": []any{
				map[string]any{
					"displayName":   "99% availability",
					"target":        0.99,
					"targetPercent": float64(99),
				},
			},
		}))
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name": "full-slo",
			"labels": map[string]any{
				"app.kubernetes.io/name": "kept",
			},
			"annotations": map[string]any{
				"dash0.com/display-name":          "Checkout availability",
				"example.com/not-part-of-the-api": "kept",
			},
		}))
		Expect(roundTripped).ToNot(HaveKey("status"))
	})
})

func fullyPopulatedSLO() *openslov1.SLO {
	return &openslov1.SLO{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "openslo.com/v1",
			Kind:       "SLO",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-slo",
			Namespace: "namespace",
			Labels: map[string]string{
				"app.kubernetes.io/name": "kept",
			},
			Annotations: map[string]string{
				"dash0.com/display-name":          "Checkout availability",
				"example.com/not-part-of-the-api": "kept",
			},
		},
		Spec: openslov1.SLOSpec{
			Description:     "99 percent of checkout requests succeed.",
			Service:         "shop/checkout",
			BudgetingMethod: "Occurrences",
			TimeWindow: []openslov1.SLOTimeWindow{
				{Duration: "28d", IsRolling: true},
			},
			Indicator: openslov1.SLOIndicator{
				Metadata: &openslov1.SLOIndicatorMetadata{
					Name:        "checkout-success-ratio",
					DisplayName: "Checkout success ratio",
				},
				Spec: openslov1.SLOIndicatorSpec{
					RatioMetric: openslov1.SLORatioMetric{
						Counter: ptr.To(true),
						Good: openslov1.SLOMetricSourceWrapper{
							MetricSource: openslov1.SLOMetricSource{
								Type: "Prometheus",
								Spec: openslov1.SLOMetricSourceSpec{Query: "good_requests"},
							},
						},
						Total: openslov1.SLOMetricSourceWrapper{
							MetricSource: openslov1.SLOMetricSource{
								Type: "Prometheus",
								Spec: openslov1.SLOMetricSourceSpec{Query: "all_requests"},
							},
						},
					},
				},
			},
			Objectives: []openslov1.SLOObjective{
				{
					DisplayName:   "99% availability",
					Target:        ptr.To[float32](0.99),
					TargetPercent: ptr.To[float32](99),
				},
			},
		},
		Status: openslov1.SLOStatus{
			SynchronizationStatus: "successful",
		},
	}
}
