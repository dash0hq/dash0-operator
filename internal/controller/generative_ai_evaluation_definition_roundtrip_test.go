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

var _ = Describe("Converting a Dash0GenerativeAiEvaluation to the API client's GenerativeAiEvaluationDefinition", func() {
	roundTrip := func(output dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput) map[string]any {
		unstructuredGenerativeAiEvaluation, err := structToMap(fullyPopulatedGenerativeAiEvaluation(output))
		Expect(err).ToNot(HaveOccurred())

		generativeAiEvaluationDefinition, err :=
			mapToGenerativeAiEvaluationDefinition(unstructuredGenerativeAiEvaluation.Object)
		Expect(err).ToNot(HaveOccurred())
		serialized, err := json.Marshal(generativeAiEvaluationDefinition)
		Expect(err).ToNot(HaveOccurred())
		roundTripped := map[string]any{}
		Expect(json.Unmarshal(serialized, &roundTripped)).To(Succeed())
		return roundTripped
	}

	It("loses no field of the generative AI evaluation spec", func() {
		roundTripped := roundTrip(dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{
			Kind: "numeric",
			Spec: &dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputSpec{Threshold: ptr.To(0.75)},
		})

		Expect(roundTripped["kind"]).To(Equal("Dash0GenerativeAiEvaluation"))
		Expect(roundTripped["spec"]).To(Equal(map[string]any{
			"enabled": true,
			"display": map[string]any{
				"name":        "Answer relevance",
				"description": "Checks that the agent answers the question that was asked.",
			},
			"match": map[string]any{
				"filters": []any{
					map[string]any{
						"key":      "gen_ai.operation.name",
						"operator": "is_set",
					},
					map[string]any{
						"key":      "service.name",
						"operator": "is_one_of",
						"values":   []any{"checkout-agent", "support-agent"},
					},
					map[string]any{
						"key":      "k8s.namespace.name",
						"operator": "is",
						"value":    "checkout",
					},
				},
			},
			"sample": map[string]any{
				"rate": 0.1,
			},
			"judge": map[string]any{
				"model":              "standard",
				"systemPrompt":       "You review the answers of an AI assistant.",
				"userPrompt":         "Question: {{ gen_ai.input.messages }}\nAnswer: {{ gen_ai.output.messages }}",
				"allowPartialPrompt": true,
				"output": map[string]any{
					"kind": "numeric",
					"spec": map[string]any{"threshold": 0.75},
				},
			},
		}))
		Expect(roundTripped["metadata"]).To(Equal(map[string]any{
			"name": "full-generative-ai-evaluation",
			"annotations": map[string]any{
				"dash0.com/folder-path": "/checkout",
				"dash0.com/sharing":     "team:team_01abc",
			},
			"labels": map[string]any{},
		}))
		Expect(roundTripped).ToNot(HaveKey("status"))
	})

	DescribeTable("keeps the judge output of every output kind",
		func(output dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput, expected map[string]any) {
			Expect(roundTrip(output)).To(HaveKeyWithValue("spec",
				HaveKeyWithValue("judge", HaveKeyWithValue("output", Equal(expected)))))
		},
		Entry("numeric without a threshold",
			dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{Kind: "numeric"},
			map[string]any{"kind": "numeric"},
		),
		Entry("boolean",
			dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{Kind: "boolean"},
			map[string]any{"kind": "boolean"},
		),
		Entry("categorical",
			dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput{
				Kind: "categorical",
				Spec: &dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputSpec{
					Options: []dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutputCategoricalOption{
						{Label: "relevant", Intent: ptr.To("positive")},
						{Label: "unclear"},
						{Label: "off-topic", Intent: ptr.To("negative")},
					},
				},
			},
			map[string]any{
				"kind": "categorical",
				"spec": map[string]any{
					"options": []any{
						map[string]any{"label": "relevant", "intent": "positive"},
						map[string]any{"label": "unclear"},
						map[string]any{"label": "off-topic", "intent": "negative"},
					},
				},
			},
		),
	)
})

func fullyPopulatedGenerativeAiEvaluation(
	output dash0v1alpha1.Dash0GenerativeAiEvaluationJudgeOutput,
) *dash0v1alpha1.Dash0GenerativeAiEvaluation {
	return &dash0v1alpha1.Dash0GenerativeAiEvaluation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0GenerativeAiEvaluation",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-generative-ai-evaluation",
			Namespace: "namespace",
			Labels: map[string]string{
				"app.kubernetes.io/name": "dropped",
			},
			Annotations: map[string]string{
				"dash0.com/folder-path":           "/checkout",
				"dash0.com/sharing":               "team:team_01abc",
				"example.com/not-part-of-the-api": "dropped",
			},
		},
		Spec: dash0v1alpha1.Dash0GenerativeAiEvaluationSpec{
			Enabled: true,
			Display: dash0v1alpha1.Dash0GenerativeAiEvaluationDisplay{
				Name:        "Answer relevance",
				Description: ptr.To("Checks that the agent answers the question that was asked."),
			},
			Match: dash0v1alpha1.Dash0GenerativeAiEvaluationMatch{
				Filters: []dash0v1alpha1.Dash0GenerativeAiEvaluationAttributeFilter{
					{Key: "gen_ai.operation.name", Operator: "is_set"},
					{Key: "service.name", Operator: "is_one_of", Values: []string{"checkout-agent", "support-agent"}},
					{Key: "k8s.namespace.name", Operator: "is", Value: ptr.To("checkout")},
				},
			},
			Sample: dash0v1alpha1.Dash0GenerativeAiEvaluationSampling{
				Rate: 0.1,
			},
			Judge: dash0v1alpha1.Dash0GenerativeAiEvaluationJudge{
				Model:              "standard",
				SystemPrompt:       "You review the answers of an AI assistant.",
				UserPrompt:         "Question: {{ gen_ai.input.messages }}\nAnswer: {{ gen_ai.output.messages }}",
				AllowPartialPrompt: ptr.To(true),
				Output:             output,
			},
		},
		Status: dash0v1alpha1.Dash0GenerativeAiEvaluationStatus{
			SynchronizationStatus: "successful",
		},
	}
}
