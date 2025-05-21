// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"encoding/json"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type disableLogCollectionTestCase struct {
	namespace              string
	monitoringResourceSpec string
	expectPatch            bool
}

type normalizeTransformSpecTestCase struct {
	monitoringResourceSpec string
	expected               *dash0v1alpha1.NormalizedTransformSpec
}

const (
	errorModeKey  = "error_mode"
	contextKey    = "context"
	conditionsKey = "conditions"
	statementsKey = "statements"
)

var _ = Describe("The mutation webhook for the monitoring resource", func() {

	Describe("when mutating the operator configuration resource", func() {
		DescribeTable("should disable log collection in the operator namespace", func(testCase disableLogCollectionTestCase) {
			var unmarshalledYaml map[string]interface{}
			Expect(yaml.Unmarshal([]byte(testCase.monitoringResourceSpec), &unmarshalledYaml)).To(Succeed())
			rawSpecJson, err := json.Marshal(unmarshalledYaml)
			Expect(err).ToNot(HaveOccurred())
			response := monitoringMutatingWebhookHandler.Handle(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "resource-name",
					Namespace: testCase.namespace,
					Object: runtime.RawExtension{
						Raw: rawSpecJson,
					},
				},
			})

			Expect(response.Allowed).To(BeTrue())

			var logCollectionPatch interface{}
			for _, patch := range response.Patches {
				if patch.Operation == "replace" && patch.Path == "/spec/logCollection/enabled" {
					logCollectionPatch = patch.Value
				}
			}

			if !testCase.expectPatch {
				Expect(logCollectionPatch).To(BeNil())
				return
			}

			// If we patch the logCollection.enabled field, we only ever set it to false.
			Expect(logCollectionPatch).ToNot(BeNil())
			Expect(logCollectionPatch).To(BeFalse())

		}, []TableEntry{
			Entry("with an empty spec in an arbitrary namespace", disableLogCollectionTestCase{
				namespace: "some-namespace",
				monitoringResourceSpec: `
spec: {}
`,
				expectPatch: false,
			}),
			Entry("with an empty spec in the operator namespace", disableLogCollectionTestCase{
				namespace: OperatorNamespace,
				monitoringResourceSpec: `
spec: {}
`,
				expectPatch: false,
			}),
			Entry("with log collection disabled in an arbitrary namespace", disableLogCollectionTestCase{
				namespace: "some-namespace",
				monitoringResourceSpec: `
spec: 
  logCollection:
    enabled: false
`,
				expectPatch: false,
			}),
			Entry("with log collection diabled in the operator namespace", disableLogCollectionTestCase{
				namespace: OperatorNamespace,
				monitoringResourceSpec: `
spec: 
  logCollection:
    enabled: false
`,
				expectPatch: false,
			}),
			Entry("with log collection enabled in an arbitrary namespace", disableLogCollectionTestCase{
				namespace: "some-namespace",
				monitoringResourceSpec: `
spec: 
  logCollection:
    enabled: true
`,
				expectPatch: false,
			}),
			Entry("with log collection enabled in the operator namespace", disableLogCollectionTestCase{
				namespace: OperatorNamespace,
				monitoringResourceSpec: `
spec: 
  logCollection:
    enabled: true
`,
				expectPatch: true,
			}),
		})

		DescribeTable("should normalize the transform spec", func(testCase normalizeTransformSpecTestCase) {
			var unmarshalledYaml map[string]interface{}
			Expect(yaml.Unmarshal([]byte(testCase.monitoringResourceSpec), &unmarshalledYaml)).To(Succeed())
			rawSpecJson, err := json.Marshal(unmarshalledYaml)
			Expect(err).ToNot(HaveOccurred())
			response := monitoringMutatingWebhookHandler.Handle(ctx, admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name:      "resource-name",
					Namespace: "some-namespace",
					Object: runtime.RawExtension{
						Raw: rawSpecJson,
					},
				},
			})

			Expect(response.Allowed).To(BeTrue())

			var normalizedTransformsPatch interface{}
			for _, patch := range response.Patches {
				if patch.Operation == "add" && patch.Path == "/spec/__dash0_internal__normalizedTransform" {
					normalizedTransformsPatch = patch.Value
				}
			}

			if testCase.expected == nil {
				Expect(normalizedTransformsPatch).To(BeNil())
				return
			}

			Expect(normalizedTransformsPatch).ToNot(BeNil())
			patchAsMap, ok := normalizedTransformsPatch.(map[string]interface{})
			Expect(ok).To(BeTrue())

			expected := *testCase.expected

			verifyNormalizedTransformGroupsForOneSignal(expected.Traces, patchAsMap, "trace_statements")
			verifyNormalizedTransformGroupsForOneSignal(expected.Metrics, patchAsMap, "metric_statements")
			verifyNormalizedTransformGroupsForOneSignal(expected.Logs, patchAsMap, "log_statements")

		}, []TableEntry{
			Entry("without a transform spec", normalizeTransformSpecTestCase{
				monitoringResourceSpec: `
spec: {}
`,
				expected: nil,
			}),

			Entry("an empty transform spec", normalizeTransformSpecTestCase{
				monitoringResourceSpec: `
spec:
  transform: {}
`,
				expected: &dash0v1alpha1.NormalizedTransformSpec{},
			}),

			Entry("a transform spec with flat string trace statements", normalizeTransformSpecTestCase{
				monitoringResourceSpec: `
spec:
  transform:
    trace_statements:
    - 'trace statement 1'
    - 'trace statement 2'
`,
				expected: &dash0v1alpha1.NormalizedTransformSpec{
					Traces: []dash0v1alpha1.NormalizedTransformGroup{
						{Statements: []string{"trace statement 1"}},
						{Statements: []string{"trace statement 2"}},
					},
				},
			}),

			Entry("a transform spec with flat string statements for all signal types", normalizeTransformSpecTestCase{
				monitoringResourceSpec: `
spec:
  transform:
    trace_statements:
      - 'trace statement 1'
      - 'trace statement 2'
    metric_statements:
    - 'metric statement 1'
    - 'metric statement 2'
    log_statements:
    - 'log statement 1'
    - 'log statement 2'
`,
				expected: &dash0v1alpha1.NormalizedTransformSpec{
					Traces: []dash0v1alpha1.NormalizedTransformGroup{
						{Statements: []string{"trace statement 1"}},
						{Statements: []string{"trace statement 2"}},
					},
					Metrics: []dash0v1alpha1.NormalizedTransformGroup{
						{Statements: []string{"metric statement 1"}},
						{Statements: []string{"metric statement 2"}},
					},
					Logs: []dash0v1alpha1.NormalizedTransformGroup{
						{Statements: []string{"log statement 1"}},
						{Statements: []string{"log statement 2"}},
					},
				},
			}),

			Entry("a transform spec with advanced style trace groups", normalizeTransformSpecTestCase{
				monitoringResourceSpec: `
spec:
  transform:
    trace_statements:
    - context: 'trace context 1'
      error_mode: silent
      conditions:
      - 'trace condition 1.1'
      - 'trace condition 1.2'
      statements:
      - 'trace statement 1.1'
      - 'trace statement 1.2'
    - context: 'trace context 2'
      error_mode: propagate
      conditions:
      - 'trace condition 2.1'
      - 'trace condition 2.2'
      statements:
      - 'trace statement 2.1'
      - 'trace statement 2.2'
`,
				expected: &dash0v1alpha1.NormalizedTransformSpec{
					Traces: []dash0v1alpha1.NormalizedTransformGroup{
						{
							Context:    ptr.To("trace context 1"),
							ErrorMode:  ptr.To(dash0v1alpha1.FilterTransformErrorModeSilent),
							Conditions: []string{"trace condition 1.1", "trace condition 1.2"},
							Statements: []string{"trace statement 1.1", "trace statement 1.2"},
						},
						{
							Context:    ptr.To("trace context 2"),
							ErrorMode:  ptr.To(dash0v1alpha1.FilterTransformErrorModePropagate),
							Conditions: []string{"trace condition 2.1", "trace condition 2.2"},
							Statements: []string{"trace statement 2.1", "trace statement 2.2"},
						},
					},
				},
			}),

			Entry("a transform spec with advanced style, but only statements", normalizeTransformSpecTestCase{
				monitoringResourceSpec: `
spec:
  transform:
    trace_statements:
      - statements:
        - 'trace statement 1.1'
        - 'trace statement 1.2'
`,
				expected: &dash0v1alpha1.NormalizedTransformSpec{
					Traces: []dash0v1alpha1.NormalizedTransformGroup{
						{
							Statements: []string{"trace statement 1.1", "trace statement 1.2"},
						},
					},
				},
			}),

			Entry("a transform spec with all signals and mixed styles", normalizeTransformSpecTestCase{
				monitoringResourceSpec: `
spec:
  transform:
    trace_statements:
    - conditions:
      - 'trace condition 1.1'
      - 'trace condition 1.2'
      statements:
      - 'trace statement 1.1'
      - 'trace statement 1.2'
    - 'trace statement 2'
    - 'trace statement 3'
    metric_statements:
    - 'metric statement 1'
    - conditions:
      - 'metric condition 2.1'
      - 'metric condition 2.2'
      statements:
      - 'metric statement 2.1'
      - 'metric statement 2.2'
    - 'metric statement 3'
    log_statements:
    - 'log statement 1'
    - 'log statement 2'
    - conditions:
      - 'log condition 3.1'
      - 'log condition 3.2'
      statements:
      - 'log statement 3.1'
      - 'log statement 3.2'
`,
				expected: &dash0v1alpha1.NormalizedTransformSpec{
					Traces: []dash0v1alpha1.NormalizedTransformGroup{
						{
							Conditions: []string{"trace condition 1.1", "trace condition 1.2"},
							Statements: []string{"trace statement 1.1", "trace statement 1.2"},
						},
						{Statements: []string{"trace statement 2"}},
						{Statements: []string{"trace statement 3"}},
					},
					Metrics: []dash0v1alpha1.NormalizedTransformGroup{
						{Statements: []string{"metric statement 1"}},
						{
							Conditions: []string{"metric condition 2.1", "metric condition 2.2"},
							Statements: []string{"metric statement 2.1", "metric statement 2.2"},
						},
						{Statements: []string{"metric statement 3"}},
					},
					Logs: []dash0v1alpha1.NormalizedTransformGroup{
						{Statements: []string{"log statement 1"}},
						{Statements: []string{"log statement 2"}},
						{
							Conditions: []string{"log condition 3.1", "log condition 3.2"},
							Statements: []string{"log statement 3.1", "log statement 3.2"},
						},
					},
				},
			}),

			//
		})
	})

	Describe("using the actual webhook", func() {
		AfterEach(func() {
			Expect(
				k8sClient.DeleteAllOf(ctx, &dash0v1alpha1.Dash0Monitoring{}, client.InNamespace(TestNamespaceName)),
			).To(Succeed())
		})

		It("should normalize the transform spec", func() {
			_, err := CreateMonitoringResourceWithPotentialError(ctx, k8sClient, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: MonitoringResourceDefaultObjectMeta,
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Endpoint: EndpointDash0Test,
							Authorization: dash0v1alpha1.Authorization{
								Token: &AuthorizationTokenTest,
							},
						},
					},
					Transform: &dash0v1alpha1.Transform{
						Traces: []json.RawMessage{
							[]byte(`"truncate_all(span.attributes, 1024)"`),
						},
						Metrics: []json.RawMessage{
							[]byte(`{
                            "error_mode": "silent",
					        "conditions": [ "metric.type == METRIC_DATA_TYPE_SUM" ],
					        "statements": [ "truncate_all(datapoint.attributes, 1024)" ],
					        "context": "datapoint"
					    }`),
						},
						Logs: []json.RawMessage{
							[]byte(`"truncate_all(log.attributes, 1024)"`),
						},
					},
				},
			})
			Expect(err).ToNot(HaveOccurred())

			normalizedMonitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, Default)
			normalizedTransformSpec := normalizedMonitoringResource.Spec.NormalizedTransformSpec
			Expect(normalizedTransformSpec).ToNot(BeNil())
			Expect(normalizedTransformSpec.Traces).To(HaveLen(1))
			t1 := normalizedTransformSpec.Traces[0]
			Expect(t1.Context).To(BeNil())
			Expect(t1.ErrorMode).To(BeNil())
			Expect(t1.Conditions).To(BeNil())
			Expect(t1.Statements).To(HaveLen(1))
			Expect(t1.Statements[0]).To(Equal("truncate_all(span.attributes, 1024)"))
			Expect(normalizedTransformSpec.Metrics).To(HaveLen(1))
			m1 := normalizedTransformSpec.Metrics[0]
			Expect(m1.Context).ToNot(BeNil())
			Expect(*m1.Context).To(Equal("datapoint"))
			Expect(m1.ErrorMode).ToNot(BeNil())
			Expect(string(*m1.ErrorMode)).To(Equal("silent"))
			Expect(m1.Conditions).To(HaveLen(1))
			Expect(m1.Conditions[0]).To(Equal("metric.type == METRIC_DATA_TYPE_SUM"))
			Expect(m1.Statements).To(HaveLen(1))
			Expect(m1.Statements[0]).To(Equal("truncate_all(datapoint.attributes, 1024)"))
			Expect(normalizedTransformSpec.Logs).To(HaveLen(1))
			l1 := normalizedTransformSpec.Logs[0]
			Expect(l1.Context).To(BeNil())
			Expect(l1.ErrorMode).To(BeNil())
			Expect(l1.Conditions).To(BeNil())
			Expect(l1.Statements).To(HaveLen(1))
			Expect(l1.Statements[0]).To(Equal("truncate_all(log.attributes, 1024)"))

		})
	})
})

