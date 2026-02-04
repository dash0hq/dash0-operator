// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	persesv1alpha1 "github.com/perses/perses-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	dashboardOriginPattern            = "dash0-operator_%s_test-dataset_test-namespace_test-dashboard"
	dashboardOriginPatternAlternative = "dash0-operator_%s_test-dataset-alt_test-namespace_test-dashboard"
)

var (
	persesDashboardCrd        *apiextensionsv1.CustomResourceDefinition
	testQueuePersesDashboards = workqueue.NewTypedWithConfig(workqueue.TypedQueueConfig[ThirdPartyResourceSyncJob]{
		Name: "dash0-third-party-resource-synchronization-queue",
	})

	dashboardApiBasePath = "/api/dashboards/"

	defaultExpectedPathDashboard     = fmt.Sprintf("%s.*%s", dashboardApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-dashboard")
	ExpectedPathDashboardAlternative = fmt.Sprintf("%s.*%s", dashboardApiBasePath, "dash0-operator_.*_test-dataset-alt_test-namespace_test-dashboard")
)

var _ = Describe("The Perses dashboard controller", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	var clusterId string

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
		clusterId = string(util.ReadPseudoClusterUid(ctx, k8sClient, &logger))
	})

	Describe("the Perses dashboard CRD reconciler", func() {

		AfterEach(func() {
			deletePersesDashboardCrdIfItExists(ctx)
		})

		It("does not start watching Perses dashboard resources if the CRD does not exist and neither API endpoint nor auth token have been provided", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Perses dashboard resources if the CRD does not exist and the auth token has not been provided", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetDefaultApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the CRD does not exist and the API endpoint has not been provided", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetDefaultAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the API endpoint & auth token have been provided but the CRD does not exist", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetDefaultApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetDefaultAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the CRD exists but the auth token has not been provided", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetDefaultApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the CRD exists but the API endpoint has not been provided", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetDefaultAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("starts watching Perses dashboards if the CRD exists and the API endpoint has been provided", func() {
			ensurePersesDashboardCrdExists(ctx)
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
			persesDashboardCrdReconciler.SetDefaultApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetDefaultAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
		})

		It("starts watching Perses dashboards if API endpoint is provided and the CRD is created later on", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())

			// provide the API endpoint first
			persesDashboardCrdReconciler.SetDefaultApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetDefaultAuthToken(ctx, AuthorizationTokenTest, &logger)

			// create the CRD a bit later
			time.Sleep(100 * time.Millisecond)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
			ensurePersesDashboardCrdExists(ctx)
			// watches are not triggered in unit tests
			persesDashboardCrdReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: persesDashboardCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			// verify that the controller starts watching when it sees the CRD being created
			Eventually(func(g Gomega) {
				g.Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
			}).Should(Succeed())
		})

		It("stops watching Perses dashboards if the CRD is deleted", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
			persesDashboardCrdReconciler.SetDefaultAuthToken(ctx, AuthorizationTokenTest, &logger)
			persesDashboardCrdReconciler.SetDefaultApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())

			deletePersesDashboardCrdIfItExists(ctx)
			// watches are not triggered in unit tests
			persesDashboardCrdReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[client.Object]{
					Object: persesDashboardCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)
			Eventually(func(g Gomega) {
				Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
			}).Should(Succeed())
		})

		It("can cope with multiple consecutive create & delete events", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetDefaultApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetDefaultAuthToken(ctx, AuthorizationTokenTest, &logger)

			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
			ensurePersesDashboardCrdExists(ctx)
			persesDashboardCrdReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: persesDashboardCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)
			Eventually(func(g Gomega) {
				g.Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
			}).Should(Succeed())

			deletePersesDashboardCrdIfItExists(ctx)
			persesDashboardCrdReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[client.Object]{
					Object: persesDashboardCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)
			Eventually(func(g Gomega) {
				g.Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
			}).Should(Succeed())

			ensurePersesDashboardCrdExists(ctx)
			persesDashboardCrdReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: persesDashboardCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)
			Eventually(func(g Gomega) {
				g.Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
			}).Should(Succeed())
		})
	})

	Describe("the Perses dashboard resource reconciler", func() {
		var persesDashboardCrdReconciler *PersesDashboardCrdReconciler
		var persesDashboardReconciler *PersesDashboardReconciler

		BeforeAll(func() {
			persesDashboardCrdReconciler = createPersesDashboardCrdReconciler()
			ensurePersesDashboardCrdExists(ctx)

			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())

			StartProcessingThirdPartySynchronizationQueue(testQueuePersesDashboards, &logger)
		})

		BeforeEach(func() {
			persesDashboardCrdReconciler.SetDefaultApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetDefaultAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
			persesDashboardReconciler = persesDashboardCrdReconciler.persesDashboardReconciler
			// to make tests that involve http retries faster, we do not want to wait for one second for each retry
			persesDashboardReconciler.overrideHttpRetryDelay(20 * time.Millisecond)
		})

		AfterEach(func() {
			DeleteMonitoringResourceIfItExists(ctx, k8sClient)
			persesDashboardCrdReconciler.RemoveNamespacedApiEndpointAndDataset(ctx, TestNamespaceName, &logger)
			persesDashboardCrdReconciler.RemoveNamespacedAuthToken(ctx, TestNamespaceName, &logger)
		})

		AfterAll(func() {
			deletePersesDashboardCrdIfItExists(ctx)
			StopProcessingThirdPartySynchronizationQueue(testQueuePersesDashboards, &logger)
		})

		It("it ignores Perses dashboard resource changes if no Dash0 monitoring resource exists in the namespace", func() {
			expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsPending()).To(BeTrue())
		})

		It("it ignores Perses dashboard resource changes if synchronization is disabled via the Dash0 monitoring resource", func() {
			monitoringResource := EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)
			monitoringResource.Spec.SynchronizePersesDashboards = ptr.To(false)
			Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())

			expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyNoPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
			Expect(gock.IsPending()).To(BeTrue())
		})

		It("it ignores Perses dashboard resource changes if the API endpoint is not configured", func() {
			EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
			defer gock.Off()

			persesDashboardCrdReconciler.RemoveDefaultApiEndpointAndDataset(ctx, &logger)

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyNoPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
			Expect(gock.IsPending()).To(BeTrue())
		})

		It("creates a dashboard", func() {
			EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPersesSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("creates a dashboard with namespaced config from the monitoring resource", func() {
			monitoringResource := DefaultMonitoringResourceWithCustomApiConfigAndToken(
				MonitoringResourceQualifiedName,
				ApiEndpointTestAlternative,
				DatasetCustomTestAlternative,
				AuthorizationTokenTestAlternative)
			EnsureMonitoringResourceWithSpecExistsAndIsAvailable(ctx, k8sClient, monitoringResource.Spec)

			expectDashboardPutRequestCustom(
				clusterId,
				ExpectedPathDashboardAlternative,
				ApiEndpointTestAlternative,
				DatasetCustomTestAlternative,
				AuthorizationHeaderTestAlternative,
				dashboardOriginPatternAlternative,
			)
			defer gock.Off()

			apiConfig := ApiConfig{
				Endpoint: ApiEndpointTestAlternative,
				Dataset:  DatasetCustomTestAlternative,
			}

			persesDashboardCrdReconciler.SetNamespacedApiEndpointAndDataset(ctx, TestNamespaceName, &apiConfig, &logger)
			persesDashboardCrdReconciler.SetNamespacedAuthToken(ctx, TestNamespaceName, AuthorizationTokenTestAlternative, &logger)

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				expectedPersesSyncResult(clusterId, ApiEndpointStandardizedTestAlternative, DatasetCustomTestAlternative, dashboardOriginPatternAlternative),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates a dashboard", func() {
			EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectNew: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPersesSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates a dashboard if dash0.com/enable is set but not to \"false\"", func() {
			EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardPutRequest(clusterId, defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResourceWithEnableLabel("whatever")
			persesDashboardReconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectNew: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPersesSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a dashboard", func() {
			EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardDeleteRequestWithHttpStatus(defaultExpectedPathDashboard, http.StatusNotFound)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[*unstructured.Unstructured]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPersesSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a dashboard on Create (and does not try to create it) if labelled with dash0.com/enable=false", func() {
			EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardDeleteRequestWithHttpStatus(defaultExpectedPathDashboard, http.StatusNotFound)
			defer gock.Off()

			dashboardResource := createDashboardResourceWithEnableLabel("false")
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPersesSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a dashboard on Update (and does not try to update it) if labelled with dash0.com/enable=false", func() {
			EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardDeleteRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResourceWithEnableLabel("false")
			persesDashboardReconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectNew: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPersesSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("reports validation issues for a dashboard", func() {
			EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

			dashboardResource := createDashboardResource()
			spec := dashboardResource.Object["spec"].(map[string]interface{})
			spec["display"] = "not a map"
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0common.PersesDashboardSynchronizationResults{
					SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
					ValidationIssues:      []string{"spec.display is not a map"},
					SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
						{
							Dash0ApiEndpoint:     ApiEndpointStandardizedTest,
							Dash0Origin:          "",
							Dash0Dataset:         DatasetCustomTest,
							SynchronizationError: "",
						},
					},
				},
			)
		})

		It("reports http errors when synchronizing a dashboard", func() {
			EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

			gock.New(ApiEndpointTest).
				Put(defaultExpectedPathDashboard).
				MatchParam("dataset", DatasetCustomTest).
				Times(3).
				Reply(503).
				JSON(map[string]string{})
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0common.PersesDashboardSynchronizationResults{
					SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
					ValidationIssues:      nil,
					SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
						{
							Dash0ApiEndpoint:     ApiEndpointStandardizedTest,
							Dash0Origin:          "",
							Dash0Dataset:         DatasetCustomTest,
							SynchronizationError: "^unexpected status code 503 when trying to synchronize the dashboard \"test-dashboard\": PUT https://api.dash0.com/api/dashboards/dash0-operator_.*_test-dataset_test-namespace_test-dashboard\\?dataset=test-dataset, response body is {}\n$",
						},
					},
				},
			)
			Expect(gock.IsDone()).To(BeTrue())
		})
	})

	Describe("mapping dashboard resources to http requests", func() {

		type dashboardToRequestTestConfig struct {
			dashboard           string
			expectedName        string
			expectedDescription *string
			expectedAnnotations map[string]string
		}

		var persesDashboardReconciler *PersesDashboardReconciler

		BeforeEach(func() {
			persesDashboardReconciler = &PersesDashboardReconciler{}
		})

		DescribeTable("maps both CRD versions", func(testConfig dashboardToRequestTestConfig) {
			dashboard := map[string]interface{}{}
			Expect(yaml.Unmarshal([]byte(testConfig.dashboard), &dashboard)).To(Succeed())
			preconditionValidationResult := &preconditionValidationResult{
				k8sName:            "perses-dashboard",
				k8sNamespace:       TestNamespaceName,
				resource:           dashboard,
				validatedApiConfig: &ValidatedApiConfigAndToken{},
			}
			resourceToRequestsResult :=
				persesDashboardReconciler.MapResourceToHttpRequests(
					preconditionValidationResult,
					upsertAction,
					&logger,
				)
			Expect(resourceToRequestsResult.ItemsTotal).To(Equal(1))
			Expect(resourceToRequestsResult.OriginsInResource).To(BeNil())
			Expect(resourceToRequestsResult.ValidationIssues).To(BeNil())
			Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())

			Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(1))
			apiRequest := resourceToRequestsResult.ApiRequests[0]
			Expect(apiRequest.ItemName).To(Equal("perses-dashboard"))
			req := apiRequest.Request
			defer func() {
				_ = req.Body.Close()
			}()
			body, err := io.ReadAll(req.Body)
			Expect(err).ToNot(HaveOccurred())
			resultingDashboardInRequest := map[string]interface{}{}
			Expect(json.Unmarshal(body, &resultingDashboardInRequest)).To(Succeed())
			Expect(ReadFromMap(resultingDashboardInRequest, []string{"spec", "display", "name"})).To(Equal(testConfig.expectedName))
			if testConfig.expectedDescription != nil {
				Expect(ReadFromMap(resultingDashboardInRequest, []string{"spec", "display", "description"})).To(Equal(*testConfig.expectedDescription))
			} else {
				Expect(ReadFromMap(resultingDashboardInRequest, []string{"spec", "display", "description"})).To(BeNil())
			}

			Expect(resultingDashboardInRequest["metadata"]).ToNot(BeNil())
			Expect(ReadFromMap(resultingDashboardInRequest, []string{"metadata", "name"})).To(Equal("perses-dashboard"))

			if testConfig.expectedAnnotations != nil {
				annotationsRaw := ReadFromMap(resultingDashboardInRequest, []string{"metadata", "annotations"})
				Expect(annotationsRaw).ToNot(BeNil())
				annotations := annotationsRaw.(map[string]interface{})
				Expect(annotations).To(HaveLen(len(testConfig.expectedAnnotations)))
				for expectedKey, expectedValue := range testConfig.expectedAnnotations {
					value, ok := annotations[expectedKey]
					Expect(ok).To(BeTrue())
					Expect(value).To(Equal(expectedValue))
				}
			} else {
				Expect(ReadFromMap(resultingDashboardInRequest, []string{"metadata", "annotations"})).To(BeNil())
			}
		},
			Entry("should map v1alpha1", dashboardToRequestTestConfig{
				dashboard: `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  display:
    name: Perses Dashboard Example
    description: This is an example dashboard.
  duration: 5m
`,
				expectedName:        "Perses Dashboard Example",
				expectedDescription: ptr.To("This is an example dashboard."),
			}),
			Entry("should map v1alpha2", dashboardToRequestTestConfig{
				dashboard: `
apiVersion: perses.dev/v1alpha2
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  config:
    display:
      name: Perses Dashboard Example
      description: This is an example dashboard.
    duration: 5m
`,
				expectedName:        "Perses Dashboard Example",
				expectedDescription: ptr.To("This is an example dashboard."),
			}),
			Entry("should add name to v1alpha1 with display but without name", dashboardToRequestTestConfig{
				dashboard: `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  display:
    description: This is an example dashboard.
  duration: 5m
`,
				expectedName:        fmt.Sprintf("%s/perses-dashboard", TestNamespaceName),
				expectedDescription: ptr.To("This is an example dashboard."),
			}),
			Entry("should add name to v1alpha2 with display but without name", dashboardToRequestTestConfig{
				dashboard: `
apiVersion: perses.dev/v1alpha2
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  config:
    display:
      description: This is an example dashboard.
    duration: 5m
`,
				expectedName:        fmt.Sprintf("%s/perses-dashboard", TestNamespaceName),
				expectedDescription: ptr.To("This is an example dashboard."),
			}),
			Entry("should add name to v1alpha1 without display", dashboardToRequestTestConfig{
				dashboard: `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  duration: 5m
`,
				expectedName:        fmt.Sprintf("%s/perses-dashboard", TestNamespaceName),
				expectedDescription: nil,
			}),
			Entry("should add name to v1alpha2 without display", dashboardToRequestTestConfig{
				dashboard: `
apiVersion: perses.dev/v1alpha2
kind: PersesDashboard
metadata:
  name: perses-dashboard
spec:
  config:
    duration: 5m
`,
				expectedName:        fmt.Sprintf("%s/perses-dashboard", TestNamespaceName),
				expectedDescription: nil,
			}),
			Entry("should send annotations with v1alpha1", dashboardToRequestTestConfig{
				dashboard: `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: perses-dashboard
  annotations:
    dash0com/annotation1: value1
    dash0com/annotation2: value2
spec:
  display:
    name: Perses Dashboard Example
    description: This is an example dashboard.
  duration: 5m
`,
				expectedName:        "Perses Dashboard Example",
				expectedDescription: ptr.To("This is an example dashboard."),
				expectedAnnotations: map[string]string{
					"dash0com/annotation1": "value1",
					"dash0com/annotation2": "value2",
				},
			}),
			Entry("should send annotations with v1alpha2", dashboardToRequestTestConfig{
				dashboard: `
apiVersion: perses.dev/v1alpha2
kind: PersesDashboard
metadata:
  name: perses-dashboard
  annotations:
    dash0com/annotation1: value1
    dash0com/annotation2: value2
spec:
  config:
    display:
      name: Perses Dashboard Example
      description: This is an example dashboard.
    duration: 5m
`,
				expectedName:        "Perses Dashboard Example",
				expectedDescription: ptr.To("This is an example dashboard."),
				expectedAnnotations: map[string]string{
					"dash0com/annotation1": "value1",
					"dash0com/annotation2": "value2",
				},
			}),
		)
	})
})

