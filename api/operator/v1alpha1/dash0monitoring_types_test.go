// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	TestNamespaceName      = "test-namespace"
	MonitoringResourceName = "dash0-monitoring-test-resource"
	EndpointDash0Test      = "endpoint.dash0.com:4317"
	AuthorizationTokenTest = "authorization-token-test"
)


var _ = Describe("v1alpha1 Dash0 monitoring CRD", func() {

	type convertToTestCase struct {
		srcSpec                       *Dash0MonitoringSpec
		srcStatus                     *Dash0MonitoringStatus
		propagatorsAnnotation         *string
		previousPropagatorsAnnotation *string
		expectedDstSpec               dash0v1beta1.Dash0MonitoringSpec
		expectedDstStatus             dash0v1beta1.Dash0MonitoringStatus
	}

	Describe("converting to and from hub version", func() {

		DescribeTable("should convert v1alpha1 to hub version", func(testConfig convertToTestCase) {
			objectMeta := testObjectMeta()
			if testConfig.propagatorsAnnotation != nil {
				objectMeta.Annotations[annotationNameSpecInstrumentWorkloadsTraceContextPropagators] =
					*testConfig.propagatorsAnnotation
			}
			if testConfig.previousPropagatorsAnnotation != nil {
				objectMeta.Annotations[annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators] =
					*testConfig.previousPropagatorsAnnotation
			}
			src := &Dash0Monitoring{
				TypeMeta: metav1.TypeMeta{
					Kind: "Dash0Monitoring",
				},
				ObjectMeta: objectMeta,
			}
			if testConfig.srcSpec != nil {
				src.Spec = *testConfig.srcSpec
			}
			if testConfig.srcStatus != nil {
				src.Status = *testConfig.srcStatus
			}
			dst := dash0v1beta1.Dash0Monitoring{}
			hub := conversion.Hub(&dst)

			Expect(src.ConvertTo(hub)).To(Succeed())

			Expect(dst.Namespace).To(Equal(TestNamespaceName))
			Expect(dst.Name).To(Equal(MonitoringResourceName))
			Expect(dst.Labels).To(HaveLen(1))
			Expect(dst.Labels["test-label"]).To(Equal("test-value"))
			Expect(dst.Annotations).To(HaveLen(1))
			Expect(dst.Annotations["test-annotation"]).To(Equal("test-value"))
			Expect(dst.Spec).To(Equal(testConfig.expectedDstSpec))
			Expect(dst.Status).To(Equal(testConfig.expectedDstStatus))
		},
			Entry("no spec, no status", convertToTestCase{
				srcSpec:           nil,
				expectedDstSpec:   dash0v1beta1.Dash0MonitoringSpec{},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{},
			}),
			Entry("empty spec, empty status", convertToTestCase{
				srcSpec:           &Dash0MonitoringSpec{},
				srcStatus:         &Dash0MonitoringStatus{},
				expectedDstSpec:   dash0v1beta1.Dash0MonitoringSpec{},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{},
			}),
			Entry("full spec, full status", convertToTestCase{
				srcSpec: &Dash0MonitoringSpec{
					Export:              testExport(),
					InstrumentWorkloads: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
					LogCollection: dash0common.LogCollection{
						Enabled: ptr.To(false),
					},
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
					Filter:                      testFilter(),
					Transform:                   testTransform(),
					NormalizedTransformSpec:     testNormalizedTransform(),
					SynchronizePersesDashboards: ptr.To(false),
					SynchronizePrometheusRules:  ptr.To(false),
				},
				srcStatus: &Dash0MonitoringStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(dash0common.ConditionTypeAvailable),
							Status: metav1.ConditionTrue,
						},
					},
					PreviousInstrumentWorkloads: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
				},
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					Export: testExport(),
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
					},
					LogCollection: dash0common.LogCollection{
						Enabled: ptr.To(false),
					},
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
					Filter:                      testFilter(),
					Transform:                   testTransform(),
					NormalizedTransformSpec:     testNormalizedTransform(),
					SynchronizePersesDashboards: ptr.To(false),
					SynchronizePrometheusRules:  ptr.To(false),
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(dash0common.ConditionTypeAvailable),
							Status: metav1.ConditionTrue,
						},
					},
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
					},
				},
			}),
			Entry("convert legacy prometheus scraping setting (disabled only via legacy setting)", convertToTestCase{
				srcSpec: &Dash0MonitoringSpec{
					PrometheusScrapingEnabled: ptr.To(false),
				},
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
				},
			}),
			Entry("convert legacy prometheus scraping setting (disabled via both settings)", convertToTestCase{
				srcSpec: &Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
					PrometheusScrapingEnabled: ptr.To(false),
				},
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
				},
			}),
			Entry("convert legacy prometheus scraping setting (legacy false, new setting true)", convertToTestCase{
				srcSpec: &Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(true),
					},
					PrometheusScrapingEnabled: ptr.To(false),
				},
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
				},
			}),
			Entry("convert legacy prometheus scraping setting (legacy true, new setting false)", convertToTestCase{
				srcSpec: &Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
					PrometheusScrapingEnabled: ptr.To(true),
				},
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
				},
			}),
			Entry("with propagators annotation", convertToTestCase{
				srcSpec:               &Dash0MonitoringSpec{},
				propagatorsAnnotation: ptr.To("traceparent,aws"),
				srcStatus:             &Dash0MonitoringStatus{},
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("traceparent,aws"),
						},
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{},
			}),
			Entry("with previous propagators annotation", convertToTestCase{
				srcSpec:                       &Dash0MonitoringSpec{},
				previousPropagatorsAnnotation: ptr.To("traceparent,aws"),
				srcStatus:                     &Dash0MonitoringStatus{},
				expectedDstSpec:               dash0v1beta1.Dash0MonitoringSpec{},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("traceparent,aws"),
						},
					},
				},
			}),
			Entry("with both propagators annotations", convertToTestCase{
				srcSpec:                       &Dash0MonitoringSpec{},
				propagatorsAnnotation:         ptr.To("traceparent,aws"),
				previousPropagatorsAnnotation: ptr.To("aws"),
				srcStatus:                     &Dash0MonitoringStatus{},
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("traceparent,aws"),
						},
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("aws"),
						},
					},
				},
			}),
		)

		type convertFromTestCase struct {
			srcSpec                             *dash0v1beta1.Dash0MonitoringSpec
			srcStatus                           *dash0v1beta1.Dash0MonitoringStatus
			expectedDstSpec                     Dash0MonitoringSpec
			expectedDstStatus                   Dash0MonitoringStatus
			expectPropagatorsAnnotation         *string
			expectPreviousPropagatorsAnnotation *string
		}

		DescribeTable("should convert from hub version to v1alpha1", func(testConfig convertFromTestCase) {
			src := dash0v1beta1.Dash0Monitoring{
				TypeMeta: metav1.TypeMeta{
					Kind: "Dash0Monitoring",
				},
				ObjectMeta: testObjectMeta(),
			}
			if testConfig.srcSpec != nil {
				src.Spec = *testConfig.srcSpec
			}
			if testConfig.srcStatus != nil {
				src.Status = *testConfig.srcStatus
			}
			dst := &Dash0Monitoring{}
			hub := conversion.Hub(&src)

			Expect(dst.ConvertFrom(hub)).To(Succeed())

			Expect(dst.Namespace).To(Equal(TestNamespaceName))
			Expect(dst.Name).To(Equal(MonitoringResourceName))
			Expect(dst.Labels).To(HaveLen(1))
			Expect(dst.Labels["test-label"]).To(Equal("test-value"))
			if testConfig.expectPropagatorsAnnotation != nil && testConfig.expectPreviousPropagatorsAnnotation != nil {
				Expect(dst.Annotations).To(HaveLen(3))
			} else if testConfig.expectPropagatorsAnnotation != nil || testConfig.expectPreviousPropagatorsAnnotation != nil {
				Expect(dst.Annotations).To(HaveLen(2))
			} else {
				Expect(dst.Annotations).To(HaveLen(1))
			}
			Expect(dst.Annotations["test-annotation"]).To(Equal("test-value"))

			if testConfig.expectPropagatorsAnnotation != nil {
				Expect(dst.Annotations[annotationNameSpecInstrumentWorkloadsTraceContextPropagators]).To(Equal(*testConfig.expectPropagatorsAnnotation))
			} else if testConfig.expectPreviousPropagatorsAnnotation != nil {
				Expect(dst.Annotations[annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators]).To(Equal(*testConfig.expectPreviousPropagatorsAnnotation))
			}

			Expect(dst.Spec).To(Equal(testConfig.expectedDstSpec))
			Expect(dst.Status).To(Equal(testConfig.expectedDstStatus))
		},
			Entry("no spec, no status", convertFromTestCase{
				srcSpec:           nil,
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("empty spec, empty status", convertFromTestCase{
				srcSpec:           &dash0v1beta1.Dash0MonitoringSpec{},
				srcStatus:         &dash0v1beta1.Dash0MonitoringStatus{},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("full spec, full status", convertFromTestCase{
				srcSpec: &dash0v1beta1.Dash0MonitoringSpec{
					Export: testExport(),
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
					},
					LogCollection: dash0common.LogCollection{
						Enabled: ptr.To(false),
					},
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
					Filter:                      testFilter(),
					Transform:                   testTransform(),
					NormalizedTransformSpec:     testNormalizedTransform(),
					SynchronizePersesDashboards: ptr.To(false),
					SynchronizePrometheusRules:  ptr.To(false),
				},
				srcStatus: &dash0v1beta1.Dash0MonitoringStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(dash0common.ConditionTypeAvailable),
							Status: metav1.ConditionTrue,
						},
					},
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
					},
				},
				expectedDstSpec: Dash0MonitoringSpec{
					Export:              testExport(),
					InstrumentWorkloads: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
					LogCollection: dash0common.LogCollection{
						Enabled: ptr.To(false),
					},
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
					Filter:                      testFilter(),
					Transform:                   testTransform(),
					NormalizedTransformSpec:     testNormalizedTransform(),
					SynchronizePersesDashboards: ptr.To(false),
					SynchronizePrometheusRules:  ptr.To(false),
				},
				expectedDstStatus: Dash0MonitoringStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(dash0common.ConditionTypeAvailable),
							Status: metav1.ConditionTrue,
						},
					},
					PreviousInstrumentWorkloads: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
				},
			}),
			Entry("with spec.instrumentWorkloads.traceContext.propagators", convertFromTestCase{
				srcSpec: &dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("traceparent,aws"),
						},
					},
				},
				srcStatus:                   &dash0v1beta1.Dash0MonitoringStatus{},
				expectedDstSpec:             Dash0MonitoringSpec{},
				expectedDstStatus:           Dash0MonitoringStatus{},
				expectPropagatorsAnnotation: ptr.To("traceparent,aws"),
			}),
			Entry("with status.previousInstrumentWorkloads.traceContext.propagators", convertFromTestCase{
				srcSpec: &dash0v1beta1.Dash0MonitoringSpec{},
				srcStatus: &dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("aws"),
						},
					},
				},
				expectedDstSpec:                     Dash0MonitoringSpec{},
				expectedDstStatus:                   Dash0MonitoringStatus{},
				expectPreviousPropagatorsAnnotation: ptr.To("aws"),
			}),
			Entry("with propagators in both attributes", convertFromTestCase{
				srcSpec: &dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("traceparent,aws"),
						},
					},
				},
				srcStatus: &dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("aws"),
						},
					},
				},
				expectedDstSpec:                     Dash0MonitoringSpec{},
				expectedDstStatus:                   Dash0MonitoringStatus{},
				expectPropagatorsAnnotation:         ptr.To("traceparent,aws"),
				expectPreviousPropagatorsAnnotation: ptr.To("aws"),
			}),
		)
	})
})

func testObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: TestNamespaceName,
		Name:      MonitoringResourceName,
		Labels: map[string]string{
			"test-label": "test-value",
		},
		Annotations: map[string]string{
			"test-annotation": "test-value",
		},
	}
}

func testExport() *dash0common.Export {
	return &dash0common.Export{
		Dash0: &dash0common.Dash0Configuration{
			Endpoint: EndpointDash0Test,
			Authorization: dash0common.Authorization{
				Token: ptr.To(AuthorizationTokenTest),
			},
		},
	}
}

func testFilter() *dash0common.Filter {
	return &dash0common.Filter{
		ErrorMode: dash0common.FilterTransformErrorModePropagate,
		Traces: &dash0common.TraceFilter{
			SpanFilter: []string{
				"span-filter-1",
				"span-filter-2",
			},
			SpanEventFilter: []string{
				"span-event-filter-1",
				"span-event-filter-2",
			},
		},
		Metrics: &dash0common.MetricFilter{
			MetricFilter: []string{
				"metric-filter-1",
				"metric-filter-2",
			},
			DataPointFilter: []string{
				"data-point-filter-1",
				"data-point-filter-2",
			},
		},
		Logs: &dash0common.LogFilter{
			LogRecordFilter: []string{
				"log-record-filter-1",
				"log-record-filter-2",
			},
		},
	}
}

