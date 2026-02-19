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
	"k8s.io/utils/ptr"
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
	syntheticCheckName            = "test-synthetic-check"
	extraNamespaceSyntheticChecks = "extra-namespace-synthetic-checks"
	syntheticCheckName2           = "test-synthetic-check-2"
	syntheticCheckApiBasePath     = "/api/synthetic-checks/"

	syntheticCheckId                       = "synthetic-check-id"
	syntheticCheckOriginPattern            = "dash0-operator_%s_test-dataset_test-namespace_test-synthetic-check"
	syntheticCheckOriginPatternExtra       = "dash0-operator_%s_test-dataset_extra-namespace-synthetic-checks_test-synthetic-check-2"
	syntheticCheckOriginPatternAlternative = "dash0-operator_%s_test-dataset-alt_test-namespace_test-synthetic-check"
)

var (
	defaultExpectedPathSyntheticCheck = fmt.Sprintf(
		"%s.*%s",
		syntheticCheckApiBasePath,
		"dash0-operator_.*_test-dataset_test-namespace_test-synthetic-check",
	)
	expectedPathSyntheticCheckAlternative = fmt.Sprintf(
		"%s.*%s",
		syntheticCheckApiBasePath,
		"dash0-operator_.*_test-dataset-alt_test-namespace_test-synthetic-check",
	)
	defaultExpectedPathSyntheticCheck2 = fmt.Sprintf(
		"%s.*%s",
		syntheticCheckApiBasePath,
		"dash0-operator_.*_test-dataset_extra-namespace-synthetic-checks_test-synthetic-check-2",
	)
	leaderElectionAware = NewLeaderElectionAwareMock(true)
)

