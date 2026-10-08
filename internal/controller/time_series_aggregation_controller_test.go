// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	timeSeriesAggregationName            = "test-time-series-aggregation"
	extraNamespaceTimeSeriesAggregations = "extra-namespace-time-series-aggregations"
	timeSeriesAggregationName2           = "test-time-series-aggregation-2"
	timeSeriesAggregationApiBasePath     = "/api/time-series-aggregations/"

	timeSeriesAggregationId                       = "time-series-aggregation-id"
	timeSeriesAggregationOriginPattern            = "dash0-operator_%s_test-dataset_test-namespace_test-time-series-aggregation"
	timeSeriesAggregationOriginPatternExtra       = "dash0-operator_%s_test-dataset_extra-namespace-time-series-aggregations_test-time-series-aggregation-2"
	timeSeriesAggregationOriginPatternAlternative = "dash0-operator_%s_test-dataset-alt_test-namespace_test-time-series-aggregation"
)

var (
	defaultExpectedPathTimeSeriesAggregation = fmt.Sprintf(
		"%s.*%s",
		timeSeriesAggregationApiBasePath,
		"dash0-operator_.*_test-dataset_test-namespace_test-time-series-aggregation",
	)
	expectedPathTimeSeriesAggregationAlternative = fmt.Sprintf(
		"%s.*%s",
		timeSeriesAggregationApiBasePath,
		"dash0-operator_.*_test-dataset-alt_test-namespace_test-time-series-aggregation",
	)
	defaultExpectedPathTimeSeriesAggregation2 = fmt.Sprintf(
		"%s.*%s",
		timeSeriesAggregationApiBasePath,
		"dash0-operator_.*_test-dataset_extra-namespace-time-series-aggregations_test-time-series-aggregation-2",
	)
	timeSeriesAggregationLeaderElectionAware = NewLeaderElectionAwareMock(true)
)

