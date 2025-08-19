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
	"github.com/dash0hq/dash0-operator/internal/util"

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
		srcObjectMeta         metav1.ObjectMeta
		srcSpec               Dash0MonitoringSpec
		srcStatus             Dash0MonitoringStatus
		expectedDstObjectMeta metav1.ObjectMeta
		expectedDstSpec       dash0v1beta1.Dash0MonitoringSpec
		expectedDstStatus     dash0v1beta1.Dash0MonitoringStatus
	}

	Describe("converting to and from hub version", func() {

		DescribeTable("should convert v1alpha1 to hub version", func(testConfig convertToTestCase) {
			objectMeta := testConfig.srcObjectMeta
			src := &Dash0Monitoring{
				TypeMeta: metav1.TypeMeta{
					Kind: "Dash0Monitoring",
				},
				ObjectMeta: objectMeta,
				Spec:       testConfig.srcSpec,
				Status:     testConfig.srcStatus,
			}
			dst := dash0v1beta1.Dash0Monitoring{}
			hub := conversion.Hub(&dst)

			Expect(src.ConvertTo(hub)).To(Succeed())

			Expect(dst.ObjectMeta).To(Equal(testConfig.expectedDstObjectMeta))
			Expect(dst.Spec).To(Equal(testConfig.expectedDstSpec))
			Expect(dst.Status).To(Equal(testConfig.expectedDstStatus))
		},
			Entry("empty object meta, empty spec, empty status", convertToTestCase{
				srcObjectMeta:         metav1.ObjectMeta{},
				srcSpec:               Dash0MonitoringSpec{},
				srcStatus:             Dash0MonitoringStatus{},
				expectedDstObjectMeta: metav1.ObjectMeta{},
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("object meta has no annotations, empty spec, empty status", convertToTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
				},
				srcSpec:   Dash0MonitoringSpec{},
				srcStatus: Dash0MonitoringStatus{},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
				},
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("empty spec, empty status", convertToTestCase{
				srcObjectMeta:         testObjectMeta(),
				srcSpec:               Dash0MonitoringSpec{},
				srcStatus:             Dash0MonitoringStatus{},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("full spec, full status", convertToTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec: Dash0MonitoringSpec{
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
				srcStatus: Dash0MonitoringStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(dash0common.ConditionTypeAvailable),
							Status: metav1.ConditionTrue,
						},
					},
					PreviousInstrumentWorkloads: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
				},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					Export: testExport(),
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						Mode:          dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
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
						Mode:          dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("convert legacy prometheus scraping setting (disabled only via legacy setting)", convertToTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec: Dash0MonitoringSpec{
					PrometheusScrapingEnabled: ptr.To(false),
				},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("convert legacy prometheus scraping setting (disabled via both settings)", convertToTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec: Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
					PrometheusScrapingEnabled: ptr.To(false),
				},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("convert legacy prometheus scraping setting (legacy false, new setting true)", convertToTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec: Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(true),
					},
					PrometheusScrapingEnabled: ptr.To(false),
				},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("convert legacy prometheus scraping setting (legacy true, new setting false)", convertToTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec: Dash0MonitoringSpec{
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
					PrometheusScrapingEnabled: ptr.To(true),
				},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
					PrometheusScraping: dash0common.PrometheusScraping{
						Enabled: ptr.To(false),
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("with propagators annotation", convertToTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameSpecInstrumentWorkloadsTraceContextPropagators: "tracecontext,xray",
					},
				},
				srcSpec:               Dash0MonitoringSpec{},
				srcStatus:             Dash0MonitoringStatus{},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("tracecontext,xray"),
						},
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("with previous propagators annotation", convertToTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators: "tracecontext,xray",
					},
				},
				srcSpec:               Dash0MonitoringSpec{},
				srcStatus:             Dash0MonitoringStatus{},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("tracecontext,xray"),
						},
					},
				},
			}),
			Entry("with auto-instrumentation label selector annotation", convertToTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameSpecInstrumentWorkloadsLabelSelector: "some-label,dash0-auto-instrument=yes,stage in (dev,prod)",
					},
				},
				srcSpec:               Dash0MonitoringSpec{},
				srcStatus:             Dash0MonitoringStatus{},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "some-label,dash0-auto-instrument=yes,stage in (dev,prod)",
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
			}),
			Entry("with previous auto-instrumentation label selector annotation", convertToTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameStatusPreviousInstrumentWorkloadsLabelSelector: "dash0-auto-instrument=yes-please",
					},
				},
				srcSpec:               Dash0MonitoringSpec{},
				srcStatus:             Dash0MonitoringStatus{},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: util.DefaultAutoInstrumentationLabelSelector,
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "dash0-auto-instrument=yes-please",
					},
				},
			}),
			Entry("with multiple annotations for saving attributes not supported in v1alpha1", convertToTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameSpecInstrumentWorkloadsTraceContextPropagators:           "tracecontext,xray",
						annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators: "xray",
						annotationNameSpecInstrumentWorkloadsLabelSelector:                     "some-label,dash0-auto-instrument=yes,stage in (dev,prod)",
						annotationNameStatusPreviousInstrumentWorkloadsLabelSelector:           "dash0-auto-instrument=yes-please",
					},
				},
				srcSpec:               Dash0MonitoringSpec{},
				srcStatus:             Dash0MonitoringStatus{},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "some-label,dash0-auto-instrument=yes,stage in (dev,prod)",
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("tracecontext,xray"),
						},
					},
				},
				expectedDstStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "dash0-auto-instrument=yes-please",
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("xray"),
						},
					},
				},
			}),
		)

		type convertFromTestCase struct {
			srcObjectMeta         metav1.ObjectMeta
			srcSpec               dash0v1beta1.Dash0MonitoringSpec
			srcStatus             dash0v1beta1.Dash0MonitoringStatus
			expectedDstObjectMeta metav1.ObjectMeta
			expectedDstSpec       Dash0MonitoringSpec
			expectedDstStatus     Dash0MonitoringStatus
		}

		DescribeTable("should convert from hub version to v1alpha1", func(testConfig convertFromTestCase) {
			src := dash0v1beta1.Dash0Monitoring{
				TypeMeta: metav1.TypeMeta{
					Kind: "Dash0Monitoring",
				},
				ObjectMeta: testConfig.srcObjectMeta,
				Spec:       testConfig.srcSpec,
				Status:     testConfig.srcStatus,
			}
			dst := &Dash0Monitoring{}
			hub := conversion.Hub(&src)

			Expect(dst.ConvertFrom(hub)).To(Succeed())

			Expect(dst.ObjectMeta).To(Equal(testConfig.expectedDstObjectMeta))
			Expect(dst.Spec).To(Equal(testConfig.expectedDstSpec))
			Expect(dst.Status).To(Equal(testConfig.expectedDstStatus))
		},
			Entry("empty object meta, empty spec, empty status", convertFromTestCase{
				srcObjectMeta:         metav1.ObjectMeta{},
				srcSpec:               dash0v1beta1.Dash0MonitoringSpec{},
				srcStatus:             dash0v1beta1.Dash0MonitoringStatus{},
				expectedDstObjectMeta: metav1.ObjectMeta{},
				expectedDstSpec:       Dash0MonitoringSpec{},
				expectedDstStatus:     Dash0MonitoringStatus{},
			}),
			Entry("object meta has no annotations, empty spec, empty status", convertFromTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
				},
				srcSpec:   dash0v1beta1.Dash0MonitoringSpec{},
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
				},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("empty spec, empty status", convertFromTestCase{
				srcObjectMeta:         testObjectMeta(),
				srcSpec:               dash0v1beta1.Dash0MonitoringSpec{},
				srcStatus:             dash0v1beta1.Dash0MonitoringStatus{},
				expectedDstObjectMeta: testObjectMeta(),
				expectedDstSpec:       Dash0MonitoringSpec{},
				expectedDstStatus:     Dash0MonitoringStatus{},
			}),
			Entry("full spec, full status", convertFromTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec: dash0v1beta1.Dash0MonitoringSpec{
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
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{
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
				expectedDstObjectMeta: testObjectMeta(),
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
				srcObjectMeta: testObjectMeta(),
				srcSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("tracecontext,xray"),
						},
					},
				},
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameSpecInstrumentWorkloadsTraceContextPropagators: "tracecontext,xray",
					},
				},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("with status.previousInstrumentWorkloads.traceContext.propagators", convertFromTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec:       dash0v1beta1.Dash0MonitoringSpec{},
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("xray"),
						},
					},
				},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators: "xray",
					},
				},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("with spec.instrumentWorkloads.labelSelector", convertFromTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "some-label,dash0-auto-instrument=yes,stage in (dev,prod)",
					},
				},
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameSpecInstrumentWorkloadsLabelSelector: "some-label,dash0-auto-instrument=yes,stage in (dev,prod)",
					},
				},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("with status.previousInstrumentWorkloads.labelSelector", convertFromTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec:       dash0v1beta1.Dash0MonitoringSpec{},
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "dash0-auto-instrument=affirmative",
					},
				},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameStatusPreviousInstrumentWorkloadsLabelSelector: "dash0-auto-instrument=affirmative",
					},
				},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("with nil src annotations and spec.instrumentWorkloads.traceContext.propagators", convertFromTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
				},
				srcSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("tracecontext,xray"),
						},
					},
				},
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Annotations: map[string]string{
						annotationNameSpecInstrumentWorkloadsTraceContextPropagators: "tracecontext,xray",
					},
				},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("with nil src annotations and status.previousInstrumentWorkloads.traceContext.propagators", convertFromTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
				},
				srcSpec: dash0v1beta1.Dash0MonitoringSpec{},
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("xray"),
						},
					},
				},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Annotations: map[string]string{
						annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators: "xray",
					},
				},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("with nil src annotations and propagators in both attributes", convertFromTestCase{
				srcObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
				},
				srcSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("tracecontext,xray"),
						},
					},
				},
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("xray"),
						},
					},
				},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Annotations: map[string]string{
						annotationNameSpecInstrumentWorkloadsTraceContextPropagators:           "tracecontext,xray",
						annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators: "xray",
					},
				},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
			}),
			Entry("with multiple attributes not supported in v1alpha1", convertFromTestCase{
				srcObjectMeta: testObjectMeta(),
				srcSpec: dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "some-label,dash0-auto-instrument=yes,stage in (dev,prod)",
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("tracecontext,xray"),
						},
					},
				},
				srcStatus: dash0v1beta1.Dash0MonitoringStatus{
					PreviousInstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "dash0-auto-instrument=affirmative",
						TraceContext: dash0v1beta1.TraceContext{
							Propagators: ptr.To("xray"),
						},
					},
				},
				expectedDstObjectMeta: metav1.ObjectMeta{
					Namespace: TestNamespaceName,
					Name:      MonitoringResourceName,
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
						annotationNameSpecInstrumentWorkloadsTraceContextPropagators:           "tracecontext,xray",
						annotationNameStatusPreviousInstrumentWorkloadsTraceContextPropagators: "xray",
						annotationNameSpecInstrumentWorkloadsLabelSelector:                     "some-label,dash0-auto-instrument=yes,stage in (dev,prod)",
						annotationNameStatusPreviousInstrumentWorkloadsLabelSelector:           "dash0-auto-instrument=affirmative",
					},
				},
				expectedDstSpec:   Dash0MonitoringSpec{},
				expectedDstStatus: Dash0MonitoringStatus{},
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
