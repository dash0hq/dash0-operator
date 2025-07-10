// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The conversion webhook for the monitoring resource", Ordered, func() {

	BeforeAll(func() {
		operatorConfigurationResource := CreateOperatorConfigurationResourceWithSpec(
			ctx,
			k8sClient,
			dash0v1alpha1.Dash0OperatorConfigurationSpec{
				Export: Dash0ExportWithEndpointAndToken(),
			},
		)
		operatorConfigurationResource.EnsureResourceIsMarkedAsAvailable()
		Expect(k8sClient.Status().Update(ctx, operatorConfigurationResource)).To(Succeed())
	})

	AfterAll(func() {
		Expect(k8sClient.DeleteAllOf(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{})).To(Succeed())
	})

	AfterEach(func() {
		Expect(
			k8sClient.DeleteAllOf(ctx, &dash0v1beta1.Dash0Monitoring{}, client.InNamespace(TestNamespaceName)),
		).To(Succeed())
	})

	type convertToTestCase struct {
		srcSpec         dash0v1alpha1.Dash0MonitoringSpec
		expectedDstSpec dash0v1beta1.Dash0MonitoringSpec
		check           func(convertedSpec dash0v1beta1.Dash0MonitoringSpec)
	}

	DescribeTable("should convert v1alpha1 to hub version when a v1alpha1 version is deployed", func(testConfig convertToTestCase) {
		source := dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: MonitoringResourceObjectMetaWithLabelAndAnnotation,
			Spec:       testConfig.srcSpec,
		}
		Expect(k8sClient.Create(ctx, &source)).To(Succeed())

		convertedResource := dash0v1beta1.Dash0Monitoring{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Namespace: source.ObjectMeta.Namespace,
			Name:      source.ObjectMeta.Name,
		}, &convertedResource)).To(Succeed())
		Expect(convertedResource).ToNot(BeNil())

		Expect(convertedResource.Namespace).To(Equal(TestNamespaceName))
		Expect(convertedResource.Name).To(Equal(MonitoringResourceName))
		Expect(convertedResource.Labels).To(HaveLen(1))
		Expect(convertedResource.Labels["test-label"]).To(Equal("test-value"))
		Expect(convertedResource.Annotations).To(HaveLen(1))
		Expect(convertedResource.Annotations["test-annotation"]).To(Equal("test-value"))

		if testConfig.check != nil {
			testConfig.check(convertedResource.Spec)
		} else {
			Expect(convertedResource.Spec.InstrumentWorkloads).To(Equal(testConfig.expectedDstSpec.InstrumentWorkloads))
			Expect(convertedResource.Spec.LogCollection).To(Equal(testConfig.expectedDstSpec.LogCollection))
			Expect(convertedResource.Spec.PrometheusScraping).To(Equal(testConfig.expectedDstSpec.PrometheusScraping))
			if testConfig.expectedDstSpec.Filter == nil {
				Expect(convertedResource.Spec.Filter).To(BeNil())
			} else {
				Expect(*convertedResource.Spec.Filter).To(Equal(*testConfig.expectedDstSpec.Filter))
			}
			if testConfig.expectedDstSpec.Transform == nil {
				Expect(convertedResource.Spec.Transform).To(BeNil())
			} else {
				Expect(*convertedResource.Spec.Transform).To(Equal(*testConfig.expectedDstSpec.Transform))
			}
			Expect(convertedResource.Spec.SynchronizePersesDashboards).To(Equal(testConfig.expectedDstSpec.SynchronizePersesDashboards))
			Expect(convertedResource.Spec.SynchronizePrometheusRules).To(Equal(testConfig.expectedDstSpec.SynchronizePrometheusRules))
		}
	},
		Entry("empty spec", convertToTestCase{
			srcSpec: dash0v1alpha1.Dash0MonitoringSpec{},
			// the mutating webhook will apply all defaults, so the expected spec will be all default values
			expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
				InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
					Mode: dash0common.InstrumentWorkloadsModeAll,
				},
				LogCollection: dash0common.LogCollection{
					Enabled: ptr.To(true),
				},
				PrometheusScraping: dash0common.PrometheusScraping{
					Enabled: ptr.To(true),
				},
				SynchronizePersesDashboards: ptr.To(true),
				SynchronizePrometheusRules:  ptr.To(true),
			},
		}),
		Entry("full spec", convertToTestCase{
			srcSpec: dash0v1alpha1.Dash0MonitoringSpec{
				InstrumentWorkloads: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
				LogCollection: dash0common.LogCollection{
					Enabled: ptr.To(false),
				},
				PrometheusScraping: dash0common.PrometheusScraping{
					Enabled: ptr.To(false),
				},
				Filter:                      testFilter(),
				Transform:                   testTransform(),
				SynchronizePersesDashboards: ptr.To(false),
				SynchronizePrometheusRules:  ptr.To(false),
			},
			expectedDstSpec: dash0v1beta1.Dash0MonitoringSpec{
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
				SynchronizePersesDashboards: ptr.To(false),
				SynchronizePrometheusRules:  ptr.To(false),
			},
		}),
		Entry("convert legacy prometheus scraping setting (disabled only via legacy setting)", convertToTestCase{
			srcSpec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScrapingEnabled: ptr.To(false),
			},
			check: func(convertedSpec dash0v1beta1.Dash0MonitoringSpec) {
				Expect(*convertedSpec.PrometheusScraping.Enabled).To(BeFalse())
			},
		}),
		Entry("convert legacy prometheus scraping setting (disabled via both settings)", convertToTestCase{
			srcSpec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScraping: dash0common.PrometheusScraping{
					Enabled: ptr.To(false),
				},
				PrometheusScrapingEnabled: ptr.To(false),
			},
			check: func(convertedSpec dash0v1beta1.Dash0MonitoringSpec) {
				Expect(*convertedSpec.PrometheusScraping.Enabled).To(BeFalse())
			},
		}),
		Entry("convert legacy prometheus scraping setting (legacy false, new setting true)", convertToTestCase{
			srcSpec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScraping: dash0common.PrometheusScraping{
					Enabled: ptr.To(true),
				},
				PrometheusScrapingEnabled: ptr.To(false),
			},
			check: func(convertedSpec dash0v1beta1.Dash0MonitoringSpec) {
				Expect(*convertedSpec.PrometheusScraping.Enabled).To(BeFalse())
			},
		}),
		Entry("convert legacy prometheus scraping setting (legacy true, new setting false)", convertToTestCase{
			srcSpec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScraping: dash0common.PrometheusScraping{
					Enabled: ptr.To(false),
				},
				PrometheusScrapingEnabled: ptr.To(true),
			},
			check: func(convertedSpec dash0v1beta1.Dash0MonitoringSpec) {
				Expect(*convertedSpec.PrometheusScraping.Enabled).To(BeFalse())
			},
		}),
	)

	type convertFromTestCase struct {
		srcSpec         dash0v1beta1.Dash0MonitoringSpec
		expectedDstSpec dash0v1alpha1.Dash0MonitoringSpec
	}

	DescribeTable("should convert from hub version to v1alpha1 when requested", func(testConfig convertFromTestCase) {
		source := dash0v1beta1.Dash0Monitoring{
			TypeMeta: metav1.TypeMeta{
				Kind: "dash0v1beta1.Dash0Monitoring",
			},
			ObjectMeta: MonitoringResourceObjectMetaWithLabelAndAnnotation,
			Spec:       testConfig.srcSpec,
		}
		Expect(k8sClient.Create(ctx, &source)).To(Succeed())

		legacyResource := dash0v1alpha1.Dash0Monitoring{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Namespace: source.ObjectMeta.Namespace,
			Name:      source.ObjectMeta.Name,
		}, &legacyResource)).To(Succeed())
		Expect(legacyResource).ToNot(BeNil())

		Expect(legacyResource.Namespace).To(Equal(TestNamespaceName))
		Expect(legacyResource.Name).To(Equal(MonitoringResourceName))
		Expect(legacyResource.Labels).To(HaveLen(1))
		Expect(legacyResource.Labels["test-label"]).To(Equal("test-value"))
		Expect(legacyResource.Annotations).To(HaveLen(1))
		Expect(legacyResource.Annotations["test-annotation"]).To(Equal("test-value"))

		Expect(legacyResource.Spec.InstrumentWorkloads).To(Equal(testConfig.expectedDstSpec.InstrumentWorkloads))
		Expect(legacyResource.Spec.LogCollection).To(Equal(testConfig.expectedDstSpec.LogCollection))
		Expect(legacyResource.Spec.PrometheusScraping).To(Equal(testConfig.expectedDstSpec.PrometheusScraping))
		if testConfig.expectedDstSpec.Filter == nil {
			Expect(legacyResource.Spec.Filter).To(BeNil())
		} else {
			Expect(*legacyResource.Spec.Filter).To(Equal(*testConfig.expectedDstSpec.Filter))
		}
		if testConfig.expectedDstSpec.Transform == nil {
			Expect(legacyResource.Spec.Transform).To(BeNil())
		} else {
			Expect(*legacyResource.Spec.Transform).To(Equal(*testConfig.expectedDstSpec.Transform))
		}
		Expect(legacyResource.Spec.SynchronizePersesDashboards).To(Equal(testConfig.expectedDstSpec.SynchronizePersesDashboards))
		Expect(legacyResource.Spec.SynchronizePrometheusRules).To(Equal(testConfig.expectedDstSpec.SynchronizePrometheusRules))
	},
		Entry("empty spec", convertFromTestCase{
			srcSpec: dash0v1beta1.Dash0MonitoringSpec{},
			// the mutating webhook will apply all defaults, so the expected spec will be all default values
			expectedDstSpec: dash0v1alpha1.Dash0MonitoringSpec{
				InstrumentWorkloads: dash0common.InstrumentWorkloadsModeAll,
				LogCollection: dash0common.LogCollection{
					Enabled: ptr.To(true),
				},
				PrometheusScraping: dash0common.PrometheusScraping{
					Enabled: ptr.To(true),
				},
				SynchronizePersesDashboards: ptr.To(true),
				SynchronizePrometheusRules:  ptr.To(true),
			},
		}),
		Entry("full spec", convertFromTestCase{
			srcSpec: dash0v1beta1.Dash0MonitoringSpec{
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
				SynchronizePersesDashboards: ptr.To(false),
				SynchronizePrometheusRules:  ptr.To(false),
			},
			expectedDstSpec: dash0v1alpha1.Dash0MonitoringSpec{
				InstrumentWorkloads: dash0common.InstrumentWorkloadsModeCreatedAndUpdated,
				LogCollection: dash0common.LogCollection{
					Enabled: ptr.To(false),
				},
				PrometheusScraping: dash0common.PrometheusScraping{
					Enabled: ptr.To(false),
				},
				Filter:                      testFilter(),
				Transform:                   testTransform(),
				SynchronizePersesDashboards: ptr.To(false),
				SynchronizePrometheusRules:  ptr.To(false),
			},
		}),
	)
})