func verifyNormalizedTransformGroupsForOneSignal(
	expectedGroups []dash0v1alpha1.NormalizedTransformGroup,
	patchAsMap map[string]interface{},
	fieldName string,
) {
	if expectedGroups == nil {
		Expect(patchAsMap[fieldName]).To(BeNil())
	} else {
		traceStatements, ok := patchAsMap[fieldName].([]interface{})
		Expect(ok).To(BeTrue())
		Expect(traceStatements).To(HaveLen(len(expectedGroups)))
		for i, expectedTransformGroup := range expectedGroups {
			actualTransformGroup := traceStatements[i].(map[string]interface{})
			verifyString(expectedTransformGroup.Context, actualTransformGroup, contextKey)
			verifyErrorMode(expectedTransformGroup.ErrorMode, actualTransformGroup)
			verifyListOfStrings(expectedTransformGroup.Statements, actualTransformGroup, statementsKey)
			verifyListOfStrings(expectedTransformGroup.Conditions, actualTransformGroup, conditionsKey)
		}
	}
}

func verifyString(expectedString *string, actualTransformGroup map[string]interface{}, key string) {
	if expectedString == nil {
		Expect(actualTransformGroup[key]).To(BeNil())
	} else {
		actualValue := actualTransformGroup[key]
		Expect(actualValue).To(Equal(*expectedString))
	}
}

func verifyErrorMode(expectedErrorMode *dash0v1alpha1.FilterTransformErrorMode, actualTransformGroup map[string]interface{}) {
	if expectedErrorMode == nil {
		Expect(actualTransformGroup[errorModeKey]).To(BeNil())
	} else {
		actualValue := actualTransformGroup[errorModeKey]
		Expect(actualValue).To(Equal(string(*expectedErrorMode)))
	}
}

func verifyListOfStrings(expectedStrings []string, actualTransformGroup map[string]interface{}, key string) {
	if expectedStrings == nil {
		Expect(actualTransformGroup[key]).To(BeNil())
	} else {
		actualValues := actualTransformGroup[key].([]interface{})
		Expect(actualValues).To(HaveLen(len(expectedStrings)))
		for j, expectedStatement := range expectedStrings {
			Expect(actualValues[j]).To(Equal(expectedStatement))
		}
	}
}