var _ = Describe(
	"The Synthetic Check controller", Ordered, func() {
		ctx := context.Background()
		logger := log.FromContext(ctx)
		var testStartedAt time.Time
		var clusterId string

		BeforeAll(
			func() {
				EnsureTestNamespaceExists(ctx, k8sClient)
				EnsureOperatorNamespaceExists(ctx, k8sClient)
				clusterId = string(util.ReadPseudoClusterUid(ctx, k8sClient, &logger))
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
			"the synthetic check reconciler", func() {
				var syntheticCheckReconciler *SyntheticCheckReconciler

				BeforeEach(
					func() {
						syntheticCheckReconciler = createSyntheticCheckReconciler(clusterId)

						// Set default API configs directly (not via SetDefaultApiConfigs) to avoid
						// triggering maybeDoInitialSynchronizationOfAllResources, which would set
						// initialSyncHasHappend to true and prevent the DescribeTable tests from
						// verifying initial sync behavior.
						syntheticCheckReconciler.defaultApiConfigs.Set(
							[]ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							},
						)

						// to make tests that involve http retries faster, we do not want to wait for one second for each retry
						syntheticCheckReconciler.overrideHttpRetryDelay(20 * time.Millisecond)
					},
				)

				AfterEach(
					func() {
						DeleteMonitoringResourceIfItExists(ctx, k8sClient)
						deleteSyntheticCheckResourceIfItExists(ctx, k8sClient, TestNamespaceName, syntheticCheckName)
						deleteSyntheticCheckResourceIfItExists(ctx, k8sClient, extraNamespaceSyntheticChecks, syntheticCheckName2)
					},
				)

				It(
					"it ignores synthetic check resource changes if no Dash0 monitoring resource exists in the namespace",
					func() {
						expectSyntheticCheckPutRequest(clusterId, defaultExpectedPathSyntheticCheck)
						defer gock.Off()

						syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

						Expect(gock.IsPending()).To(BeTrue())
						verifySyntheticCheckHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
						)
					},
				)

				It(
					"it ignores synthetic check resource changes if the API endpoint is not configured", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSyntheticCheckPutRequest(clusterId, defaultExpectedPathSyntheticCheck)
						defer gock.Off()

						syntheticCheckReconciler.RemoveDefaultApiConfigs(ctx, &logger)

						syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())
						result, err := syntheticCheckReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      syntheticCheckName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsPending()).To(BeTrue())
						verifySyntheticCheckHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
						)
					},
				)

				It(
					"it ignores synthetic check resource changes if the auth token has not been provided yet", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSyntheticCheckPutRequest(clusterId, defaultExpectedPathSyntheticCheck)
						defer gock.Off()

						// Remove API configs which also removes the auth tokens (since tokens are now part of ApiConfig)
						syntheticCheckReconciler.RemoveDefaultApiConfigs(ctx, &logger)

						syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())
						result, err := syntheticCheckReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      syntheticCheckName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						Expect(gock.IsPending()).To(BeTrue())
						verifySyntheticCheckHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
						)
					},
				)

				It(
					"creates a synthetic check", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSyntheticCheckPutRequest(clusterId, defaultExpectedPathSyntheticCheck)
						defer gock.Off()

						syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

						result, err := syntheticCheckReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      syntheticCheckName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySyntheticCheckSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							syntheticCheckId,
							fmt.Sprintf(syntheticCheckOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates a synthetic check with namespaced config from the monitoring resource", func() {
						monitoringResource := DefaultMonitoringResourceWithCustomApiConfigAndToken(
							MonitoringResourceQualifiedName,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
						)
						EnsureMonitoringResourceWithSpecExistsAndIsAvailable(ctx, k8sClient, monitoringResource.Spec)

						expectSyntheticCheckPutRequestCustom(
							clusterId,
							expectedPathSyntheticCheckAlternative,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
							syntheticCheckOriginPatternAlternative,
							1,
						)
						defer gock.Off()

						syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

						syntheticCheckReconciler.SetNamespacedApiConfigs(
							ctx, TestNamespaceName, []ApiConfig{
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, &logger,
						)

						// note: we don't trigger reconcile here because setting the API config and token already triggers a reconciliation

						verifySyntheticCheckSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							syntheticCheckId,
							fmt.Sprintf(syntheticCheckOriginPatternAlternative, clusterId),
							ApiEndpointStandardizedTestAlternative,
							DatasetCustomTestAlternative,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"updates a synthetic check", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSyntheticCheckPutRequest(clusterId, defaultExpectedPathSyntheticCheck)
						defer gock.Off()

						syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

						// Modify the resource
						syntheticCheckResource.Spec.Display.Name = "Updated Synthetic Check"
						Expect(k8sClient.Update(ctx, syntheticCheckResource)).To(Succeed())

						result, err := syntheticCheckReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      syntheticCheckName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySyntheticCheckSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							syntheticCheckId,
							fmt.Sprintf(syntheticCheckOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes a synthetic check", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSyntheticCheckDeleteRequest(defaultExpectedPathSyntheticCheck)
						defer gock.Off()

						syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())
						Expect(k8sClient.Delete(ctx, syntheticCheckResource)).To(Succeed())

						result, err := syntheticCheckReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      syntheticCheckName,
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
					"deletes a synthetic check if labelled with dash0.com/enable=false", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSyntheticCheckDeleteRequestWithHttpStatus(defaultExpectedPathSyntheticCheck, http.StatusNotFound)
						defer gock.Off()

						syntheticCheckResource :=
							createSyntheticCheckResourceWithEnableLabel(TestNamespaceName, syntheticCheckName, "false")
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

						result, err := syntheticCheckReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      syntheticCheckName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySyntheticCheckSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							"", // when deleting an object, we do not get an HTTP response body with an ID
							fmt.Sprintf(syntheticCheckOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates a synthetic check if dash0.com/enable is set but not to \"false\"", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectSyntheticCheckPutRequest(clusterId, defaultExpectedPathSyntheticCheck)
						defer gock.Off()

						syntheticCheckResource :=
							createSyntheticCheckResourceWithEnableLabel(TestNamespaceName, syntheticCheckName, "whatever")
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

						result, err := syntheticCheckReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      syntheticCheckName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySyntheticCheckSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							syntheticCheckId,
							fmt.Sprintf(syntheticCheckOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports http errors when synchronizing a synthetic check", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathSyntheticCheck).
							MatchParam("dataset", DatasetCustomTest).
							Times(3).
							Reply(503).
							JSON(map[string]string{})
						defer gock.Off()

						syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

						result, err := syntheticCheckReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      syntheticCheckName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySyntheticCheckSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
							dash0common.Dash0ApiResourceSynchronizationStatusFailed,
							testStartedAt,
							"",
							"",
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"unexpected status code 503 when trying to synchronize the check \"test-synthetic-check\": "+
								"PUT https://api.dash0.com/api/synthetic-checks/"+
								"dash0-operator_"+clusterId+
								"_test-dataset_test-namespace_test-synthetic-check?dataset=test-dataset, response body is {}",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"retries synchronization when synchronizing a synthetic check", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						gock.New(ApiEndpointTest).
							Put(defaultExpectedPathSyntheticCheck).
							MatchParam("dataset", DatasetCustomTest).
							Times(2).
							Reply(503).
							JSON(map[string]string{})
						expectSyntheticCheckPutRequest(clusterId, defaultExpectedPathSyntheticCheck)
						defer gock.Off()

						syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

						result, err := syntheticCheckReconciler.Reconcile(
							ctx, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Namespace: TestNamespaceName,
									Name:      syntheticCheckName,
								},
							},
						)
						Expect(err).NotTo(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						verifySyntheticCheckSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							syntheticCheckId,
							fmt.Sprintf(syntheticCheckOriginPattern, clusterId),
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
					"synchronizes all existing synthetic check resources when the auth token or api endpoint become available",
					func(testConfig maybeDoInitialSynchronizationOfAllResourcesTest) {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// Disable synchronization by removing the auth token or api endpoint.
						testConfig.disableSync()

						EnsureNamespaceExists(ctx, k8sClient, extraNamespaceSyntheticChecks)
						secondMonitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
							ctx,
							k8sClient,
							MonitoringResourceDefaultSpecWithoutExport,
							types.NamespacedName{Namespace: extraNamespaceSyntheticChecks, Name: MonitoringResourceName},
						)
						extraMonitoringResourceNames = append(
							extraMonitoringResourceNames, types.NamespacedName{
								Namespace: secondMonitoringResource.Namespace,
								Name:      secondMonitoringResource.Name,
							},
						)

						expectSyntheticCheckPutRequest(clusterId, defaultExpectedPathSyntheticCheck)
						expectSyntheticCheckPutRequest(clusterId, defaultExpectedPathSyntheticCheck2)
						defer gock.Off()

						syntheticCheckResource1 := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
						Expect(k8sClient.Create(ctx, syntheticCheckResource1)).To(Succeed())
						syntheticCheckResource2 := createSyntheticCheckResource(extraNamespaceSyntheticChecks, syntheticCheckName2)
						Expect(k8sClient.Create(ctx, syntheticCheckResource2)).To(Succeed())

						// verify that the synthetic checks have not been synchronized yet
						Expect(gock.IsPending()).To(BeTrue())
						verifySyntheticCheckHasNoSynchronizationStatus(ctx, k8sClient, TestNamespaceName, syntheticCheckName)
						verifySyntheticCheckHasNoSynchronizationStatus(
							ctx,
							k8sClient,
							extraNamespaceSyntheticChecks,
							syntheticCheckName2,
						)

						// Now provide the auth token or API endpoint, which was unset before. This should trigger initial
						// synchronization of all resources.
						testConfig.enabledSync()

						// Verify both synthetic check resources have been synchronized
						verifySyntheticCheckSynchronizationStatus(
							ctx,
							k8sClient,
							TestNamespaceName,
							syntheticCheckName,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							syntheticCheckId,
							fmt.Sprintf(syntheticCheckOriginPattern, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						verifySyntheticCheckSynchronizationStatus(
							ctx,
							k8sClient,
							extraNamespaceSyntheticChecks,
							syntheticCheckName2,
							dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
							testStartedAt,
							syntheticCheckId,
							fmt.Sprintf(syntheticCheckOriginPatternExtra, clusterId),
							ApiEndpointStandardizedTest,
							DatasetCustomTest,
							"",
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
					Entry(
						"when the auth token becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								syntheticCheckReconciler.RemoveDefaultApiConfigs(ctx, &logger)
							},
							enabledSync: func() {
								syntheticCheckReconciler.SetDefaultApiConfigs(
									ctx, []ApiConfig{
										{
											Endpoint: ApiEndpointTest,
											Dataset:  DatasetCustomTest,
											Token:    AuthorizationTokenTest,
										},
									}, &logger,
								)
							},
						},
					),
					Entry(
						"when the api endpoint becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								syntheticCheckReconciler.RemoveDefaultApiConfigs(ctx, &logger)
							},
							enabledSync: func() {
								syntheticCheckReconciler.SetDefaultApiConfigs(
									ctx,
									[]ApiConfig{
										{
											Endpoint: ApiEndpointTest,
											Dataset:  DatasetCustomTest,
											Token:    AuthorizationTokenTest,
										},
									},
									&logger,
								)
							},
						},
					),
					Entry(
						"when the operator manager becomes leader", maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								leaderElectionAware.SetLeader(false)
							},
							enabledSync: func() {
								leaderElectionAware.SetLeader(true)
								syntheticCheckReconciler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
							},
						},
					),
					Entry(
						"only sync once evenn if the the auth token is set multiple times",
						maybeDoInitialSynchronizationOfAllResourcesTest{
							disableSync: func() {
								syntheticCheckReconciler.RemoveDefaultApiConfigs(ctx, &logger)
							},
							enabledSync: func() {
								// gock only expects two PUT requests, so if we would synchronize twice, the test would fail
								syntheticCheckReconciler.SetDefaultApiConfigs(
									ctx, []ApiConfig{
										{
											Endpoint: ApiEndpointTest,
											Dataset:  DatasetCustomTest,
											Token:    AuthorizationTokenTest,
										},
									}, &logger,
								)
								syntheticCheckReconciler.SetDefaultApiConfigs(
									ctx, []ApiConfig{
										{
											Endpoint: ApiEndpointTest,
											Dataset:  DatasetCustomTest,
											Token:    AuthorizationTokenTest,
										},
									}, &logger,
								)
							},
						},
					),
				)
			},
		)

		Describe(
			"mapping synthetic check resources to http requests", func() {

				type syntheticCheckToRequestTestConfig struct {
					syntheticCheck      string
					expectedAnnotations map[string]string
				}

				var syntheticCheckReconciler *SyntheticCheckReconciler

				BeforeEach(
					func() {
						syntheticCheckReconciler = &SyntheticCheckReconciler{}
					},
				)

				DescribeTable(
					"maps synthetic checks", func(testConfig syntheticCheckToRequestTestConfig) {
						syntheticCheck := map[string]interface{}{}
						Expect(yaml.Unmarshal([]byte(testConfig.syntheticCheck), &syntheticCheck)).To(Succeed())
						apiConfig := ApiConfig{
							Endpoint: ApiEndpointTest,
							Dataset:  DatasetCustomTest,
							Token:    AuthorizationTokenTest,
						}
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:      "dash0-synthetic-check",
							k8sNamespace: TestNamespaceName,
							resource:     syntheticCheck,
							validatedApiConfigs: []ValidatedApiConfigAndToken{
								*NewValidatedApiConfigAndToken(apiConfig.Endpoint, apiConfig.Dataset, apiConfig.Token),
							},
						}
						resourceToRequestsResult :=
							syntheticCheckReconciler.MapResourceToHttpRequests(
								preconditionValidationResult,
								apiConfig,
								upsertAction,
								&logger,
							)
						Expect(resourceToRequestsResult.ItemsTotal).To(Equal(1))
						Expect(resourceToRequestsResult.OriginsInResource).To(BeNil())
						Expect(resourceToRequestsResult.ValidationIssues).To(BeNil())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())

						Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(1))
						apiRequest := resourceToRequestsResult.ApiRequests[0]
						Expect(apiRequest.ItemName).To(Equal("dash0-synthetic-check"))
						req := apiRequest.Request
						defer func() {
							_ = req.Body.Close()
						}()
						body, err := io.ReadAll(req.Body)
						Expect(err).ToNot(HaveOccurred())
						resultingSyntheticCheckInRequest := map[string]interface{}{}
						Expect(json.Unmarshal(body, &resultingSyntheticCheckInRequest)).To(Succeed())
						Expect(resultingSyntheticCheckInRequest["spec"]).ToNot(BeNil())

						Expect(resultingSyntheticCheckInRequest["metadata"]).ToNot(BeNil())
						Expect(
							ReadFromMap(
								resultingSyntheticCheckInRequest,
								[]string{"metadata", "name"},
							),
						).To(Equal("dash0-synthetic-check"))

						if testConfig.expectedAnnotations != nil {
							annotationsRaw := ReadFromMap(resultingSyntheticCheckInRequest, []string{"metadata", "annotations"})
							Expect(annotationsRaw).ToNot(BeNil())
							annotations := annotationsRaw.(map[string]interface{})
							Expect(annotations).To(HaveLen(len(testConfig.expectedAnnotations)))
							for expectedKey, expectedValue := range testConfig.expectedAnnotations {
								value, ok := annotations[expectedKey]
								Expect(ok).To(BeTrue())
								Expect(value).To(Equal(expectedValue))
							}
						} else {
							Expect(ReadFromMap(resultingSyntheticCheckInRequest, []string{"metadata", "annotations"})).To(BeNil())
						}
					},
					Entry(
						"should map synthetic check", syntheticCheckToRequestTestConfig{
							syntheticCheck: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SyntheticCheck
metadata:
  name: dash0-synthetic-check
spec:
  enabled: true
  notifications:
    channels: []
`,
						},
					),
					Entry(
						"should send annotations", syntheticCheckToRequestTestConfig{
							syntheticCheck: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SyntheticCheck
metadata:
  name: dash0-synthetic-check
  annotations:
    dash0com/annotation1: value1
    dash0com/annotation2: value2
spec:
  enabled: true
  notifications:
    channels: []
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

func createSyntheticCheckReconciler(clusterId string) *SyntheticCheckReconciler {
	syntheticCheckReconciler := NewSyntheticCheckReconciler(
		k8sClient,
		types.UID(clusterId),
		leaderElectionAware,
		&http.Client{},
	)
	return syntheticCheckReconciler
}

func expectSyntheticCheckPutRequestCustom(
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
		JSON(syntheticCheckPutResponse(clusterId, originPattern, dataset))
}

func expectSyntheticCheckPutRequest(clusterId string, expectedPath string) {
	expectSyntheticCheckPutRequestCustom(
		clusterId,
		expectedPath,
		ApiEndpointTest,
		DatasetCustomTest,
		AuthorizationHeaderTest,
		syntheticCheckOriginPattern,
		1,
	)
}

func syntheticCheckPutResponse(clusterId string, originPattern string, dataset string) map[string]interface{} {
	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{
				"dash0.com/id":      syntheticCheckId,
				"dash0.com/origin":  fmt.Sprintf(originPattern, clusterId),
				"dash0.com/dataset": dataset,
			},
		},
	}
}

func expectSyntheticCheckDeleteRequest(expectedPath string) {
	expectSyntheticCheckDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectSyntheticCheckDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status)
}

func createSyntheticCheckResource(namespace string, name string) *dash0v1alpha1.Dash0SyntheticCheck {
	return createSyntheticCheckResourceWithEnableLabel(namespace, name, "")
}

func createSyntheticCheckResourceWithEnableLabel(
	namespace string,
	name string,
	dash0EnableLabelValue string,
) *dash0v1alpha1.Dash0SyntheticCheck {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1alpha1.Dash0SyntheticCheck{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0SyntheticCheck",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1alpha1.Dash0SyntheticCheckSpec{
			Display: dash0v1alpha1.Dash0SyntheticCheckDisplay{
				Name: "Test Synthetic Check",
			},
			Plugin: dash0v1alpha1.Dash0SyntheticCheckPlugin{
				Kind: "http",
				Spec: dash0v1alpha1.Dash0SyntheticCheckHTTPPluginSpec{
					Request: dash0v1alpha1.Dash0SyntheticCheckHTTPRequest{
						Method:    "get",
						URL:       "https://example.com",
						Redirects: "follow",
						TLS: dash0v1alpha1.Dash0SyntheticCheckHTTPTLS{
							AllowInsecure: false,
						},
						Tracing: dash0v1alpha1.Dash0SyntheticCheckHTTPTracing{
							AddTracingHeaders: false,
						},
						Headers:         []dash0v1alpha1.Dash0SyntheticCheckHTTPHeader{},
						QueryParameters: []dash0v1alpha1.Dash0SyntheticCheckHTTPQueryParameter{},
					},
					Assertions: dash0v1alpha1.Dash0SyntheticCheckHTTPAssertions{
						CriticalAssertions: []dash0v1alpha1.Dash0SyntheticCheckAssertion{
							{
								Kind: "status_code",
								Spec: dash0v1alpha1.Dash0SyntheticCheckAssertionSpec{
									Operator: ptr.To("is"),
									Value:    ptr.To("200"),
								},
							},
						},
						DegradedAssertions: []dash0v1alpha1.Dash0SyntheticCheckAssertion{},
					},
				},
			},
			Schedule: dash0v1alpha1.Dash0SyntheticCheckSchedule{
				Strategy:  "all_locations",
				Interval:  "5m",
				Locations: []string{"us-east-1"},
			},
			Retries: dash0v1alpha1.Dash0SyntheticCheckRetries{
				Kind: "off",
				Spec: dash0v1alpha1.Dash0SyntheticCheckRetriesSpec{},
			},
			Notifications: dash0v1alpha1.Dash0SyntheticCheckNotifications{
				Channels: []dash0v1alpha1.NotificationChannelID{},
			},
			Enabled: true,
		},
	}
}

func deleteSyntheticCheckResourceIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	syntheticCheck := &dash0v1alpha1.Dash0SyntheticCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(
		ctx, syntheticCheck, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifySyntheticCheckSynchronizationStatus(
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
			syntheticCheck := &dash0v1alpha1.Dash0SyntheticCheck{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, syntheticCheck,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(syntheticCheck.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(syntheticCheck.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(syntheticCheck.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
			// syntheticChecks have no operator-side validations, all local validations are encoded in the CRD already
			g.Expect(syntheticCheck.Status.ValidationIssues).To(BeNil())

			g.Expect(syntheticCheck.Status.SynchronizationResults).To(HaveLen(1))
			syncResultPerEndpointAndDataset := syntheticCheck.Status.SynchronizationResults[0]
			g.Expect(syncResultPerEndpointAndDataset).ToNot(BeNil())
			g.Expect(syncResultPerEndpointAndDataset.Dash0ApiEndpoint).To(Equal(expectedApiEndpoint))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Dataset).To(Equal(expectedDataset))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Id).To(Equal(expectedId))
			g.Expect(syncResultPerEndpointAndDataset.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(syncResultPerEndpointAndDataset.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}

func verifySyntheticCheckHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	Eventually(
		func(g Gomega) {
			syntheticCheck := &dash0v1alpha1.Dash0SyntheticCheck{}
			err := k8sClient.Get(
				ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, syntheticCheck,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(syntheticCheck.Status.SynchronizationStatus)).To(Equal(""))
			g.Expect(syntheticCheck.Status.ValidationIssues).To(BeNil())
			g.Expect(syntheticCheck.Status.SynchronizationResults).To(HaveLen(0))
		},
	).Should(Succeed())
}
