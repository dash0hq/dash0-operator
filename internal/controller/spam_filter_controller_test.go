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
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	spamFilterName            = "test-spam-filter"
	extraNamespaceSpamFilters = "extra-namespace-spam-filters"
	spamFilterName2           = "test-spam-filter-2"
	spamFilterApiBasePath     = "/api/spam-filters/"

	spamFilterId                       = "spam-filter-id"
	spamFilterOriginPattern            = "dash0-operator_%s_test-dataset_test-namespace_test-spam-filter"
	spamFilterOriginPatternExtra       = "dash0-operator_%s_test-dataset_extra-namespace-spam-filters_test-spam-filter-2"
	spamFilterOriginPatternAlternative = "dash0-operator_%s_test-dataset-alt_test-namespace_test-spam-filter"
)

var (
	defaultExpectedPathSpamFilter = fmt.Sprintf(
		"%s.*%s",
		spamFilterApiBasePath,
		"dash0-operator_.*_test-dataset_test-namespace_test-spam-filter",
	)
	expectedPathSpamFilterAlternative = fmt.Sprintf(
		"%s.*%s",
		spamFilterApiBasePath,
		"dash0-operator_.*_test-dataset-alt_test-namespace_test-spam-filter",
	)
	defaultExpectedPathSpamFilter2 = fmt.Sprintf(
		"%s.*%s",
		spamFilterApiBasePath,
		"dash0-operator_.*_test-dataset_extra-namespace-spam-filters_test-spam-filter-2",
	)
	spamFilterLeaderElectionAware = NewLeaderElectionAwareMock(true)
)

