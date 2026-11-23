// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	apiSyncTestMetricNamePrefix = "dash0.operator.manager."
	apiSyncTestDataset          = "test-dataset"
)

var (
	apiSyncTestKey = apiSyncResourceKey{
		kind:      "PrometheusRule",
		namespace: "test-namespace",
		name:      "test-rule",
	}
	apiSyncTestApiConfig = ApiConfig{
		Endpoint: "https://api.dash0.com/",
		Dataset:  apiSyncTestDataset,
		Token:    "token",
	}
)

var _ = Describe("API sync self-monitoring metrics", func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)
	var reader *sdkmetric.ManualReader

	BeforeEach(func() {
		reader = initApiSyncMetricsForTest(logger)
	})

	AfterEach(func() {
		resetApiSyncMetricsForTest()
	})

	Describe("recordApiSyncResult", func() {
		It("records nothing for a no-op", func() {
			result := &ResourceToRequestsResult{ApiConfig: apiSyncTestApiConfig}

			Expect(recordApiSyncResult(ctx, apiSyncTestKey, apiSyncTestApiConfig, upsertAction, result, 0)).To(BeNil())

			Expect(collectApiSyncSynchronizations(ctx, reader)).To(BeEmpty())
		})

		It("counts a successful synchronization and does not report a failure", func() {
			result := &ResourceToRequestsResult{
				ApiConfig:   apiSyncTestApiConfig,
				ApiRequests: []WrappedApiRequest{{ItemName: "rule-1"}},
			}

			Expect(recordApiSyncResult(ctx, apiSyncTestKey, apiSyncTestApiConfig, upsertAction, result, 1)).To(BeNil())

			Expect(collectApiSyncSynchronizations(ctx, reader)).To(ConsistOf(
				metricdata.DataPoint[int64]{
					Attributes: attribute.NewSet(
						attribute.String(apiSyncAttributeResourceKind, "PrometheusRule"),
						attribute.String(apiSyncAttributeResourceNamespace, "test-namespace"),
						attribute.String(apiSyncAttributeDataset, apiSyncTestDataset),
						attribute.String(apiSyncAttributeAction, apiSyncActionUpsert),
						attribute.String(apiSyncAttributeStatus, "successful"),
					),
					Value: 1,
				},
			))
		})

		It("reports a failed synchronization with the highest HTTP status code", func() {
			result := &ResourceToRequestsResult{
				ApiConfig: apiSyncTestApiConfig,
				SynchronizationErrors: map[string]string{
					"rule-1": "bad request",
					"rule-2": "service unavailable",
				},
				SynchronizationErrorStatusCodes: map[string]int{
					"rule-1": http.StatusBadRequest,
					"rule-2": http.StatusServiceUnavailable,
				},
			}

			failure := recordApiSyncResult(ctx, apiSyncTestKey, apiSyncTestApiConfig, deleteAction, result, 0)

			Expect(failure).To(Equal(&apiSyncFailure{
				dataset:        apiSyncTestDataset,
				status:         dash0common.Dash0ApiResourceSynchronizationStatusFailed,
				httpStatusCode: http.StatusServiceUnavailable,
			}))
			synchronizations := collectApiSyncSynchronizations(ctx, reader)
			Expect(synchronizations).To(HaveLen(1))
			Expect(apiSyncAttributeValue(synchronizations[0].Attributes, apiSyncAttributeAction)).To(
				Equal(attribute.StringValue(apiSyncActionDelete)))
			Expect(apiSyncAttributeValue(synchronizations[0].Attributes, apiSyncAttributeStatus)).To(
				Equal(attribute.StringValue("failed")))
		})

		It("reports a partially successful synchronization", func() {
			result := &ResourceToRequestsResult{
				ApiConfig:   apiSyncTestApiConfig,
				ApiRequests: []WrappedApiRequest{{ItemName: "rule-1"}},
				ValidationIssues: map[string][]string{
					"rule-2": {"no expression"},
				},
			}

			Expect(recordApiSyncResult(ctx, apiSyncTestKey, apiSyncTestApiConfig, upsertAction, result, 1)).To(
				Equal(&apiSyncFailure{
					dataset: apiSyncTestDataset,
					status:  dash0common.Dash0ApiResourceSynchronizationStatusPartiallySuccessful,
				}))
		})
	})

	Describe("recordApiSyncPreconditionFailure", func() {
		It("counts the failure and replaces the previous failures of the resource", func() {
			apiSyncFailures.set(apiSyncTestKey, []apiSyncFailure{
				{
					dataset:        apiSyncTestDataset,
					status:         dash0common.Dash0ApiResourceSynchronizationStatusFailed,
					httpStatusCode: http.StatusBadRequest,
				},
			})

			recordApiSyncPreconditionFailure(
				ctx, apiSyncTestKey, upsertAction, apiSyncPreconditionFailureApiConfigUnavailable,
			)

			Expect(collectApiSyncSynchronizations(ctx, reader)).To(ConsistOf(
				metricdata.DataPoint[int64]{
					Attributes: attribute.NewSet(
						attribute.String(apiSyncAttributeResourceKind, "PrometheusRule"),
						attribute.String(apiSyncAttributeResourceNamespace, "test-namespace"),
						attribute.String(apiSyncAttributeAction, apiSyncActionUpsert),
						attribute.String(apiSyncAttributeStatus, "failed"),
						attribute.String(apiSyncAttributeErrorType, "api_config_unavailable"),
					),
					Value: 1,
				},
			))
			Expect(collectApiSyncUnsynchronizedResources(ctx, reader)).To(ConsistOf(
				metricdata.DataPoint[int64]{
					Attributes: attribute.NewSet(
						attribute.String(apiSyncAttributeResourceKind, "PrometheusRule"),
						attribute.String(apiSyncAttributeResourceNamespace, "test-namespace"),
						attribute.String(apiSyncAttributeResourceName, "test-rule"),
						attribute.String(apiSyncAttributeStatus, "failed"),
						attribute.String(apiSyncAttributeErrorType, "api_config_unavailable"),
					),
					Value: 1,
				},
			))
		})
	})

	Describe("synchronizing a resource when the lookup of the Dash0 monitoring resource fails", func() {
		const lookupFailureViewName = "lookup-failure-view"
		lookupFailureKey := apiSyncResourceKey{kind: "Dash0View", namespace: TestNamespaceName, name: lookupFailureViewName}
		var viewReconciler *ViewReconciler
		var viewResource *unstructured.Unstructured

		BeforeEach(func() {
			failingClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
						return errors.New("list failed")
					},
				}).
				Build()
			viewReconciler = NewViewReconciler(
				failingClient,
				"cluster-id",
				NewLeaderElectionAwareMock(true),
				recorder,
				testApiClientPool(),
			)
			viewReconciler.defaultApiConfigs.Set([]ApiConfig{apiSyncTestApiConfig})
			var err error
			viewResource, err = structToMap(createViewResource(TestNamespaceName, lookupFailureViewName))
			Expect(err).NotTo(HaveOccurred())
			setApiSyncTestFailure(lookupFailureKey)
		})

		It("keeps reporting the resource, with the precondition failure", func() {
			synchronizeViaApiAndUpdateStatus(ctx, viewReconciler, viewResource, nil, upsertAction, logger)

			Expect(collectApiSyncUnsynchronizedResources(ctx, reader)).To(ConsistOf(
				metricdata.DataPoint[int64]{
					Attributes: attribute.NewSet(
						attribute.String(apiSyncAttributeResourceKind, "Dash0View"),
						attribute.String(apiSyncAttributeResourceNamespace, TestNamespaceName),
						attribute.String(apiSyncAttributeResourceName, lookupFailureViewName),
						attribute.String(apiSyncAttributeStatus, "failed"),
						attribute.String(apiSyncAttributeErrorType, "monitoring_resource_lookup_failed"),
					),
					Value: 1,
				},
			))
		})

		It("stops reporting the resource when it has been deleted", func() {
			synchronizeViaApiAndUpdateStatus(ctx, viewReconciler, viewResource, nil, deleteAction, logger)

			Expect(collectApiSyncUnsynchronizedResources(ctx, reader)).To(BeEmpty())
		})
	})

	Describe("the unsynchronized resources gauge", func() {
		It("reports nothing when there are no failures", func() {
			Expect(collectApiSyncUnsynchronizedResources(ctx, reader)).To(BeEmpty())
		})

		It("reports each failure of a resource", func() {
			apiSyncFailures.set(apiSyncTestKey, []apiSyncFailure{
				{
					dataset:        apiSyncTestDataset,
					status:         dash0common.Dash0ApiResourceSynchronizationStatusFailed,
					httpStatusCode: http.StatusBadRequest,
				},
				{
					dataset: "other-dataset",
					status:  dash0common.Dash0ApiResourceSynchronizationStatusPartiallySuccessful,
				},
			})

			Expect(collectApiSyncUnsynchronizedResources(ctx, reader)).To(ConsistOf(
				metricdata.DataPoint[int64]{
					Attributes: attribute.NewSet(
						attribute.String(apiSyncAttributeResourceKind, "PrometheusRule"),
						attribute.String(apiSyncAttributeResourceNamespace, "test-namespace"),
						attribute.String(apiSyncAttributeResourceName, "test-rule"),
						attribute.String(apiSyncAttributeDataset, apiSyncTestDataset),
						attribute.String(apiSyncAttributeStatus, "failed"),
						attribute.Int(apiSyncAttributeHttpStatusCode, http.StatusBadRequest),
					),
					Value: 1,
				},
				metricdata.DataPoint[int64]{
					Attributes: attribute.NewSet(
						attribute.String(apiSyncAttributeResourceKind, "PrometheusRule"),
						attribute.String(apiSyncAttributeResourceNamespace, "test-namespace"),
						attribute.String(apiSyncAttributeResourceName, "test-rule"),
						attribute.String(apiSyncAttributeDataset, "other-dataset"),
						attribute.String(apiSyncAttributeStatus, "partially-successful"),
					),
					Value: 1,
				},
			))
		})

		It("stops reporting a resource once it has no failures", func() {
			setApiSyncTestFailure(apiSyncTestKey)

			apiSyncFailures.set(apiSyncTestKey, nil)

			Expect(collectApiSyncUnsynchronizedResources(ctx, reader)).To(BeEmpty())
		})
	})

	Describe("removing failures", func() {
		otherName := apiSyncResourceKey{kind: "PrometheusRule", namespace: "test-namespace", name: "other"}
		otherNamespace := apiSyncResourceKey{kind: "PrometheusRule", namespace: "other-namespace", name: "test-rule"}
		otherKind := apiSyncResourceKey{kind: "PersesDashboard", namespace: "test-namespace", name: "test-rule"}

		BeforeEach(func() {
			for _, key := range []apiSyncResourceKey{apiSyncTestKey, otherName, otherNamespace, otherKind} {
				setApiSyncTestFailure(key)
			}
		})

		DescribeTable("removes only the matching failures",
			func(remove func(), expectedRemaining []apiSyncResourceKey) {
				remove()
				Expect(apiSyncFailureKeys()).To(ConsistOf(expectedRemaining))
			},
			Entry("by resource",
				func() { apiSyncFailures.delete(apiSyncTestKey) },
				[]apiSyncResourceKey{otherName, otherNamespace, otherKind},
			),
			Entry("by namespace",
				func() { apiSyncFailures.deleteNamespace("test-namespace") },
				[]apiSyncResourceKey{otherNamespace},
			),
			Entry("by kind",
				func() { apiSyncFailures.deleteKind("PrometheusRule") },
				[]apiSyncResourceKey{otherKind},
			),
			Entry("by kind in namespace",
				func() { apiSyncFailures.deleteKindInNamespace("PrometheusRule", "test-namespace") },
				[]apiSyncResourceKey{otherNamespace, otherKind},
			),
			Entry("by precondition failure in namespace",
				func() {
					for _, key := range []apiSyncResourceKey{otherName, otherNamespace} {
						apiSyncFailures.set(key, []apiSyncFailure{{
							status:              dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							preconditionFailure: apiSyncPreconditionFailureApiConfigUnavailable,
						}})
					}
					apiSyncFailures.deletePreconditionFailuresInNamespace(
						"test-namespace",
						apiSyncPreconditionFailureApiConfigUnavailable,
					)
				},
				[]apiSyncResourceKey{apiSyncTestKey, otherNamespace, otherKind},
			),
		)
	})
})