var _ = Describe(
	"The TimeSeriesAggregation controller", Ordered, func() {
		var (
			extraMonitoringResourceNames []types.NamespacedName
			testStartedAt                time.Time
			clusterId                    string
		)

		ctx := context.Background()
		logger := logd.FromContext(ctx)

		BeforeAll(
			func() {
				EnsureTestNamespaceExists(ctx, k8sClient)
				EnsureOperatorNamespaceExists(ctx, k8sClient)
				clusterId = string(cluster.ReadPseudoClusterUid(ctx, k8sClient, logger))
			},
		)

		BeforeEach(
			func() {
				testStartedAt = time.Now()
				extraMonitoringResourceNames = make([]types.NamespacedName, 0)
			},
		)

		AfterEach(
			func() {
				DeleteMonitoringResource(ctx, k8sClient)
				for _, name := range extraMonitoringResourceNames {
					DeleteMonitoringResourceByName(ctx, k8sClient, name, true)
				}
				extraMonitoringResourceNames = make([]types.NamespacedName, 0)
			},
		)

		Describe(
			"the time series aggregation reconciler", func() {
				var timeSeriesAggregationReconciler *TimeSeriesAggregationReconciler

				BeforeEach(
					func() {
						timeSeriesAggregationReconciler = createTimeSeriesAggregationReconciler(clusterId)

						// Set default API configs directly (not via SetDefaultApiConfigs) to avoid
						// triggering maybeDoInitialSynchronizationOfAllResources, which would set
						// initialSyncHasHappend to true and prevent the DescribeTable tests from
						// verifying initial sync behavior.
						timeSeriesAggregationReconciler.defaultApiConfigs.Set(
							[]ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							},
						)

					},
				)

				AfterEach(
					func() {
						DeleteMonitoringResourceIfItExists(ctx, k8sClient)
						deleteTimeSeriesAggregationResourceIfItExists(
							ctx, k8sClient, TestNamespaceName, timeSeriesAggregationName)
						deleteTimeSeriesAggregationResourceIfItExists(
							ctx, k8sClient, extraNamespaceTimeSeriesAggregations, timeSeriesAggregationName2)
					},
				)

				It(
					"it ignores time series aggregation resource changes if no Dash0 monitoring resource exists in the namespace", func() {
						expectTimeSeriesAggregationPutRequest(clusterId, defaultExpectedPathTimeSeriesAggregation)
						defer gock.Off()

						timeSeriesAggregationResource :=
							createTimeSeriesAggregationResource(TestNamespaceName, timeSeriesAggregationName)
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource)).To(Succeed())

						Expect(gock.IsPending()).To(BeTrue())
						verifyTimeSeriesAggregationHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							timeSeriesAggregationName,
						)
					},
				)

				It(
					"it ignores time series aggregation resource changes if the API endpoint is not configured", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectTimeSeriesAggregationPutRequest(clusterId, defaultExpectedPathTimeSeriesAggregation)
						defer gock.Off()

						timeSeriesAggregationReconciler.RemoveDefaultApiConfigs(ctx, logger)

						timeSeriesAggregationResource :=
							createTimeSeriesAggregationResource(TestNamespaceName, timeSeriesAggregationName)
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource)).To(Succeed())
						result, err := timeSeriesAggregationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      timeSeriesAggregationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsPending()).To(BeTrue())
						verifyTimeSeriesAggregationHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							timeSeriesAggregationName,
						)
					},
				)

				It(
					"creates a time series aggregation", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectTimeSeriesAggregationPutRequest(clusterId, defaultExpectedPathTimeSeriesAggregation)
						defer gock.Off()

						timeSeriesAggregationResource :=
							createTimeSeriesAggregationResource(TestNamespaceName, timeSeriesAggregationName)
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource)).To(Succeed())

						result, err := timeSeriesAggregationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      timeSeriesAggregationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyTimeSeriesAggregationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							timeSeriesAggregationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							timeSeriesAggregationId,
							fmt.Sprintf(timeSeriesAggregationOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates a time series aggregation with namespaced config from the monitoring resource", func() {
						monitoringResource := DefaultMonitoringResourceWithCustomApiConfigAndToken(
							MonitoringResourceQualifiedName,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
						)
						EnsureMonitoringResourceWithSpecExistsAndIsAvailable(ctx, k8sClient, monitoringResource.Spec)

						expectTimeSeriesAggregationPutRequestCustom(
							clusterId,
							expectedPathTimeSeriesAggregationAlternative,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
							timeSeriesAggregationOriginPatternAlternative,
							1,
						)
						defer gock.Off()

						timeSeriesAggregationResource :=
							createTimeSeriesAggregationResource(TestNamespaceName, timeSeriesAggregationName)
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource)).To(Succeed())

						timeSeriesAggregationReconciler.SetNamespacedApiConfigs(
							ctx, TestNamespaceName, []ApiConfig{
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, logger,
						)

						// note: we don't trigger reconcile here because setting the API config and token already triggers a reconciliation

						verifyTimeSeriesAggregationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							timeSeriesAggregationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							timeSeriesAggregationId,
							fmt.Sprintf(timeSeriesAggregationOriginPatternAlternative, clusterId),
							ApiEndpointStandardizedTestAlternative,
							DatasetCustomTestAlternative,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates a time series aggregation", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectTimeSeriesAggregationPutRequest(clusterId, defaultExpectedPathTimeSeriesAggregation)
						defer gock.Off()

						timeSeriesAggregationResource :=
							createTimeSeriesAggregationResource(TestNamespaceName, timeSeriesAggregationName)
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource)).To(Succeed())

						// Modify the resource
						timeSeriesAggregationResource.Spec.Sample.Interval = "120s"
						Expect(k8sClient.Update(ctx, timeSeriesAggregationResource)).To(Succeed())

						result, err := timeSeriesAggregationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      timeSeriesAggregationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyTimeSeriesAggregationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							timeSeriesAggregationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							timeSeriesAggregationId,
							fmt.Sprintf(timeSeriesAggregationOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a time series aggregation", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectTimeSeriesAggregationDeleteRequest(defaultExpectedPathTimeSeriesAggregation)
						defer gock.Off()

						timeSeriesAggregationResource :=
							createTimeSeriesAggregationResource(TestNamespaceName, timeSeriesAggregationName)
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, timeSeriesAggregationResource)).To(Succeed())

						result, err := timeSeriesAggregationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      timeSeriesAggregationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						// We do not call verifyTimeSeriesAggregationSynchronizationStatus in this test case since the
						// entire time series aggregation resource is deleted, hence there is nothing to write the status to.
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a time series aggregation if labelled with dash0.com/enable=false", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectTimeSeriesAggregationDeleteRequestWithHttpStatus(
							defaultExpectedPathTimeSeriesAggregation, http.StatusNotFound)
						defer gock.Off()

						timeSeriesAggregationResource := createTimeSeriesAggregationResourceWithEnableLabel(
							TestNamespaceName, timeSeriesAggregationName, "false")
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource)).To(Succeed())

						result, err := timeSeriesAggregationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      timeSeriesAggregationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyTimeSeriesAggregationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							timeSeriesAggregationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							"", // when deleting an object, we do not get an HTTP response body with an ID
							fmt.Sprintf(timeSeriesAggregationOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports http errors when synchronizing a time series aggregation", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathTimeSeriesAggregation).
							MatchParam("dataset", DatasetCustomTest).
							Times(3).
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						timeSeriesAggregationResource :=
							createTimeSeriesAggregationResource(TestNamespaceName, timeSeriesAggregationName)
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource)).To(Succeed())

						result, err := timeSeriesAggregationReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      timeSeriesAggregationName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyTimeSeriesAggregationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							timeSeriesAggregationName,
							dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							testStartedAt,
							"",
							"",
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"unexpected status code 503 when trying to synchronize the time-series-aggregation "+
								"\"test-time-series-aggregation\": "+
								"PUT https://api.dash0.com/api/time-series-aggregations/"+
								"dash0-operator_"+clusterId+
								"_test-dataset_test-namespace_test-time-series-aggregation?dataset=test-dataset, "+
								"response body is {}",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				type maybeDoInitialSynchronizationOfAllResourcesTest struct {
					disableSync func()
					enabledSync func()
				}

				DescribeTable(
					"synchronizes all existing time series aggregation resources when the auth token or api endpoint become available",
					func(testConfig maybeDoInitialSynchronizationOfAllResourcesTest) {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// Disable synchronization by removing the auth token or api endpoint.
						testConfig.disableSync()

						EnsureNamespaceExists(ctx, k8sClient, extraNamespaceTimeSeriesAggregations)
						secondMonitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
							ctx,
							k8sClient,
							MonitoringResourceDefaultSpecWithoutExport,
							types.NamespacedName{Namespace: extraNamespaceTimeSeriesAggregations, Name: MonitoringResourceName},
						)
						extraMonitoringResourceNames = append(
							extraMonitoringResourceNames, types.NamespacedName{
								Namespace: secondMonitoringResource.Namespace,
								Name:      secondMonitoringResource.Name,
							},
						)

						expectTimeSeriesAggregationPutRequest(clusterId, defaultExpectedPathTimeSeriesAggregation)
						expectTimeSeriesAggregationPutRequest(clusterId, defaultExpectedPathTimeSeriesAggregation2)
						defer gock.Off()

						timeSeriesAggregationResource1 :=
							createTimeSeriesAggregationResource(TestNamespaceName, timeSeriesAggregationName)
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource1)).To(Succeed())
						timeSeriesAggregationResource2 := createTimeSeriesAggregationResource(
							extraNamespaceTimeSeriesAggregations, timeSeriesAggregationName2)
						Expect(k8sClient.Create(ctx, timeSeriesAggregationResource2)).To(Succeed())

						// verify that the time series aggregations have not been synchronized yet
						Expect(gock.IsPending()).To(BeTrue())
						verifyTimeSeriesAggregationHasNoSynchronizationStatus(
							ctx, k8sClient, TestNamespaceName, timeSeriesAggregationName)
						verifyTimeSeriesAggregationHasNoSynchronizationStatus(
							ctx, k8sClient, extraNamespaceTimeSeriesAggregations, timeSeriesAggregationName2)

						// Now provide the auth token or API endpoint, which was unset before. This should trigger initial
						// synchronization of all resources.
						testConfig.enabledSync()

						// Verify both time series aggregation resources have been synchronized
						verifyTimeSeriesAggregationSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							timeSeriesAggregationName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							timeSeriesAggregationId,
							fmt.Sprintf(timeSeriesAggregationOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						verifyTimeSeriesAggregationSynchronizationStatus(
							ctx,
							k8sClient,
							extraNamespaceTimeSeriesAggregations,
							timeSeriesAggregationName2,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							timeSeriesAggregationId,
							fmt.Sprintf(timeSeriesAggregationOriginPatternExtra, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
					Entry(
						"when the auth token becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								timeSeriesAggregationReconciler.RemoveDefaultApiConfigs(ctx, logger)
							},
							enabledSync: func() {
								timeSeriesAggregationReconciler.SetDefaultApiConfigs(
									ctx, []ApiConfig{
										{
											Endpoint: ApiEndpointTest,
											Dataset:  DatasetCustomTest,
											Token:    AuthorizationTokenTest,
										},
									}, logger,
								)
							},
						},
					),
					Entry(
						"when the operator manager becomes leader", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								timeSeriesAggregationLeaderElectionAware.SetLeader(false)
							},
							enabledSync: func() {
								timeSeriesAggregationLeaderElectionAware.SetLeader(true)
								timeSeriesAggregationReconciler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
							},
						},
					),
				)
			},
		)

		Describe(
			"mapping time series aggregation resources to http requests", func() {

				type timeSeriesAggregationToRequestTestConfig struct {
					timeSeriesAggregation string
					expectedAnnotations   map[string]string
				}

				var timeSeriesAggregationReconciler *TimeSeriesAggregationReconciler

				BeforeEach(
					func() {
						timeSeriesAggregationReconciler = &TimeSeriesAggregationReconciler{}
					},
				)

				DescribeTable(
					"maps time series aggregations", func(testConfig timeSeriesAggregationToRequestTestConfig) {
						timeSeriesAggregation := map[string]any{}
						Expect(yaml.Unmarshal([]byte(testConfig.timeSeriesAggregation), &timeSeriesAggregation)).To(Succeed())
						apiConfig := ApiConfig{
							Endpoint: ApiEndpointTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						}
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:      "dash0-time-series-aggregation",
							k8sNamespace: TestNamespaceName,
							resource:     timeSeriesAggregation,
							validatedApiConfigs: []ValidatedApiConfigAndToken{
								*NewValidatedApiConfigAndToken(apiConfig.Endpoint, apiConfig.Dataset, apiConfig.Token),
							},
						}
						resourceToRequestsResult :=
							timeSeriesAggregationReconciler.MapResourceToHttpRequests(
								preconditionValidationResult, apiConfig, upsertAction, logger)
						Expect(resourceToRequestsResult.OriginsInResource).To(BeNil())
						Expect(resourceToRequestsResult.ValidationIssues).To(BeNil())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())

						Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(1))
						apiRequest := resourceToRequestsResult.ApiRequests[0]
						Expect(apiRequest.ItemName).To(Equal("dash0-time-series-aggregation"))
						req := apiRequest.Request
						defer func() {
							_ = req.Body.Close()
						}()
						body, err := io.ReadAll(req.Body)
						Expect(err).ToNot(HaveOccurred())
						resultingTimeSeriesAggregationInRequest := map[string]any{}
						Expect(json.Unmarshal(body, &resultingTimeSeriesAggregationInRequest)).To(Succeed())
						Expect(resultingTimeSeriesAggregationInRequest["spec"]).ToNot(BeNil())

						Expect(resultingTimeSeriesAggregationInRequest["metadata"]).ToNot(BeNil())
						Expect(ReadFromMap(resultingTimeSeriesAggregationInRequest, []string{"metadata", "name"})).
							To(Equal("dash0-time-series-aggregation"))

						if testConfig.expectedAnnotations != nil {
							annotationsRaw := ReadFromMap(
								resultingTimeSeriesAggregationInRequest, []string{"metadata", "annotations"})
							Expect(annotationsRaw).ToNot(BeNil())
							annotations := annotationsRaw.(map[string]any)
							Expect(annotations).To(HaveLen(len(testConfig.expectedAnnotations)))
							for expectedKey, expectedValue := range testConfig.expectedAnnotations {
								value, ok := annotations[expectedKey]
								Expect(ok).To(BeTrue())
								Expect(value).To(Equal(expectedValue))
							}
						} else {
							Expect(ReadFromMap(
								resultingTimeSeriesAggregationInRequest, []string{"metadata", "annotations"})).To(BeNil())
						}
					},
					Entry(
						"should map time series aggregation", timeSeriesAggregationToRequestTestConfig{
							timeSeriesAggregation: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: dash0-time-series-aggregation
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is
      value: http.server.duration
  sample:
    interval: 60s
`,
						},
					),
					Entry(
						"should map time series aggregation with values", timeSeriesAggregationToRequestTestConfig{
							timeSeriesAggregation: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: dash0-time-series-aggregation
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is_one_of
      values:
        - http.server.duration
        - http.client.duration
    otherFilters:
      - key: k8s.namespace.name
        operator: is
        value: kube-system
  sample:
    interval: 60s
`,
						},
					),
					Entry(
						"should send annotations", timeSeriesAggregationToRequestTestConfig{
							timeSeriesAggregation: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: dash0-time-series-aggregation
  annotations:
    dash0com/annotation1: value1
    dash0com/annotation2: value2
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is
      value: http.server.duration
  sample:
    interval: 60s
`,
							expectedAnnotations: map[string]string{
								"dash0com/annotation1": "value1",
								"dash0com/annotation2": "value2",
							},
						},
					),
				)
			},
		)
	},
)

func createTimeSeriesAggregationReconciler(clusterId string) *TimeSeriesAggregationReconciler {
	timeSeriesAggregationReconciler := NewTimeSeriesAggregationReconciler(
		k8sClient,
		types.UID(clusterId),
		timeSeriesAggregationLeaderElectionAware,
		TestHTTPClient(),
	)
	return timeSeriesAggregationReconciler
}

func expectTimeSeriesAggregationPutRequestCustom(
	clusterId string,
	expectedPath string,
	endpoint string,
	dataset string,
	token string,
	originPattern string,
	times int,
) {
	gock.New(endpoint).
		Put(expectedPath).
		MatchHeader("Authorization", token).
		MatchParam("dataset", dataset).
		Times(times).
		Reply(200).
		JSON(timeSeriesAggregationPutResponse(clusterId, originPattern, dataset))
}

func expectTimeSeriesAggregationPutRequest(clusterId string, expectedPath string) {
	expectTimeSeriesAggregationPutRequestCustom(
		clusterId,
		expectedPath,
		ApiEndpointTest,
		DatasetCustomTest,
		AuthorizationHeaderTest,
		timeSeriesAggregationOriginPattern,
		1,
	)
}

func timeSeriesAggregationPutResponse(clusterId string, originPattern string, dataset string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"dash0.com/id":      timeSeriesAggregationId,
				"dash0.com/origin":  fmt.Sprintf(originPattern, clusterId),
				"dash0.com/dataset": dataset,
			},
		},
	}
}

func expectTimeSeriesAggregationDeleteRequest(expectedPath string) {
	expectTimeSeriesAggregationDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectTimeSeriesAggregationDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status)
}

func createTimeSeriesAggregationResource(namespace string, name string) *dash0v1alpha1.Dash0TimeSeriesAggregation {
	return createTimeSeriesAggregationResourceWithEnableLabel(namespace, name, "")
}

func createTimeSeriesAggregationResourceWithEnableLabel(
	namespace string,
	name string,
	dash0EnableLabelValue string,
) *dash0v1alpha1.Dash0TimeSeriesAggregation {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1alpha1.Dash0TimeSeriesAggregation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0TimeSeriesAggregation",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1alpha1.Dash0TimeSeriesAggregationSpec{
			Enabled: true,
			Match: dash0v1alpha1.Dash0TimeSeriesAggregationMatch{
				MetricNameMatcher: dash0v1alpha1.Dash0TimeSeriesAggregationMatcher{
					Operator: "is",
					Value:    ptr.To("http.server.duration"),
				},
			},
			Sample: dash0v1alpha1.Dash0TimeSeriesAggregationSample{
				Interval: "60s",
			},
		},
	}
}

func deleteTimeSeriesAggregationResourceIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	timeSeriesAggregation := &dash0v1alpha1.Dash0TimeSeriesAggregation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(
		ctx, timeSeriesAggregation, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifyTimeSeriesAggregationSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
	expectedStatus dash0common.Dash0ApiResourceSynchronizationStatus,
	testStartedAt time.Time,
	expectedId string,
	expectedOrigin string,
	expectedApiEndpoint string,
	expectedDataset string,
	expectedError string,
) {
	Eventually(
		func(g Gomega) {
			timeSeriesAggregation := &dash0v1alpha1.Dash0TimeSeriesAggregation{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, timeSeriesAggregation,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(timeSeriesAggregation.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(timeSeriesAggregation.Status.SynchronizedAt.Time).
				To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(timeSeriesAggregation.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			// time series aggregations have no operator-side validations, all local validations are encoded in the CRD already
			g.Expect(timeSeriesAggregation.Status.ValidationIssues).To(BeNil())

			g.Expect(timeSeriesAggregation.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpointAndDataset := timeSeriesAggregation.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpointAndDataset).ToNot(BeNil())
			g.Expect(syncResultPerEndpointAndDataset.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Dataset).To(Equal(expectedDataset))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(syncResultPerEndpointAndDataset.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}

func verifyTimeSeriesAggregationHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	Eventually(
		func(g Gomega) {
			timeSeriesAggregation := &dash0v1alpha1.Dash0TimeSeriesAggregation{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, timeSeriesAggregation,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(timeSeriesAggregation.Status.SynchronizationStatus)).To(Equal(""))
			g.Expect(timeSeriesAggregation.Status.ValidationIssues).To(BeNil())
			g.Expect(timeSeriesAggregation.Status.SynchronizationResults).To(HaveLen(0))
		},
	).Should(Succeed())
}
