// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

var _ = Describe("Converting a Dash0SamplingRule to the API client's SamplingDefinition", func() {
	It("loses no field of the sampling rule spec", func() {
		unstructuredSamplingRule, err := structToMap(fullyPopulatedSamplingRule())
		Expect(err).ToNot(HaveOccurred())

		samplingDefinition, err :=
			mapToSamplingDefinition(unstructuredSamplingRule.Object, "full-sampling-rule", "my dataset")
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(samplingDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())

		Expect(roundTripped["kind"]).To(Equal("Dash0Sampling"))
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name": "full-sampling-rule",
			"labels": map[string]any{
				"dash0.com/dataset": "my dataset",
			},
		}))
		Expect(roundTripped["spec"]).To(Equal(map[string]any{
			"enabled": true,
			"display": map[string]any{"name": "Full Sampling Rule"},
			"conditions": map[string]any{
				"kind": "and",
				"spec": map[string]any{
					"conditions": []any{
						map[string]any{"kind": "error"},
						map[string]any{
							"kind": "ottl",
							"spec": map[string]any{"ottl": `attributes["http.route"] == "/checkout"`},
						},
						map[string]any{
							"kind": "probabilistic",
							"spec": map[string]any{"rate": 0.25},
						},
					},
				},
			},
			"rateLimit": map[string]any{"rate": float64(100)},
		}))
	})

	It("converts the rate of a top-level probabilistic condition to a number", func() {
		samplingRule := fullyPopulatedSamplingRule()
		samplingRule.Spec.Conditions = dash0v1alpha1.Dash0SamplingRuleCondition{
			Kind: "probabilistic",
			Spec: &dash0v1alpha1.Dash0SamplingRuleConditionSpec{Rate: ptr.To("0.1")},
		}
		unstructuredSamplingRule, err := structToMap(samplingRule)
		Expect(err).ToNot(HaveOccurred())

		samplingDefinition, err :=
			mapToSamplingDefinition(unstructuredSamplingRule.Object, "full-sampling-rule", "default")
		Expect(err).ToNot(HaveOccurred())
		probabilistic, err := samplingDefinition.Spec.Conditions.AsSamplingConditionProbabilistic()
		Expect(err).ToNot(HaveOccurred())
		Expect(probabilistic.Spec.Rate).To(BeNumerically("~", 0.1, 1e-6))
	})
})

func fullyPopulatedSamplingRule() *dash0v1alpha1.Dash0SamplingRule {
	return &dash0v1alpha1.Dash0SamplingRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0SamplingRule",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-sampling-rule",
			Namespace: "namespace",
			Annotations: map[string]string{
				"example.com/not-part-of-the-api": "dropped",
			},
		},
		Spec: dash0v1alpha1.Dash0SamplingRuleSpec{
			Enabled: true,
			Display: &dash0v1alpha1.Dash0SamplingRuleDisplay{
				Name: "Full Sampling Rule",
			},
			Conditions: dash0v1alpha1.Dash0SamplingRuleCondition{
				Kind: "and",
				Spec: &dash0v1alpha1.Dash0SamplingRuleConditionSpec{
					Conditions: []apiextensionsv1.JSON{
						{Raw: []byte(`{"kind":"error"}`)},
						{Raw: []byte(`{"kind":"ottl","spec":{"ottl":"attributes[\"http.route\"] == \"/checkout\""}}`)},
						{Raw: []byte(`{"kind":"probabilistic","spec":{"rate":"0.25"}}`)},
					},
				},
			},
			RateLimit: &dash0v1alpha1.Dash0SamplingRuleRateLimit{
				Rate: 100,
			},
		},
	}
}