func testTransform() *dash0common.Transform {
	return &dash0common.Transform{
		ErrorMode: ptr.To(dash0common.FilterTransformErrorModePropagate),
		Traces: []json.RawMessage{
			[]byte(`"trace-transform-1"`),
			[]byte(`"trace-transform-2`),
		},
		Metrics: []json.RawMessage{
			[]byte(`"metric-transform-1"`),
			[]byte(`"metric-transform-2`),
		},
		Logs: []json.RawMessage{
			[]byte(`"log-transform-1"`),
			[]byte(`"log-transform-2`),
		},
	}
}

func testNormalizedTransform() *dash0common.NormalizedTransformSpec {
	return &dash0common.NormalizedTransformSpec{
		ErrorMode: ptr.To(dash0common.FilterTransformErrorModePropagate),
		Traces: []dash0common.NormalizedTransformGroup{
			{
				Context:   ptr.To("trace-transform-context"),
				ErrorMode: ptr.To(dash0common.FilterTransformErrorModePropagate),
				Conditions: []string{
					"trace-transform-condition-1",
					"trace-transform-condition-2",
				},
				Statements: []string{
					"trace-transform-statements-1",
					"trace-transform-statements-2",
				},
			},
		},
		Metrics: []dash0common.NormalizedTransformGroup{
			{
				Context:   ptr.To("metric-transform-context"),
				ErrorMode: ptr.To(dash0common.FilterTransformErrorModePropagate),
				Conditions: []string{
					"metric-transform-condition-1",
					"metric-transform-condition-2",
				},
				Statements: []string{
					"metric-transform-statements-1",
					"metric-transform-statements-2",
				},
			},
		},
		Logs: []dash0common.NormalizedTransformGroup{
			{
				Context:   ptr.To("log-transform-context"),
				ErrorMode: ptr.To(dash0common.FilterTransformErrorModePropagate),
				Conditions: []string{
					"log-transform-condition-1",
					"log-transform-condition-2",
				},
				Statements: []string{
					"log-transform-statements-1",
					"log-transform-statements-2",
				},
			},
		},
	}
}