func initApiSyncMetricsForTest(logger logd.Logger) *sdkmetric.ManualReader {
	apiSyncFailures.clear()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	(&ApiSyncMetrics{}).InitializeSelfMonitoringMetrics(meter, apiSyncTestMetricNamePrefix, logger)
	return reader
}

func resetApiSyncMetricsForTest() {
	apiSyncFailures.clear()
	apiSyncSynchronizationsMetric = nil
}

func setApiSyncTestFailure(key apiSyncResourceKey) {
	apiSyncFailures.set(key, []apiSyncFailure{
		{dataset: apiSyncTestDataset, status: dash0common.Dash0ApiResourceSynchronizationStatusFailed},
	})
}

func apiSyncFailureKeys() []apiSyncResourceKey {
	apiSyncFailures.mutex.Lock()
	defer apiSyncFailures.mutex.Unlock()
	keys := make([]apiSyncResourceKey, 0, len(apiSyncFailures.failures))
	for key := range apiSyncFailures.failures {
		keys = append(keys, key)
	}
	return keys
}

func collectApiSyncMetric(ctx context.Context, reader *sdkmetric.ManualReader, suffix string) *metricdata.Metrics {
	var resourceMetrics metricdata.ResourceMetrics
	Expect(reader.Collect(ctx, &resourceMetrics)).To(Succeed())
	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name == apiSyncTestMetricNamePrefix+suffix {
				return &metric
			}
		}
	}
	return nil
}

