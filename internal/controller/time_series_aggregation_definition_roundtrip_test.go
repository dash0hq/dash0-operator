// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

var _ = Describe("Converting a Dash0TimeSeriesAggregation to the API client's TimeSeriesAggregationDefinition", func() {
	It("loses no field of the time series aggregation spec", func() {
		unstructuredTimeSeriesAggregation, err := structToMap(fullyPopulatedTimeSeriesAggregation())
		Expect(err).ToNot(HaveOccurred())

		timeSeriesAggregationDefinition, err :=
			mapToTimeSeriesAggregationDefinition(unstructuredTimeSeriesAggregation.Object)
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(timeSeriesAggregationDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())

		Expect(roundTripped["kind"]).To(Equal("Dash0TimeSeriesAggregation"))
		Expect(roundTripped["spec"]).To(Equal(map[string]any{
			"enabled":  true,
			"priority": float64(5),
			"display": map[string]any{
				"name": "HTTP server duration",
			},
			"match": map[string]any{
				"metricNameMatcher": map[string]any{
					"operator": "is_one_of",
					"values":   []any{"http.server.duration", "http.server.request.duration"},
				},
				"otherFilters": []any{
					map[string]any{
						"key":      "k8s.namespace.name",
						"operator": "is",
						"value":    "kube-system",
					},
					map[string]any{
						"key":      "service.name",
						"operator": "is_one_of",
						"values":   []any{"frontend", "backend"},
					},
				},
			},
			"sample": map[string]any{
				"interval":   "60s",
				"delay":      "30s",
				"staleAfter": "5m",
			},
			"attributeModifications": []any{
				map[string]any{
					"kind": "drop_attributes",
					"spec": map[string]any{
						"context": "datapoint",
						"keyMatcher": map[string]any{
							"operator": "is",
							"value":    "http.route",
						},
					},
				},
				map[string]any{
					"kind": "keep_attributes",
					"spec": map[string]any{
						"context": "resource",
						"keyMatcher": map[string]any{
							"operator": "is_one_of",
							"values":   []any{"service.name", "k8s.namespace.name"},
						},
					},
				},
			},
		}))
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name":        "full-time-series-aggregation",
			"annotations": map[string]any{},
			"labels":      map[string]any{},
		}))
		Expect(roundTripped).ToNot(HaveKey("status"))
	})
})

func fullyPopulatedTimeSeriesAggregation() *dash0v1alpha1.Dash0TimeSeriesAggregation {
	return &dash0v1alpha1.Dash0TimeSeriesAggregation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0TimeSeriesAggregation",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-time-series-aggregation",
			Namespace: "namespace",
			Labels: map[string]string{
				"app.kubernetes.io/name": "dropped",
			},
			Annotations: map[string]string{
				"example.com/not-part-of-the-api": "dropped",
			},
		},
		Spec: dash0v1alpha1.Dash0TimeSeriesAggregationSpec{
			Enabled:  true,
			Priority: 5,
			Display: &dash0v1alpha1.Dash0TimeSeriesAggregationDisplay{
				Name: "HTTP server duration",
			},
			Match: dash0v1alpha1.Dash0TimeSeriesAggregationMatch{
				MetricNameMatcher: dash0v1alpha1.Dash0TimeSeriesAggregationMatcher{
					Operator: "is_one_of",
					Values:   []string{"http.server.duration", "http.server.request.duration"},
				},
				OtherFilters: []dash0v1alpha1.Dash0TimeSeriesAggregationAttributeFilter{
					{
						Key:      "k8s.namespace.name",
						Operator: "is",
						Value:    ptr.To("kube-system"),
					},
					{
						Key:      "service.name",
						Operator: "is_one_of",
						Values:   []string{"frontend", "backend"},
					},
				},
			},
			Sample: dash0v1alpha1.Dash0TimeSeriesAggregationSample{
				Interval:   "60s",
				Delay:      "30s",
				StaleAfter: "5m",
			},
			AttributeModifications: []dash0v1alpha1.Dash0TimeSeriesAggregationAttributeModification{
				{
					Kind: dash0v1alpha1.Dash0TimeSeriesAggregationDropAttributes,
					Spec: dash0v1alpha1.Dash0TimeSeriesAggregationAttributeModificationSpec{
						Context: "datapoint",
						KeyMatcher: dash0v1alpha1.Dash0TimeSeriesAggregationMatcher{
							Operator: "is",
							Value:    ptr.To("http.route"),
						},
					},
				},
				{
					Kind: dash0v1alpha1.Dash0TimeSeriesAggregationKeepAttributes,
					Spec: dash0v1alpha1.Dash0TimeSeriesAggregationAttributeModificationSpec{
						Context: "resource",
						KeyMatcher: dash0v1alpha1.Dash0TimeSeriesAggregationMatcher{
							Operator: "is_one_of",
							Values:   []string{"service.name", "k8s.namespace.name"},
						},
					},
				},
			},
		},
		Status: dash0v1alpha1.Dash0TimeSeriesAggregationStatus{
			SynchronizationStatus: "successful",
		},
	}
}