func createPersesDashboardCrdReconciler() *PersesDashboardCrdReconciler {
	crdReconciler := NewPersesDashboardCrdReconciler(
		k8sClient,
		testQueuePersesDashboards,
		&DummyLeaderElectionAware{Leader: true},
		&http.Client{},
	)

	// We create the controller multiple times in tests, this option is required, otherwise the controller
	// runtime will complain.
	crdReconciler.skipNameValidation = true
	return crdReconciler
}

func expectDashboardPutRequestCustom(clusterId string, expectedPath string, endpoint string, dataset string, authHeader string, originPattern string) {
	gock.New(endpoint).
		Put(expectedPath).
		MatchHeader("Authorization", authHeader).
		MatchParam("dataset", dataset).
		Times(1).
		Reply(200).
		JSON(dashboardPutResponse(clusterId, dataset, originPattern))
}

func expectDashboardPutRequest(clusterId string, expectedPath string) {
	expectDashboardPutRequestCustom(clusterId, expectedPath, ApiEndpointTest, DatasetCustomTest, AuthorizationHeaderTest, dashboardOriginPattern)
}

func dashboardPutResponse(clusterId string, dataset string, originPattern string) map[string]interface{} {
	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"dash0Extensions": map[string]interface{}{
				"id":      fmt.Sprintf(originPattern, clusterId),
				"dataset": dataset,
			},
		},
	}
}

