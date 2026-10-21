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

var _ = Describe("Converting a Dash0SignalToMetrics to the API client's SignalToMetricsDefinition", func() {
	It("loses no field of the signal-to-metrics spec", func() {
		unstructuredSignalToMetrics, err := structToMap(fullyPopulatedSignalToMetrics())
		Expect(err).ToNot(HaveOccurred())

		signalToMetricsDefinition, err := mapToSignalToMetricsDefinition(unstructuredSignalToMetrics.Object)
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(signalToMetricsDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())

		Expect(roundTripped["kind"]).To(Equal("Dash0SignalToMetrics"))
		Expect(roundTripped["spec"]).To(Equal(map[string]any{
			"enabled": false,
			"display": map[string]any{
				"name": "Checkout request duration",
			},
			"match": map[string]any{
				"signal": "spans",
				"filters": []any{
					map[string]any{
						"key":      "service.name",
						"operator": "is",
						"value":    "checkout-service",
					},
					map[string]any{
						"key":      "http.route",
						"operator": "is_one_of",
						"values":   []any{"/checkout", "/cart"},
					},
					map[string]any{
						"key":      "http.method",
						"operator": "is_set",
					},
				},
			},
			"output": map[string]any{
				"name":        "checkout.request.duration",
				"description": "Duration of checkout requests",
				"interval":    "1m",
				"keepResourceAttributes": []any{
					map[string]any{
						"operator": "is_one_of",
						"values":   []any{"service.name", "k8s.namespace.name"},
					},
				},
				"keepSignalAttributes": []any{
					map[string]any{
						"operator": "starts_with",
						"value":    "http.",
					},
				},
			},
		}))
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name": "full-signal-to-metrics",
			"annotations": map[string]any{
				"dash0.com/folder-path": "/team/checkout",
				"dash0.com/sharing":     "team:team_01abc",
			},
			"labels": map[string]any{},
		}))
		Expect(roundTripped).ToNot(HaveKey("apiVersion"))
		Expect(roundTripped).ToNot(HaveKey("status"))
	})
})

func fullyPopulatedSignalToMetrics() *dash0v1alpha1.Dash0SignalToMetrics {
	return &dash0v1alpha1.Dash0SignalToMetrics{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0SignalToMetrics",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-signal-to-metrics",
			Namespace: "namespace",
			Labels: map[string]string{
				"app.kubernetes.io/name": "dropped",
			},
			Annotations: map[string]string{
				"dash0.com/folder-path":           "/team/checkout",
				"dash0.com/sharing":               "team:team_01abc",
				"example.com/not-part-of-the-api": "dropped",
			},
		},
		Spec: dash0v1alpha1.Dash0SignalToMetricsSpec{
			Enabled: false,
			Display: dash0v1alpha1.Dash0SignalToMetricsDisplay{
				Name: "Checkout request duration",
			},
			Match: dash0v1alpha1.Dash0SignalToMetricsMatch{
				Signal: dash0v1alpha1.Dash0SignalToMetricsSignalTypeSpans,
				Filters: []dash0v1alpha1.Dash0SignalToMetricsAttributeFilter{
					{
						Key:      "service.name",
						Operator: "is",
						Value:    ptr.To("checkout-service"),
					},
					{
						Key:      "http.route",
						Operator: "is_one_of",
						Values:   []string{"/checkout", "/cart"},
					},
					{
						Key:      "http.method",
						Operator: "is_set",
					},
				},
			},
			Output: dash0v1alpha1.Dash0SignalToMetricsOutput{
				Name:        "checkout.request.duration",
				Description: ptr.To("Duration of checkout requests"),
				Interval:    "1m",
				KeepResourceAttributes: []dash0v1alpha1.Dash0SignalToMetricsMatcher{
					{
						Operator: "is_one_of",
						Values:   []string{"service.name", "k8s.namespace.name"},
					},
				},
				KeepSignalAttributes: []dash0v1alpha1.Dash0SignalToMetricsMatcher{
					{
						Operator: "starts_with",
						Value:    ptr.To("http."),
					},
				},
			},
		},
		Status: dash0v1alpha1.Dash0SignalToMetricsStatus{
			SynchronizationStatus: "successful",
		},
	}
}