func testFilter() *dash0common.Filter {
	return &dash0common.Filter{
		ErrorMode: dash0common.FilterTransformErrorModePropagate,
		Traces: &dash0common.TraceFilter{
			SpanFilter: []string{
				`attributes["http.route"] == "/ready"`,
				`attributes["http.route"] == "/metrics"`,
			},
			SpanEventFilter: []string{
				`attributes["grpc"] == true`,
				`IsMatch(name, ".*grpc.*")`,
			},
		},
		Metrics: &dash0common.MetricFilter{
			MetricFilter: []string{
				`name == "k8s.replicaset.available"`,
				`name == "k8s.replicaset.desired"`,
			},
			DataPointFilter: []string{
				`metric.type == METRIC_DATA_TYPE_SUMMARY`,
				`resource.attributes["service.name"] == "my_service_name"`,
			},
		},
		Logs: &dash0common.LogFilter{
			LogRecordFilter: []string{
				`IsMatch(body, ".*password.*")`,
				`severity_number < SEVERITY_NUMBER_WARN`,
			},
		},
	}
}

func testTransform() *dash0common.Transform {
	return &dash0common.Transform{
		ErrorMode: ptr.To(dash0common.FilterTransformErrorModePropagate),
		Traces: []json.RawMessage{
			[]byte(`"truncate_all(span.attributes, 1024)"`),
		},
		Metrics: []json.RawMessage{
			[]byte(`{"conditions":["metric.type == METRIC_DATA_TYPE_SUM"],"statements":["truncate_all(datapoint.attributes, 1024)"]}`),
		},
		Logs: []json.RawMessage{
			[]byte(`"truncate_all(log.attributes, 1024)"`),
		},
	}
}