func expectedPersesSyncResult(clusterId string, apiEndpoint string, dataset string, originPattern string) dash0common.PersesDashboardSynchronizationResults {
	return dash0common.PersesDashboardSynchronizationResults{
		SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusSuccessful,
		ValidationIssues:      nil,
		SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
			{
				Dash0ApiEndpoint:     apiEndpoint,
				Dash0Origin:          fmt.Sprintf(originPattern, clusterId),
				Dash0Dataset:         dataset,
				SynchronizationError: "",
			},
		},
	}
}

func defaultExpectedPersesSyncResult(clusterId string) dash0common.PersesDashboardSynchronizationResults {
	return expectedPersesSyncResult(clusterId, ApiEndpointStandardizedTest, DatasetCustomTest, dashboardOriginPattern)
}

func expectDashboardDeleteRequest(expectedPath string) {
	expectDashboardDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectDashboardDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status)
}

func createDashboardResource() unstructured.Unstructured {
	return createDashboardResourceWithEnableLabel("")
}

func createDashboardResourceWithEnableLabel(dash0EnableLabelValue string) unstructured.Unstructured {
	objectMeta := metav1.ObjectMeta{
		Name:      "test-dashboard",
		Namespace: TestNamespaceName,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	dashboard := persesv1alpha1.PersesDashboard{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "perses.dev/v1alpha1",
			Kind:       "PersesDashboard",
		},
		ObjectMeta: objectMeta,
		Spec:       persesv1alpha1.Dashboard{},
	}
	marshalled, err := json.Marshal(dashboard)
	Expect(err).NotTo(HaveOccurred())
	unstructuredObject := unstructured.Unstructured{}
	err = json.Unmarshal(marshalled, &unstructuredObject)
	Expect(err).NotTo(HaveOccurred())
	return unstructuredObject
}

