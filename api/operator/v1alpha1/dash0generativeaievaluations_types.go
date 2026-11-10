// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0GenerativeAiEvaluation is the Schema for the dash0generativeaievaluations API. It defines an eval: an LLM judge
// that scores a sample of the generative AI spans that match its filters. The name of the resource is the name of the
// eval in Dash0; it must be unique within the Dash0 dataset the eval is synchronized to.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=dash0eval;dash0evals
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
type Dash0GenerativeAiEvaluation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0GenerativeAiEvaluationSpec   `json:"spec,omitempty"`
	Status Dash0GenerativeAiEvaluationStatus `json:"status,omitempty"`
}

// Dash0GenerativeAiEvaluationSpec defines the desired state of an eval.
type Dash0GenerativeAiEvaluationSpec struct {
	// Whether the eval judges spans.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// Display configuration for the eval.
	// +kubebuilder:validation:Required
	Display Dash0GenerativeAiEvaluationDisplay `json:"display"`

	// Match selects the spans the eval judges.
	// +kubebuilder:validation:Required
	Match Dash0GenerativeAiEvaluationMatch `json:"match"`

	// Sample controls which share of the matching traces is judged.
	// +kubebuilder:validation:Required
	Sample Dash0GenerativeAiEvaluationSampling `json:"sample"`

	// Judge configures the model and the prompts that score a span.
	// +kubebuilder:validation:Required
	Judge Dash0GenerativeAiEvaluationJudge `json:"judge"`
}

// Dash0GenerativeAiEvaluationDisplay defines display configuration.
type Dash0GenerativeAiEvaluationDisplay struct {
	// Human-readable name of the eval, shown in the Dash0 UI.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Optional free-text note on what this eval judges and why. The judge does not see it.
	// +kubebuilder:validation:Optional
	Description *string `json:"description,omitempty"`
}

// Dash0GenerativeAiEvaluationMatch selects the spans an eval judges.
type Dash0GenerativeAiEvaluationMatch struct {
	// All filters must match. The Dash0 API requires an is_set filter for the key gen_ai.operation.name without a
	// value.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Filters []Dash0GenerativeAiEvaluationAttributeFilter `json:"filters"`
}

// Dash0GenerativeAiEvaluationAttributeFilter is a single filter against an attribute key.
type Dash0GenerativeAiEvaluationAttributeFilter struct {
	// The attribute key to match against.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`

	// The match operator.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=is;is_not;is_set;is_not_set;is_one_of;is_not_one_of;gt;lt;gte;lte;matches;does_not_match;contains;does_not_contain;starts_with;does_not_start_with;ends_with;does_not_end_with;is_any
	Operator string `json:"operator"`

	// Single value for operators that accept one value (is, is_not, contains, matches, ...).
	// +kubebuilder:validation:Optional
	Value *string `json:"value,omitempty"`

	// List of values for the is_one_of / is_not_one_of operators.
	// +kubebuilder:validation:Optional
	Values []string `json:"values,omitempty"`
}

// Dash0GenerativeAiEvaluationSampling controls which share of the matching traces is judged.
type Dash0GenerativeAiEvaluationSampling struct {
	// The share of matching traces to judge, between 0 and 1. The decision is made deterministically from the trace ID.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	Rate float64 `json:"rate"`
}

// Dash0GenerativeAiEvaluationJudge configures the model and the prompts that score a span.
type Dash0GenerativeAiEvaluationJudge struct {
	// The model class of the judge: light (fast, low-cost) or standard (balanced quality and cost).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=light;standard
	Model string `json:"model"`

	// The system prompt of the judge.
	// +kubebuilder:validation:Required
	SystemPrompt string `json:"systemPrompt"`

	// The prompt sent to the judge for each evaluated span. An attribute key in double curly braces is replaced with
	// the value of that span or resource attribute. A span is skipped when a referenced attribute is missing or empty,
	// unless allowPartialPrompt is set.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	UserPrompt string `json:"userPrompt"`

	// Judge spans that carry only some of the attributes the user prompt references. Defaults to false.
	// +kubebuilder:validation:Optional
	AllowPartialPrompt *bool `json:"allowPartialPrompt,omitempty"`

	// The kind of result the judge produces. The kind cannot change after the eval has been created.
	// +kubebuilder:validation:Required
	Output Dash0GenerativeAiEvaluationJudgeOutput `json:"output"`
}