func collectApiSyncSynchronizations(
	ctx context.Context,
	reader *sdkmetric.ManualReader,
) []metricdata.DataPoint[int64] {
	metric := collectApiSyncMetric(ctx, reader, apiSyncSynchronizationsMetricSuffix)
	if metric == nil {
		return nil
	}
	return withoutTimestamps(metric.Data.(metricdata.Sum[int64]).DataPoints)
}

func collectApiSyncUnsynchronizedResources(
	ctx context.Context,
	reader *sdkmetric.ManualReader,
) []metricdata.DataPoint[int64] {
	metric := collectApiSyncMetric(ctx, reader, apiSyncUnsynchronizedResourcesMetricSuffix)
	if metric == nil {
		return nil
	}
	return withoutTimestamps(metric.Data.(metricdata.Gauge[int64]).DataPoints)
}

func withoutTimestamps(dataPoints []metricdata.DataPoint[int64]) []metricdata.DataPoint[int64] {
	result := make([]metricdata.DataPoint[int64], 0, len(dataPoints))
	for _, dataPoint := range dataPoints {
		result = append(result, metricdata.DataPoint[int64]{
			Attributes: dataPoint.Attributes,
			Value:      dataPoint.Value,
		})
	}
	return result
}

func apiSyncAttributeValue(attributes attribute.Set, key string) attribute.Value {
	value, _ := attributes.Value(attribute.Key(key))
	return value
}