func ensurePersesDashboardCrdExists(ctx context.Context) {
	persesDashboardCrd = EnsurePersesDashboardCrdExists(
		ctx,
		k8sClient,
	)
}

func deletePersesDashboardCrdIfItExists(ctx context.Context) {
	if persesDashboardCrd != nil {
		err := k8sClient.Delete(ctx, persesDashboardCrd, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		})
		if err != nil && apierrors.IsNotFound(err) {
			return
		} else if err != nil {
			Expect(err).NotTo(HaveOccurred())
		}

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, PersesDashboardCrdQualifiedName, &apiextensionsv1.CustomResourceDefinition{})
			g.Expect(err).To(HaveOccurred())
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}).Should(Succeed())

		persesDashboardCrd = nil
	}
}

func isWatchingPersesDashboardResources(persesDashboardCrdReconciler *PersesDashboardCrdReconciler) bool {
	dashboardReconciler := persesDashboardCrdReconciler.persesDashboardReconciler
	dashboardReconciler.ControllerStopFunctionLock().Lock()
	defer dashboardReconciler.ControllerStopFunctionLock().Unlock()
	return dashboardReconciler.IsWatching()
}

func verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
	expectedResult dash0common.PersesDashboardSynchronizationResults,
) {
	Eventually(func(g Gomega) {
		monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
		results := monRes.Status.PersesDashboardSynchronizationResults
		g.Expect(results).NotTo(BeNil())
		g.Expect(results).To(HaveLen(1))
		result := results[fmt.Sprintf("%s/%s", TestNamespaceName, "test-dashboard")]
		g.Expect(result.SynchronizationResults).To(HaveLen(1))
		g.Expect(result).NotTo(BeNil())
		actualSyncResult := &result.SynchronizationResults[0]
		expectedSyncResult := &expectedResult.SynchronizationResults[0]
		if expectedSyncResult.SynchronizationError != "" {
			// http errors contain a different random path for each test execution
			g.Expect(actualSyncResult.SynchronizationError).To(MatchRegexp(expectedSyncResult.SynchronizationError))
		}
		g.Expect(actualSyncResult.Dash0Origin).To(Equal(expectedSyncResult.Dash0Origin))

		// we do not verify the exact timestamp
		expectedResult.SynchronizedAt = result.SynchronizedAt
		// errors have been verified using regex
		actualSyncResult.SynchronizationError = ""
		expectedSyncResult.SynchronizationError = ""

		g.Expect(result).To(Equal(expectedResult))
	}).Should(Succeed())
}

func verifyNoPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
) {
	Consistently(func(g Gomega) {
		monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
		results := monRes.Status.PersesDashboardSynchronizationResults
		g.Expect(results).To(BeNil())
	}, 200*time.Millisecond, 50*time.Millisecond).Should(Succeed())
}