// Dash0GenerativeAiEvaluationJudgeOutput defines the kind of result the judge produces.
//
// +kubebuilder:validation:XValidation:rule="self.kind == oldSelf.kind",message="kind cannot change after the eval has been created; delete the resource and create it again"
// +kubebuilder:validation:XValidation:rule="self.kind != 'boolean' || !has(self.spec)",message="spec must not be set when kind is 'boolean'"
// +kubebuilder:validation:XValidation:rule="self.kind != 'numeric' || !has(self.spec) || !has(self.spec.options)",message="spec.options must not be set when kind is 'numeric'"
// +kubebuilder:validation:XValidation:rule="self.kind != 'categorical' || (has(self.spec) && has(self.spec.options) && !has(self.spec.threshold))",message="spec.options must be set and spec.threshold must not be set when kind is 'categorical'"
type Dash0GenerativeAiEvaluationJudgeOutput struct {
	// numeric: a score between 0 and 1, 1 is the best result. boolean: true means the eval passed. categorical: one of
	// the labels in spec.options.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=numeric;boolean;categorical
	Kind string `json:"kind"`

	// The configuration of the output kind. Not allowed for boolean, required for categorical.
	// +kubebuilder:validation:Optional
	Spec *Dash0GenerativeAiEvaluationJudgeOutputSpec `json:"spec,omitempty"`
}

// Dash0GenerativeAiEvaluationJudgeOutputSpec holds the configuration of the numeric and the categorical output kinds.
type Dash0GenerativeAiEvaluationJudgeOutputSpec struct {
	// For numeric outputs: the threshold of the score, between 0 and 1. Leave it out to declare no threshold.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	Threshold *float64 `json:"threshold,omitempty"`

	// For categorical outputs: the labels the judge can choose from.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=label
	Options []Dash0GenerativeAiEvaluationJudgeOutputCategoricalOption `json:"options,omitempty"`
}

// Dash0GenerativeAiEvaluationJudgeOutputCategoricalOption is one label of a categorical output.
type Dash0GenerativeAiEvaluationJudgeOutputCategoricalOption struct {
	// The label. Labels must be unique within an eval.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Label string `json:"label"`

	// Whether the label is a desirable (positive), neutral or undesirable (negative) outcome. Without an intent, the
	// label has no polarity.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=positive;neutral;negative
	Intent *string `json:"intent,omitempty"`
}

// Dash0GenerativeAiEvaluationStatus defines the observed state of a Dash0GenerativeAiEvaluation resource.
type Dash0GenerativeAiEvaluationStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus                       `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                                             `json:"synchronizedAt"`
	ValidationIssues       []string                                                                `json:"validationIssues,omitempty"`
	SynchronizationResults []Dash0GenerativeAiEvaluationSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults"`
}

// Dash0GenerativeAiEvaluationSynchronizationResultPerEndpointAndDataset captures the result of a single sync attempt.
type Dash0GenerativeAiEvaluationSynchronizationResultPerEndpointAndDataset struct {
	SynchronizationStatus dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	Dash0ApiEndpoint      string                                            `json:"dash0ApiEndpoint,omitempty"`
	Dash0Dataset          string                                            `json:"dash0Dataset,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Id string `json:"dash0Id,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin          string `json:"dash0Origin,omitempty"`
	SynchronizationError string `json:"synchronizationError,omitempty"`
	// HttpStatusCode is the HTTP status code that the Dash0 API returned for the failed synchronization attempt, if the
	// failure was caused by an unexpected HTTP response. It is 0 (absent) for successful synchronizations and for
	// transport-level errors (network errors, timeouts) where no HTTP response was received. It is used to decide
	// whether a failed synchronization should be retried.
	// +kubebuilder:validation:Optional
	HttpStatusCode int `json:"httpStatusCode,omitempty"`
}

// Dash0GenerativeAiEvaluationList contains a list of Dash0GenerativeAiEvaluation resources.
//
// +kubebuilder:object:root=true
type Dash0GenerativeAiEvaluationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0GenerativeAiEvaluation `json:"items"`
}
