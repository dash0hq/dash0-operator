// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("Dash0TelemetryTransformationRule schema", func() {
	ctx := context.Background()

	BeforeEach(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	createRule := func(rule *dash0v1alpha1.Dash0TelemetryTransformationRule) {
		Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
		})
	}

	expectRejected := func(object client.Object, field string) {
		err := k8sClient.Create(ctx, object, client.DryRunAll)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected a validation error, got %v", err)
		Expect(err.Error()).To(ContainSubstring(field))
	}

	It("round-trips every spec field and lists the resource only in its namespace", func() {
		rule := telemetryTransformationRuleSchemaFixture()
		rule.Spec.Display = &dash0v1alpha1.Dash0TelemetryTransformationRuleDisplay{Name: "Checkout logs"}
		rule.Spec.Priority = ptr.To(0)
		rule.Spec.Conditions = []string{`resource.attributes["service.name"] == "checkout"`, `severity_number >= 17`}
		rule.Spec.Transformations = []dash0v1alpha1.Dash0TelemetryTransformationRuleTransformation{
			{Name: "Set attributes", Statements: []string{`set(attributes["first"], true)`, `set(body, "checkout")`}},
			{Name: "Set attributes", Statements: []string{`set(attributes["second"], attributes["first"])`}},
		}
		rule.Spec.Examples = []dash0v1alpha1.Dash0TelemetryTransformationRuleExample{
			{Name: "Empty object", Otlp: apiextensionsv1.JSON{Raw: []byte(`{}`)}},
		}
		expected := rule.DeepCopy().Spec
		createRule(rule)

		stored := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rule), stored)).To(Succeed())
		Expect(stored.Spec).To(Equal(expected))

		list := &dash0v1alpha1.Dash0TelemetryTransformationRuleList{}
		Expect(k8sClient.List(ctx, list, client.InNamespace(rule.Namespace))).To(Succeed())
		Expect(list.Items).To(ContainElement(HaveField("Name", rule.Name)))
		Expect(k8sClient.List(ctx, list, client.InNamespace("default"))).To(Succeed())
		Expect(list.Items).NotTo(ContainElement(HaveField("Name", rule.Name)))
	})

	It("accepts exactly the supported signal and context combinations", func() {
		validContexts := map[string][]string{
			"traces":   {"resource", "scope", "span", "spanevent"},
			"metrics":  {"resource", "scope", "metric", "datapoint"},
			"logs":     {"resource", "scope", "log"},
			"profiles": {"resource", "scope", "profile"},
		}
		for _, signal := range []string{"traces", "metrics", "logs", "profiles"} {
			for _, evaluationContext := range []string{
				"resource", "scope", "span", "spanevent", "metric", "datapoint", "log", "profile",
			} {
				By(fmt.Sprintf("validating signal %s with context %s", signal, evaluationContext))
				rule := telemetryTransformationRuleSchemaFixture()
				rule.Spec.Signal = dash0v1alpha1.Dash0TelemetryTransformationRuleSignalType(signal)
				rule.Spec.Context = dash0v1alpha1.Dash0TelemetryTransformationRuleContext(evaluationContext)
				if slices.Contains(validContexts[signal], evaluationContext) {
					Expect(k8sClient.Create(ctx, rule, client.DryRunAll)).To(Succeed())
				} else {
					expectRejected(rule, "context")
				}
			}
		}
	})

	It("rejects unknown and empty signal and context values", func() {
		for _, signal := range []string{"spans", "unknown", ""} {
			rule := telemetryTransformationRuleSchemaFixture()
			rule.Spec.Signal = dash0v1alpha1.Dash0TelemetryTransformationRuleSignalType(signal)
			expectRejected(rule, "signal")
		}
		for _, evaluationContext := range []string{"span_event", "unknown", ""} {
			rule := telemetryTransformationRuleSchemaFixture()
			rule.Spec.Context = dash0v1alpha1.Dash0TelemetryTransformationRuleContext(evaluationContext)
			expectRejected(rule, "context")
		}
	})

	It("accepts every supported priority without replacing an explicit zero", func() {
		for _, priority := range []int{0, 100, 200, 300, 400} {
			rule := telemetryTransformationRuleSchemaFixture()
			rule.Spec.Priority = ptr.To(priority)
			Expect(k8sClient.Create(ctx, rule, client.DryRunAll)).To(Succeed())
			Expect(rule.Spec.Priority).To(HaveValue(Equal(priority)))
		}
	})

	It("defaults an omitted priority to 200", func() {
		rule := telemetryTransformationRuleSchemaFixture()
		Expect(rule.Spec.Priority).To(BeNil())
		createRule(rule)

		stored := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rule), stored)).To(Succeed())
		Expect(stored.Spec.Priority).To(HaveValue(Equal(200)))
	})

	It("rejects priorities outside the supported steps and bounds", func() {
		for _, priority := range []int{-100, 50, 150, 450, 500} {
			rule := telemetryTransformationRuleSchemaFixture()
			rule.Spec.Priority = ptr.To(priority)
			expectRejected(rule, "spec.priority")
		}
	})

	It("requires the spec and every required spec field", func() {
		for _, path := range [][]string{
			{"spec"},
			{"spec", "enabled"},
			{"spec", "signal"},
			{"spec", "context"},
			{"spec", "transformations"},
		} {
			object := telemetryTransformationRuleSchemaObject()
			unstructured.RemoveNestedField(object.Object, path...)
			expectRejected(object, path[len(path)-1])
		}
	})

	It("requires display names, transformation names and statements, and example names and OTLP", func() {
		for _, testCase := range []struct {
			field        string
			value        any
			missingField string
		}{
			{field: "display", value: map[string]any{}, missingField: "spec.display.name"},
			{
				field:        "transformations",
				value:        []any{map[string]any{"statements": []any{}}},
				missingField: "spec.transformations[0].name",
			},
			{
				field:        "transformations",
				value:        []any{map[string]any{"name": "Named transformation"}},
				missingField: "spec.transformations[0].statements",
			},
			{
				field:        "examples",
				value:        []any{map[string]any{"otlp": map[string]any{}}},
				missingField: "spec.examples[0].name",
			},
			{
				field:        "examples",
				value:        []any{map[string]any{"name": "Named example"}},
				missingField: "spec.examples[0].otlp",
			},
		} {
			object := telemetryTransformationRuleSchemaObject()
			object.Object["spec"].(map[string]any)[testCase.field] = testCase.value
			expectRejected(object, testCase.missingField)
		}
	})

	It("rejects empty and null transformation lists", func() {
		for _, transformations := range []any{[]any{}, nil} {
			object := telemetryTransformationRuleSchemaObject()
			object.Object["spec"].(map[string]any)["transformations"] = transformations
			expectRejected(object, "spec.transformations")
		}
	})

	It("allows empty names and statement lists and leaves OTTL compilation to the backend", func() {
		rule := telemetryTransformationRuleSchemaFixture()
		rule.Spec.Display = &dash0v1alpha1.Dash0TelemetryTransformationRuleDisplay{Name: ""}
		rule.Spec.Conditions = []string{"not a valid OTTL condition"}
		rule.Spec.Transformations = []dash0v1alpha1.Dash0TelemetryTransformationRuleTransformation{
			{Name: "", Statements: []string{}},
			{Name: "", Statements: []string{"not a valid OTTL statement"}},
		}
		rule.Spec.Examples = []dash0v1alpha1.Dash0TelemetryTransformationRuleExample{
			{Name: "", Otlp: apiextensionsv1.JSON{Raw: []byte(`{}`)}},
		}
		Expect(k8sClient.Create(ctx, rule, client.DryRunAll)).To(Succeed())
		object := telemetryTransformationRuleSchemaObject()
		object.Object["spec"].(map[string]any)["conditions"] = []any{}
		object.Object["spec"].(map[string]any)["examples"] = []any{}
		Expect(k8sClient.Create(ctx, object, client.DryRunAll)).To(Succeed())
	})

	It("allows at most ten examples", func() {
		for _, count := range []int{0, 10, 11} {
			rule := telemetryTransformationRuleSchemaFixture()
			rule.Spec.Examples = make([]dash0v1alpha1.Dash0TelemetryTransformationRuleExample, count)
			for i := range rule.Spec.Examples {
				rule.Spec.Examples[i] = dash0v1alpha1.Dash0TelemetryTransformationRuleExample{
					Name: fmt.Sprintf("Example %d", i),
					Otlp: apiextensionsv1.JSON{Raw: []byte(`{}`)},
				}
			}
			if count <= 10 {
				Expect(k8sClient.Create(ctx, rule, client.DryRunAll)).To(Succeed())
			} else {
				expectRejected(rule, "spec.examples")
			}
		}
	})

	It("requires OTLP examples to be objects", func() {
		for _, otlp := range []any{"string", int64(42), true, []any{}, nil} {
			object := telemetryTransformationRuleSchemaObject()
			object.Object["spec"].(map[string]any)["examples"] = []any{
				map[string]any{"name": "Not an object", "otlp": otlp},
			}
			expectRejected(object, "spec.examples[0].otlp")
		}
	})

	It("preserves arbitrary nested OTLP fields and exact integers through Kubernetes and JSON round trips", func() {
		payload := []byte(`{
			"resourceLogs": [{
				"resource": {
					"attributes": [{"key": "service.name", "value": {"stringValue": "checkout"}}],
					"futureResource": {"enabled": true}
				},
				"scopeLogs": [{
					"scope": {"name": "schema-test", "futureScope": {"versions": [1, 2]}},
					"logRecords": [{
						"timeUnixNano": "1793793600000000000",
						"body": {"kvlistValue": {"values": [{"key": "answer", "value": {"intValue": "42"}}]}},
						"futureLog": {
							"numbers": [9007199254740993, 9223372036854775807],
							"nested": [{"nil": null, "empty": {}, "values": [true, "text"]}]
						}
					}]
				}]
			}],
			"futureExport": {"nullable": null, "array": [{"value": -9007199254740993}]}
		}`)
		var expected any
		expectedDecoder := json.NewDecoder(bytes.NewReader(payload))
		expectedDecoder.UseNumber()
		Expect(expectedDecoder.Decode(&expected)).To(Succeed())

		rule := telemetryTransformationRuleSchemaFixture()
		rule.Spec.Examples = []dash0v1alpha1.Dash0TelemetryTransformationRuleExample{
			{Name: "Future OTLP fields", Otlp: apiextensionsv1.JSON{Raw: payload}},
		}
		createRule(rule)

		stored := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rule), stored)).To(Succeed())
		serialized, err := json.Marshal(stored)
		Expect(err).NotTo(HaveOccurred())
		roundTripped := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
		Expect(json.Unmarshal(serialized, roundTripped)).To(Succeed())
		Expect(roundTripped.Spec.Examples).To(HaveLen(1))
		Expect(roundTripped.Spec.Examples[0].Name).To(Equal("Future OTLP fields"))

		var actual any
		actualDecoder := json.NewDecoder(bytes.NewReader(roundTripped.Spec.Examples[0].Otlp.Raw))
		actualDecoder.UseNumber()
		Expect(actualDecoder.Decode(&actual)).To(Succeed())
		Expect(actual).To(Equal(expected))
	})

	It("round-trips validation issues and synchronization results through the status subresource", func() {
		rule := telemetryTransformationRuleSchemaFixture()
		createRule(rule)
		expected := dash0v1alpha1.Dash0TelemetryTransformationRuleStatus{
			SynchronizationStatus: dash0common.Dash0ApiResourceSynchronizationStatusPartiallySuccessful,
			SynchronizedAt:        metav1.NewTime(time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)),
			ValidationIssues:      []string{"invalid OTTL statement"},
			SynchronizationResults: []dash0v1alpha1.Dash0TelemetryTransformationRuleSynchronizationResultPerEndpointAndDataset{
				{
					SynchronizationStatus: dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
					Dash0ApiEndpoint:      "https://api.eu-central-1.aws.dash0.com",
					Dash0Dataset:          "default",
					Dash0Id:               "first-rule-id",
					Dash0Origin:           "first-rule-origin",
				},
				{
					SynchronizationStatus: dash0common.Dash0ApiResourceSynchronizationStatusFailed,
					Dash0ApiEndpoint:      "https://api.us-west-2.aws.dash0.com",
					Dash0Dataset:          "production",
					Dash0Id:               "second-rule-id",
					Dash0Origin:           "second-rule-origin",
					SynchronizationError:  "invalid OTTL statement",
					HttpStatusCode:        400,
				},
			},
		}
		rule.Status = expected
		Expect(k8sClient.Status().Update(ctx, rule)).To(Succeed())

		stored := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rule), stored)).To(Succeed())
		Expect(stored.Status.SynchronizationStatus).To(Equal(expected.SynchronizationStatus))
		Expect(stored.Status.SynchronizedAt.Time).To(BeTemporally("==", expected.SynchronizedAt.Time))
		Expect(stored.Status.ValidationIssues).To(Equal(expected.ValidationIssues))
		Expect(stored.Status.SynchronizationResults).To(Equal(expected.SynchronizationResults))
	})
})

func telemetryTransformationRuleSchemaFixture() *dash0v1alpha1.Dash0TelemetryTransformationRule {
	return &dash0v1alpha1.Dash0TelemetryTransformationRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: dash0v1alpha1.GroupVersion.String(),
			Kind:       "Dash0TelemetryTransformationRule",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "telemetry-transformation-schema-",
			Namespace:    TestNamespaceName,
		},
		Spec: dash0v1alpha1.Dash0TelemetryTransformationRuleSpec{
			Enabled: false,
			Signal:  dash0v1alpha1.Dash0TelemetryTransformationRuleSignalTypeLogs,
			Context: dash0v1alpha1.Dash0TelemetryTransformationRuleContextLog,
			Transformations: []dash0v1alpha1.Dash0TelemetryTransformationRuleTransformation{
				{Name: "Set body", Statements: []string{`set(body, "transformed")`}},
			},
		},
	}
}

func telemetryTransformationRuleSchemaObject() *unstructured.Unstructured {
	serialized, err := json.Marshal(telemetryTransformationRuleSchemaFixture())
	Expect(err).NotTo(HaveOccurred())
	object := &unstructured.Unstructured{}
	Expect(json.Unmarshal(serialized, &object.Object)).To(Succeed())
	return object
}
