// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	viewName            = "test-view"
	extraNamespaceViews = "extra-namespace-views"
	viewName2           = "test-view-2"
	viewApiBasePath     = "/api/views/"

	viewId                       = "view-id"
	viewOriginPattern            = "dash0-operator_%s_test-dataset_test-namespace_test-view"
	viewOriginPatternExtra       = "dash0-operator_%s_test-dataset_extra-namespace-views_test-view-2"
	viewOriginPatternAlternative = "dash0-operator_%s_test-dataset-alt_test-namespace_test-view"
)

var (
	defaultExpectedPathView = fmt.Sprintf(
		"%s.*%s",
		viewApiBasePath,
		"dash0-operator_.*_test-dataset_test-namespace_test-view",
	)
	expectedPathViewAlternative = fmt.Sprintf(
		"%s.*%s",
		viewApiBasePath,
		"dash0-operator_.*_test-dataset-alt_test-namespace_test-view",
	)
	defaultExpectedPathView2 = fmt.Sprintf(
		"%s.*%s",
		viewApiBasePath,
		"dash0-operator_.*_test-dataset_extra-namespace-views_test-view-2",
	)
	viewLeaderElectionAware = NewLeaderElectionAwareMock(true)
)

var _ = Describe(
	"The View controller", Ordered, func() {
		ctx := context.Background()
		logger := log.FromContext(ctx)
		var testStartedAt time.Time
		var clusterId string

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
			"the view reconciler", func() {
				var viewReconciler *ViewReconciler

				BeforeEach(
					func() {
						viewReconciler = createViewReconciler(clusterId)

						// Set default API configs directly (not via SetDefaultApiConfigs) to avoid
						// triggering maybeDoInitialSynchronizationOfAllResources, which would set
						// initialSyncHasHappend to true and prevent the DescribeTable tests from
						// verifying initial sync behavior.
						viewReconciler.defaultApiConfigs.Set(
							[]ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							},
						)

						// to make tests that involve http retries faster, we do not want to wait for one second for each retry
						viewReconciler.overrideHttpRetryDelay(20 * time.Millisecond)
					},
				)

				AfterEach(
					func() {
						DeleteMonitoringResourceIfItExists(ctx, k8sClient)
						deleteViewResourceIfItExists(ctx, k8sClient, TestNamespaceName, viewName)
						deleteViewResourceIfItExists(ctx, k8sClient, extraNamespaceViews, viewName2)
					},
				)

				It(
					"it ignores view resource changes if no Dash0 monitoring resource exists in the namespace", func() {
						expectViewPutRequest(clusterId, defaultExpectedPathView)
						defer gock.Off()

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						Expect(gock.IsPending()).To(BeTrue())
						verifyViewHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
						)
					},
				)

				It(
					"it ignores view resource changes if the API endpoint is not configured", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectViewPutRequest(clusterId, defaultExpectedPathView)
						defer gock.Off()

						viewReconciler.RemoveDefaultApiConfigs(ctx, logger)

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())
						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsPending()).To(BeTrue())
						verifyViewHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
						)
					},
				)

				It(
					"it ignores view resource changes if the auth token has not been provided yet", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectViewPutRequest(clusterId, defaultExpectedPathView)
						defer gock.Off()

						// Remove API configs which also removes the auth tokens (since tokens are now part of ApiConfig)
						viewReconciler.RemoveDefaultApiConfigs(ctx, logger)

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())
						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsPending()).To(BeTrue())
						verifyViewHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
						)
					},
				)

				It(
					"creates a view", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectViewPutRequest(clusterId, defaultExpectedPathView)
						defer gock.Off()

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyViewSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							viewId,
							fmt.Sprintf(viewOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates a view with namespaced config from the monitoring resource", func() {
						monitoringResource := DefaultMonitoringResourceWithCustomApiConfigAndToken(
							MonitoringResourceQualifiedName,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
						)
						EnsureMonitoringResourceWithSpecExistsAndIsAvailable(ctx, k8sClient, monitoringResource.Spec)

						expectViewPutRequestCustom(
							clusterId,
							expectedPathViewAlternative,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
							viewOriginPatternAlternative,
							1,
						)
						defer gock.Off()

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						viewReconciler.SetNamespacedApiConfigs(
							ctx, TestNamespaceName, []ApiConfig{
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, logger,
						)

						// note: we don't trigger reconcile here because setting the API config and token already triggers a reconciliation

						verifyViewSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							viewId,
							fmt.Sprintf(viewOriginPatternAlternative, clusterId),
							ApiEndpointStandardizedTestAlternative,
							DatasetCustomTestAlternative,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates a view with two API configs (multi-export)", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectViewPutRequest(clusterId, defaultExpectedPathView)
						expectViewPutRequestCustom(
							clusterId,
							expectedPathViewAlternative,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
							viewOriginPatternAlternative,
							1,
						)
						defer gock.Off()

						viewReconciler.defaultApiConfigs.Set(
							[]ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							},
						)

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyViewMultiExportSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							[]expectedViewSyncResult{
								{
									id:          viewId,
									origin:      fmt.Sprintf(viewOriginPattern, clusterId),
									apiEndpoint: ApiEndpointStandardizedTest,
									dataset:     DatasetCustomTest,
									syncError:   "",
								},
								{
									id:          viewId,
									origin:      fmt.Sprintf(viewOriginPatternAlternative, clusterId),
									apiEndpoint: ApiEndpointStandardizedTestAlternative,
									dataset:     DatasetCustomTestAlternative,
									syncError:   "",
								},
							},
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates a view with two API configs where one fails (partially-successful)", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// First API config succeeds
						expectViewPutRequest(clusterId, defaultExpectedPathView)

						// Second API config fails
						gock.New(ApiEndpointTestAlternative).
							Put(expectedPathViewAlternative).
							MatchParam("dataset", DatasetCustomTestAlternative).
							Times(3). // 3 retries
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						viewReconciler.defaultApiConfigs.Set(
							[]ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							},
						)

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Eventually(
							func(g Gomega) {
								view := &dash0v1alpha1.Dash0View{}
								err := k8sClient.Get(
									ctx, types.NamespacedName{
										Namespace: TestNamespaceName,
										Name:      viewName,
									}, view,
								)
								g.Expect(err).NotTo(HaveOccurred())
								g.Expect(view.Status.SynchronizationStatus).To(Equal(
									dash0common.Dash0ApiResourceSynchronizationStatusPartiallySuccessful,
								))
								g.Expect(view.Status.SynchronizationResults).To(HaveLen(2))
							},
						).Should(Succeed())
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a view with two API configs (multi-export)", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectViewDeleteRequest(defaultExpectedPathView)
						expectViewDeleteRequestCustom(
							expectedPathViewAlternative,
							ApiEndpointTestAlternative,
							AuthorizationTokenTestAlternative,
							DatasetCustomTestAlternative,
							http.StatusOK,
						)
						defer gock.Off()

						viewReconciler.defaultApiConfigs.Set(
							[]ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							},
						)

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates a view", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectViewPutRequest(clusterId, defaultExpectedPathView)
						defer gock.Off()

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						// Modify the resource
						viewResource.Spec.Display.Name = "Updated View"
						Expect(k8sClient.Update(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyViewSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							viewId,
							fmt.Sprintf(viewOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a view", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectViewDeleteRequest(defaultExpectedPathView)
						defer gock.Off()

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						// We do not call verifyViewSynchronizationStatus in this test case since the entire view resource is
						// deleted, hence there is nothing to write the status to.
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a view if labelled with dash0.com/enable=false", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectViewDeleteRequestWithHttpStatus(defaultExpectedPathView, http.StatusNotFound)
						defer gock.Off()

						viewResource := createViewResourceWithEnableLabel(TestNamespaceName, viewName, "false")
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyViewSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							"", // when deleting an object, we do not get an HTTP response body with an ID
							fmt.Sprintf(viewOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates a view if dash0.com/enable is set but not to \"false\"", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectViewPutRequest(clusterId, defaultExpectedPathView)
						defer gock.Off()

						viewResource := createViewResourceWithEnableLabel(TestNamespaceName, viewName, "whatever")
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyViewSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							viewId,
							fmt.Sprintf(viewOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports http errors when synchronizing a view", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathView).
							MatchParam("dataset", DatasetCustomTest).
							Times(3).
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyViewSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
							dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							testStartedAt,
							"",
							"",
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"unexpected status code 503 when trying to synchronize the view \"test-view\": "+
								"PUT https://api.dash0.com/api/views/"+
								"dash0-operator_"+clusterId+
								"_test-dataset_test-namespace_test-view?dataset=test-dataset, response body is {}",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"retries synchronization when synchronizing a view", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathView).
							MatchParam("dataset", DatasetCustomTest).
							Times(2).
							Reply(503).
							JSON(map[string]interface{}{})
						expectViewPutRequest(clusterId, defaultExpectedPathView)
						defer gock.Off()

						viewResource := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

						result, err := viewReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      viewName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifyViewSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							viewId,
							fmt.Sprintf(viewOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				type maybeDoInitialSynchronizationOfAllResourcesTest struct {
					disableSync func()
					enabledSync func()
				}

				DescribeTable(
					"synchronizes all existing view resources when the auth token or api endpoint become available",
					func(testConfig maybeDoInitialSynchronizationOfAllResourcesTest) {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// Disable synchronization by removing the auth token or api endpoint.
						testConfig.disableSync()

						EnsureNamespaceExists(ctx, k8sClient, extraNamespaceViews)
						secondMonitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
							ctx,
							k8sClient,
							MonitoringResourceDefaultSpecWithoutExport,
							types.NamespacedName{Namespace: extraNamespaceViews, Name: MonitoringResourceName},
						)
						extraMonitoringResourceNames = append(
							extraMonitoringResourceNames, types.NamespacedName{
								Namespace: secondMonitoringResource.Namespace,
								Name:      secondMonitoringResource.Name,
							},
						)

						expectViewPutRequest(clusterId, defaultExpectedPathView)
						expectViewPutRequest(clusterId, defaultExpectedPathView2)
						defer gock.Off()

						viewResource1 := createViewResource(TestNamespaceName, viewName)
						Expect(k8sClient.Create(ctx, viewResource1)).To(Succeed())
						viewResource2 := createViewResource(extraNamespaceViews, viewName2)
						Expect(k8sClient.Create(ctx, viewResource2)).To(Succeed())

						// verify that the views have not been synchronized yet
						Expect(gock.IsPending()).To(BeTrue())
						verifyViewHasNoSynchronizationStatus(ctx, k8sClient, TestNamespaceName, viewName)
						verifyViewHasNoSynchronizationStatus(ctx, k8sClient, extraNamespaceViews, viewName2)

						// Now provide the auth token or API endpoint, which was unset before. This should trigger initial
						// synchronization of all resources.
						testConfig.enabledSync()

						// Verify both view resources have been synchronized
						verifyViewSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							viewName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							viewId,
							fmt.Sprintf(viewOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						verifyViewSynchronizationStatus(
							ctx,
							k8sClient,
							extraNamespaceViews,
							viewName2,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							viewId,
							fmt.Sprintf(viewOriginPatternExtra, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
					Entry(
						"when the auth token becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								viewReconciler.RemoveDefaultApiConfigs(ctx, logger)
							},
							enabledSync: func() {
								viewReconciler.SetDefaultApiConfigs(
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
						"when the api endpoint becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								viewReconciler.RemoveDefaultApiConfigs(ctx, logger)
							},
							enabledSync: func() {
								viewReconciler.SetDefaultApiConfigs(
									ctx,
									[]ApiConfig{
										{
											Endpoint: ApiEndpointTest,
											Dataset:  DatasetCustomTest,
											Token:    AuthorizationTokenTest,
										},
									},
									logger,
								)
							},
						},
					),
					Entry(
						"when the operator manager becomes leader", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								viewLeaderElectionAware.SetLeader(false)
							},
							enabledSync: func() {
								viewLeaderElectionAware.SetLeader(true)
								viewReconciler.NotifiyOperatorManagerJustBecameLeader(ctx, logger)
							},
						},
					),
					Entry(
						"only sync once even if the the auth token is set multiple times",
						maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								viewReconciler.RemoveDefaultApiConfigs(ctx, logger)
							},
							enabledSync: func() {
								// gock only expects two PUT requests, so if we would synchronize twice, the test would fail
								viewReconciler.SetDefaultApiConfigs(
									ctx, []ApiConfig{
										{
											Endpoint: ApiEndpointTest,
											Dataset:  DatasetCustomTest,
											Token:    AuthorizationTokenTest,
										},
									}, logger,
								)
								viewReconciler.SetDefaultApiConfigs(
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
				)
			},
		)

		Describe(
			"mapping view resources to http requests", func() {

				type viewToRequestTestConfig struct {
					view                string
					expectedAnnotations map[string]string
				}

				var viewReconciler *ViewReconciler

				BeforeEach(
					func() {
						viewReconciler = &ViewReconciler{}
					},
				)

				DescribeTable(
					"maps views", func(testConfig viewToRequestTestConfig) {
						view := map[string]interface{}{}
						Expect(yaml.Unmarshal([]byte(testConfig.view), &view)).To(Succeed())
						apiConfig := ApiConfig{
							Endpoint: ApiEndpointTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						}
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:      "dash0-view",
							k8sNamespace: TestNamespaceName,
							resource:     view,
							validatedApiConfigs: []ValidatedApiConfigAndToken{
								*NewValidatedApiConfigAndToken(apiConfig.Endpoint, apiConfig.Dataset, apiConfig.Token),
							},
						}
						resourceToRequestsResult :=
							viewReconciler.MapResourceToHttpRequests(preconditionValidationResult, apiConfig, upsertAction, logger)
						Expect(resourceToRequestsResult.ItemsTotal).To(Equal(1))
						Expect(resourceToRequestsResult.OriginsInResource).To(BeNil())
						Expect(resourceToRequestsResult.ValidationIssues).To(BeNil())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())

						Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(1))
						apiRequest := resourceToRequestsResult.ApiRequests[0]
						Expect(apiRequest.ItemName).To(Equal("dash0-view"))
						req := apiRequest.Request
						defer func() {
							_ = req.Body.Close()
						}()
						body, err := io.ReadAll(req.Body)
						Expect(err).ToNot(HaveOccurred())
						resultingViewInRequest := map[string]interface{}{}
						Expect(json.Unmarshal(body, &resultingViewInRequest)).To(Succeed())
						Expect(resultingViewInRequest["spec"]).ToNot(BeNil())

						Expect(resultingViewInRequest["metadata"]).ToNot(BeNil())
						Expect(ReadFromMap(resultingViewInRequest, []string{"metadata", "name"})).To(Equal("dash0-view"))

						if testConfig.expectedAnnotations != nil {
							annotationsRaw := ReadFromMap(resultingViewInRequest, []string{"metadata", "annotations"})
							Expect(annotationsRaw).ToNot(BeNil())
							annotations := annotationsRaw.(map[string]interface{})
							Expect(annotations).To(HaveLen(len(testConfig.expectedAnnotations)))
							for expectedKey, expectedValue := range testConfig.expectedAnnotations {
								value, ok := annotations[expectedKey]
								Expect(ok).To(BeTrue())
								Expect(value).To(Equal(expectedValue))
							}
						} else {
							Expect(ReadFromMap(resultingViewInRequest, []string{"metadata", "annotations"})).To(BeNil())
						}
					},
					Entry(
						"should map view", viewToRequestTestConfig{
							view: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0View
metadata:
  name: dash0-view
spec:
  display:
    name: Dash0 View Example
    description: This is an example view.
  filter:
    - key: http.status_code
      operator: gte
      value:
        stringValue: "200"
`,
						},
					),
					Entry(
						"should send annotations", viewToRequestTestConfig{
							view: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0View
metadata:
  name: dash0-view
  annotations:
    dash0com/annotation1: value1
    dash0com/annotation2: value2
spec:
  display:
    name: Dash0 View Example
    description: This is an example view.
  filter:
    - key: http.status_code
      operator: gte
      value:
        stringValue: "200"
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

func createViewReconciler(clusterId string) *ViewReconciler {
	viewReconciler := NewViewReconciler(
		k8sClient,
		types.UID(clusterId),
		viewLeaderElectionAware,
		&http.Client{},
	)
	return viewReconciler
}

func expectViewPutRequestCustom(
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
		JSON(viewPutResponse(clusterId, originPattern, dataset))
}

func expectViewPutRequest(clusterId string, expectedPath string) {
	expectViewPutRequestCustom(
		clusterId,
		expectedPath,
		ApiEndpointTest,
		DatasetCustomTest,
		AuthorizationHeaderTest,
		viewOriginPattern,
		1,
	)
}

func viewPutResponse(clusterId string, originPattern string, dataset string) map[string]interface{} {
	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{
				"dash0.com/id":      viewId,
				"dash0.com/origin":  fmt.Sprintf(originPattern, clusterId),
				"dash0.com/dataset": dataset,
			},
		},
	}
}

func expectViewDeleteRequest(expectedPath string) {
	expectViewDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectViewDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status)
}

func createViewResource(namespace string, name string) *dash0v1alpha1.Dash0View {
	return createViewResourceWithEnableLabel(namespace, name, "")
}

func createViewResourceWithEnableLabel(
	namespace string,
	name string,
	dash0EnableLabelValue string,
) *dash0v1alpha1.Dash0View {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1alpha1.Dash0View{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0View",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1alpha1.Dash0ViewSpec{
			Type: "resources",
			Display: dash0v1alpha1.Dash0ViewDisplay{
				Name: "Test View",
			},
			Filter: []dash0v1alpha1.Dash0ViewFilter{
				{
					Key:      "service.name",
					Operator: "is",
					Value:    "test-service",
				},
			},
			Table: &dash0v1alpha1.Dash0ViewTable{
				Columns: []dash0v1alpha1.Dash0ViewTableColumn{
					{
						Key: "service.name",
					},
				},
				Sort: []dash0v1alpha1.Dash0ViewTableSort{
					{
						Key:       "service.name",
						Direction: "ascending",
					},
				},
			},
			Visualizations: []dash0v1alpha1.Dash0ViewVisualization{
				{
					Renderers: []dash0v1alpha1.Dash0ViewRenderer{"resources/services/red"},
				},
			},
		},
	}
}

func deleteViewResourceIfItExists(ctx context.Context, k8sClient client.Client, namespace string, name string) {
	view := &dash0v1alpha1.Dash0View{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(
		ctx, view, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifyViewSynchronizationStatus(
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
			view := &dash0v1alpha1.Dash0View{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, view,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(view.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(view.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(view.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			// views have no operator-side validations, all local validations are encoded in the CRD already
			g.Expect(view.Status.ValidationIssues).To(BeNil())

			g.Expect(view.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpointAndDataset := view.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpointAndDataset).ToNot(BeNil())
			g.Expect(syncResultPerEndpointAndDataset.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Dataset).To(Equal(expectedDataset))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(syncResultPerEndpointAndDataset.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}

type expectedViewSyncResult struct {
	id          string
	origin      string
	apiEndpoint string
	dataset     string
	syncError   string
}

func verifyViewMultiExportSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
	expectedStatus dash0common.Dash0ApiResourceSynchronizationStatus,
	testStartedAt time.Time,
	expectedResults []expectedViewSyncResult,
) {
	Eventually(
		func(g Gomega) {
			view := &dash0v1alpha1.Dash0View{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, view,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(view.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(view.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(view.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			g.Expect(view.Status.ValidationIssues).To(BeNil())

			g.Expect(view.Status.SynchronizationResults).To(HaveLen(len(expectedResults)))
			for i, expected := range expectedResults {
				syncResult := view.Status.SynchronizationResults[i]
				g.Expect(syncResult.Dash0ApiEndpoint).To(Equal(expected.apiEndpoint))
				g.Expect(syncResult.Dash0Dataset).To(Equal(expected.dataset))
				g.Expect(syncResult.Dash0Id).To(Equal(expected.id))
				g.Expect(syncResult.Dash0Origin).To(Equal(expected.origin))
				g.Expect(syncResult.SynchronizationError).To(ContainSubstring(expected.syncError))
			}
		},
	).Should(Succeed())
}

func expectViewDeleteRequestCustom(
	expectedPath string,
	endpoint string,
	token string,
	dataset string,
	status int,
) {
	gock.New(endpoint).
		Delete(expectedPath).
		MatchHeader("Authorization", token).
		MatchParam("dataset", dataset).
		Times(1).
		Reply(status)
}

func verifyViewHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	Eventually(
		func(g Gomega) {
			view := &dash0v1alpha1.Dash0View{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, view,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(view.Status.SynchronizationStatus)).To(Equal(""))
			g.Expect(view.Status.ValidationIssues).To(BeNil())
			g.Expect(view.Status.SynchronizationResults).To(HaveLen(0))
		},
	).Should(Succeed())
}