var _ = Describe(
	"The SpamFilter controller", Ordered, func() {
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
				clusterId = string(util.ReadPseudoClusterUid(ctx, k8sClient, logger))
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
			"the spam filter reconciler", func() {
				var spamFilterReconciler *SpamFilterReconciler

				BeforeEach(
					func() {
						spamFilterReconciler = createSpamFilterReconciler(clusterId)

						// Set default API configs directly (not via SetDefaultApiConfigs) to avoid
						// triggering maybeDoInitialSynchronizationOfAllResources, which would set
						// initialSyncHasHappend to true and prevent the DescribeTable tests from
						// verifying initial sync behavior.
						spamFilterReconciler.defaultApiConfigs.Set(
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
						deleteSpamFilterResourceIfItExists(ctx, k8sClient, TestNamespaceName, spamFilterName)
						deleteSpamFilterResourceIfItExists(ctx, k8sClient, extraNamespaceSpamFilters, spamFilterName2)
					},
				)

				It(
					"it ignores spam filter resource changes if no Dash0 monitoring resource exists in the namespace", func() {
						expectSpamFilterPutRequest(clusterId, defaultExpectedPathSpamFilter)
						defer gock.Off()

						spamFilterResource := createSpamFilterResource(TestNamespaceName, spamFilterName)
						Expect(k8sClient.Create(ctx, spamFilterResource)).To(Succeed())

						Expect(gock.IsPending()).To(BeTrue())
						verifySpamFilterHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							spamFilterName,
						)
					},
				)

				It(
					"it ignores spam filter resource changes if the API endpoint is not configured", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSpamFilterPutRequest(clusterId, defaultExpectedPathSpamFilter)
						defer gock.Off()

						spamFilterReconciler.RemoveDefaultApiConfigs(ctx, logger)

						spamFilterResource := createSpamFilterResource(TestNamespaceName, spamFilterName)
						Expect(k8sClient.Create(ctx, spamFilterResource)).To(Succeed())
						result, err := spamFilterReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      spamFilterName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsPending()).To(BeTrue())
						verifySpamFilterHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							spamFilterName,
						)
					},
				)

				It(
					"creates a spam filter", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSpamFilterPutRequest(clusterId, defaultExpectedPathSpamFilter)
						defer gock.Off()

						spamFilterResource := createSpamFilterResource(TestNamespaceName, spamFilterName)
						Expect(k8sClient.Create(ctx, spamFilterResource)).To(Succeed())

						result, err := spamFilterReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      spamFilterName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySpamFilterSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							spamFilterName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							spamFilterId,
							fmt.Sprintf(spamFilterOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates a spam filter with namespaced config from the monitoring resource", func() {
						monitoringResource := DefaultMonitoringResourceWithCustomApiConfigAndToken(
							MonitoringResourceQualifiedName,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
						)
						EnsureMonitoringResourceWithSpecExistsAndIsAvailable(ctx, k8sClient, monitoringResource.Spec)

						expectSpamFilterPutRequestCustom(
							clusterId,
							expectedPathSpamFilterAlternative,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
							spamFilterOriginPatternAlternative,
							1,
						)
						defer gock.Off()

						spamFilterResource := createSpamFilterResource(TestNamespaceName, spamFilterName)
						Expect(k8sClient.Create(ctx, spamFilterResource)).To(Succeed())

						spamFilterReconciler.SetNamespacedApiConfigs(
							ctx, TestNamespaceName, []ApiConfig{
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, logger,
						)

						// note: we don't trigger reconcile here because setting the API config and token already triggers a reconciliation

						verifySpamFilterSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							spamFilterName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							spamFilterId,
							fmt.Sprintf(spamFilterOriginPatternAlternative, clusterId),
							ApiEndpointStandardizedTestAlternative,
							DatasetCustomTestAlternative,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates a spam filter", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSpamFilterPutRequest(clusterId, defaultExpectedPathSpamFilter)
						defer gock.Off()

						spamFilterResource := createSpamFilterResource(TestNamespaceName, spamFilterName)
						Expect(k8sClient.Create(ctx, spamFilterResource)).To(Succeed())

						// Modify the resource
						spamFilterResource.Spec.Contexts = []string{"log", "span"}
						Expect(k8sClient.Update(ctx, spamFilterResource)).To(Succeed())

						result, err := spamFilterReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      spamFilterName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySpamFilterSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							spamFilterName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							spamFilterId,
							fmt.Sprintf(spamFilterOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a spam filter", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSpamFilterDeleteRequest(defaultExpectedPathSpamFilter)
						defer gock.Off()

						spamFilterResource := createSpamFilterResource(TestNamespaceName, spamFilterName)
						Expect(k8sClient.Create(ctx, spamFilterResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, spamFilterResource)).To(Succeed())

						result, err := spamFilterReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      spamFilterName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						// We do not call verifySpamFilterSynchronizationStatus in this test case since the entire
						// spam filter resource is deleted, hence there is nothing to write the status to.
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a spam filter if labelled with dash0.com/enable=false", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSpamFilterDeleteRequestWithHttpStatus(defaultExpectedPathSpamFilter, http.StatusNotFound)
						defer gock.Off()

						spamFilterResource := createSpamFilterResourceWithEnableLabel(TestNamespaceName, spamFilterName, "false")
						Expect(k8sClient.Create(ctx, spamFilterResource)).To(Succeed())

						result, err := spamFilterReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      spamFilterName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySpamFilterSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							spamFilterName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							"", // when deleting an object, we do not get an HTTP response body with an ID
							fmt.Sprintf(spamFilterOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports http errors when synchronizing a spam filter", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathSpamFilter).
							MatchParam("dataset", DatasetCustomTest).
							Times(3).
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						spamFilterResource := createSpamFilterResource(TestNamespaceName, spamFilterName)
						Expect(k8sClient.Create(ctx, spamFilterResource)).To(Succeed())

						result, err := spamFilterReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      spamFilterName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySpamFilterSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							spamFilterName,
							dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							testStartedAt,
							"",
							"",
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"unexpected status code 503 when trying to synchronize the spam-filter \"test-spam-filter\": "+
								"PUT https://api.dash0.com/api/spam-filters/"+
								"dash0-operator_"+clusterId+
								"_test-dataset_test-namespace_test-spam-filter?dataset=test-dataset, response body is {}",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				type maybeDoInitialSynchronizationOfAllResourcesTest struct {
					disableSync func()
					enabledSync func()
				}

				DescribeTable(
					"synchronizes all existing spam filter resources when the auth token or api endpoint become available",
					func(testConfig maybeDoInitialSynchronizationOfAllResourcesTest) {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// Disable synchronization by removing the auth token or api endpoint.
						testConfig.disableSync()

						EnsureNamespaceExists(ctx, k8sClient, extraNamespaceSpamFilters)
						secondMonitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
							ctx,
							k8sClient,
							MonitoringResourceDefaultSpecWithoutExport,
							types.NamespacedName{Namespace: extraNamespaceSpamFilters, Name: MonitoringResourceName},
						)
						extraMonitoringResourceNames = append(
							extraMonitoringResourceNames, types.NamespacedName{
								Namespace: secondMonitoringResource.Namespace,
								Name:      secondMonitoringResource.Name,
							},
						)

						expectSpamFilterPutRequest(clusterId, defaultExpectedPathSpamFilter)
						expectSpamFilterPutRequest(clusterId, defaultExpectedPathSpamFilter2)
						defer gock.Off()

						spamFilterResource1 := createSpamFilterResource(TestNamespaceName, spamFilterName)
						Expect(k8sClient.Create(ctx, spamFilterResource1)).To(Succeed())
						spamFilterResource2 := createSpamFilterResource(extraNamespaceSpamFilters, spamFilterName2)
						Expect(k8sClient.Create(ctx, spamFilterResource2)).To(Succeed())

						// verify that the spam filters have not been synchronized yet
						Expect(gock.IsPending()).To(BeTrue())
						verifySpamFilterHasNoSynchronizationStatus(ctx, k8sClient, TestNamespaceName, spamFilterName)
						verifySpamFilterHasNoSynchronizationStatus(ctx, k8sClient, extraNamespaceSpamFilters, spamFilterName2)

						// Now provide the auth token or API endpoint, which was unset before. This should trigger initial
						// synchronization of all resources.
						testConfig.enabledSync()

						// Verify both spam filter resources have been synchronized
						verifySpamFilterSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							spamFilterName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							spamFilterId,
							fmt.Sprintf(spamFilterOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						verifySpamFilterSynchronizationStatus(
							ctx,
							k8sClient,
							extraNamespaceSpamFilters,
							spamFilterName2,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							spamFilterId,
							fmt.Sprintf(spamFilterOriginPatternExtra, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
					Entry(
						"when the auth token becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								spamFilterReconciler.RemoveDefaultApiConfigs(ctx, logger)
							},
							enabledSync: func() {
								spamFilterReconciler.SetDefaultApiConfigs(
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
								spamFilterLeaderElectionAware.SetLeader(false)
							},
							enabledSync: func() {
								spamFilterLeaderElectionAware.SetLeader(true)
								spamFilterReconciler.NotifyOperatorManagerJustBecameLeader(ctx, logger)
							},
						},
					),
				)
			},
		)

		Describe(
			"mapping spam filter resources to http requests", func() {

				type spamFilterToRequestTestConfig struct {
					spamFilter          string
					expectedAnnotations map[string]string
				}

				var spamFilterReconciler *SpamFilterReconciler

				BeforeEach(
					func() {
						spamFilterReconciler = &SpamFilterReconciler{}
					},
				)

				DescribeTable(
					"maps spam filters", func(testConfig spamFilterToRequestTestConfig) {
						spamFilter := map[string]any{}
						Expect(yaml.Unmarshal([]byte(testConfig.spamFilter), &spamFilter)).To(Succeed())
						apiConfig := ApiConfig{
							Endpoint: ApiEndpointTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						}
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:      "dash0-spam-filter",
							k8sNamespace: TestNamespaceName,
							resource:     spamFilter,
							validatedApiConfigs: []ValidatedApiConfigAndToken{
								*NewValidatedApiConfigAndToken(apiConfig.Endpoint, apiConfig.Dataset, apiConfig.Token),
							},
						}
						resourceToRequestsResult :=
							spamFilterReconciler.MapResourceToHttpRequests(preconditionValidationResult, apiConfig, upsertAction, logger)
						Expect(resourceToRequestsResult.OriginsInResource).To(BeNil())
						Expect(resourceToRequestsResult.ValidationIssues).To(BeNil())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())

						Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(1))
						apiRequest := resourceToRequestsResult.ApiRequests[0]
						Expect(apiRequest.ItemName).To(Equal("dash0-spam-filter"))
						req := apiRequest.Request
						defer func() {
							_ = req.Body.Close()
						}()
						body, err := io.ReadAll(req.Body)
						Expect(err).ToNot(HaveOccurred())
						resultingSpamFilterInRequest := map[string]any{}
						Expect(json.Unmarshal(body, &resultingSpamFilterInRequest)).To(Succeed())
						Expect(resultingSpamFilterInRequest["spec"]).ToNot(BeNil())

						Expect(resultingSpamFilterInRequest["metadata"]).ToNot(BeNil())
						Expect(ReadFromMap(resultingSpamFilterInRequest, []string{"metadata", "name"})).To(Equal("dash0-spam-filter"))

						if testConfig.expectedAnnotations != nil {
							annotationsRaw := ReadFromMap(resultingSpamFilterInRequest, []string{"metadata", "annotations"})
							Expect(annotationsRaw).ToNot(BeNil())
							annotations := annotationsRaw.(map[string]any)
							Expect(annotations).To(HaveLen(len(testConfig.expectedAnnotations)))
							for expectedKey, expectedValue := range testConfig.expectedAnnotations {
								value, ok := annotations[expectedKey]
								Expect(ok).To(BeTrue())
								Expect(value).To(Equal(expectedValue))
							}
						} else {
							Expect(ReadFromMap(resultingSpamFilterInRequest, []string{"metadata", "annotations"})).To(BeNil())
						}
					},
					Entry(
						"should map spam filter", spamFilterToRequestTestConfig{
							spamFilter: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SpamFilter
metadata:
  name: dash0-spam-filter
spec:
  contexts:
    - log
  filter:
    - key: k8s.namespace.name
      operator: is
      value: kube-system
`,
						},
					),
					Entry(
						"should send annotations", spamFilterToRequestTestConfig{
							spamFilter: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SpamFilter
metadata:
  name: dash0-spam-filter
  annotations:
    dash0com/annotation1: value1
    dash0com/annotation2: value2
spec:
  contexts:
    - log
  filter:
    - key: k8s.namespace.name
      operator: is
      value: kube-system
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

func createSpamFilterReconciler(clusterId string) *SpamFilterReconciler {
	spamFilterReconciler := NewSpamFilterReconciler(
		k8sClient,
		types.UID(clusterId),
		spamFilterLeaderElectionAware,
		TestHTTPClient(),
	)
	return spamFilterReconciler
}

func expectSpamFilterPutRequestCustom(
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
		JSON(spamFilterPutResponse(clusterId, originPattern, dataset))
}

func expectSpamFilterPutRequest(clusterId string, expectedPath string) {
	expectSpamFilterPutRequestCustom(
		clusterId,
		expectedPath,
		ApiEndpointTest,
		DatasetCustomTest,
		AuthorizationHeaderTest,
		spamFilterOriginPattern,
		1,
	)
}

func spamFilterPutResponse(clusterId string, originPattern string, dataset string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"dash0.com/id":      spamFilterId,
				"dash0.com/origin":  fmt.Sprintf(originPattern, clusterId),
				"dash0.com/dataset": dataset,
			},
		},
	}
}

func expectSpamFilterDeleteRequest(expectedPath string) {
	expectSpamFilterDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectSpamFilterDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status)
}

func createSpamFilterResource(namespace string, name string) *dash0v1alpha1.Dash0SpamFilter {
	return createSpamFilterResourceWithEnableLabel(namespace, name, "")
}

func createSpamFilterResourceWithEnableLabel(
	namespace string,
	name string,
	dash0EnableLabelValue string,
) *dash0v1alpha1.Dash0SpamFilter {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1alpha1.Dash0SpamFilter{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0SpamFilter",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1alpha1.Dash0SpamFilterSpec{
			Contexts: []string{"log"},
			Filter: []dash0v1alpha1.Dash0SpamFilterCondition{
				{
					Key:      "k8s.namespace.name",
					Operator: "is",
					Value:    ptr.To("kube-system"),
				},
			},
		},
	}
}

func deleteSpamFilterResourceIfItExists(ctx context.Context, k8sClient client.Client, namespace string, name string) {
	spamFilter := &dash0v1alpha1.Dash0SpamFilter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(
		ctx, spamFilter, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifySpamFilterSynchronizationStatus(
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
			spamFilter := &dash0v1alpha1.Dash0SpamFilter{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, spamFilter,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(spamFilter.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(spamFilter.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(spamFilter.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			// spam filters have no operator-side validations, all local validations are encoded in the CRD already
			g.Expect(spamFilter.Status.ValidationIssues).To(BeNil())

			g.Expect(spamFilter.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpointAndDataset := spamFilter.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpointAndDataset).ToNot(BeNil())
			g.Expect(syncResultPerEndpointAndDataset.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Dataset).To(Equal(expectedDataset))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(syncResultPerEndpointAndDataset.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}

func verifySpamFilterHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	Eventually(
		func(g Gomega) {
			spamFilter := &dash0v1alpha1.Dash0SpamFilter{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, spamFilter,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(spamFilter.Status.SynchronizationStatus)).To(Equal(""))
			g.Expect(spamFilter.Status.ValidationIssues).To(BeNil())
			g.Expect(spamFilter.Status.SynchronizationResults).To(HaveLen(0))
		},
	).Should(Succeed())
}
