// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
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

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	dashboardOrigin = "dashboard-origin"
)

var (
	persesDashboardCrd        *apiextensionsv1.CustomResourceDefinition
	testQueuePersesDashboards = workqueue.NewTypedWithConfig(workqueue.TypedQueueConfig[ThirdPartyResourceSyncJob]{
		Name: "dash0-third-party-resource-synchronization-queue",
	})

	dashboardApiBasePath = "/api/dashboards/"

	defaultExpectedPathDashboard = fmt.Sprintf("%s.*%s", dashboardApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-dashboard")

	defaultExpectedPersesSyncResult = dash0common.PersesDashboardSynchronizationResults{
		SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusSuccessful,
		Dash0Origin:           dashboardOrigin,
		Dash0Dataset:          DatasetCustomTest,
		SynchronizationError:  "",
		ValidationIssues:      nil,
	}

	defaultExpectedPersesSyncResultNoOriginAndDataset = dash0common.PersesDashboardSynchronizationResults{
		SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusSuccessful,
		SynchronizationError:  "",
		ValidationIssues:      nil,
	}

	dashboardResponseWithOriginAndDataset = map[string]interface{}{
		"metadata": map[string]interface{}{
			"dash0Extensions": map[string]interface{}{
				"id":      dashboardOrigin,
				"dataset": DatasetCustomTest,
			},
		},
	}
)

var _ = Describe("The Perses dashboard controller", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
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
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the CRD does not exist and the API endpoint has not been provided", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the API endpoint & auth token have been provided but the CRD does not exist", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the CRD exists but the auth token has not been provided", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the CRD exists but the API endpoint has not been provided", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
		})

		It("starts watching Perses dashboards if the CRD exists and the API endpoint has been provided", func() {
			ensurePersesDashboardCrdExists(ctx)
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeFalse())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
		})

		It("starts watching Perses dashboards if API endpoint is provided and the CRD is created later on", func() {
			persesDashboardCrdReconciler := createPersesDashboardCrdReconciler()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())

			// provide the API endpoint first
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)

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
			persesDashboardCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(ctx,
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
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)

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
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(ctx,
				&ApiConfig{
					Endpoint: ApiEndpointTest,
					Dataset:  DatasetCustomTest,
				}, &logger)
			persesDashboardCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPersesDashboardResources(persesDashboardCrdReconciler)).To(BeTrue())
			persesDashboardReconciler = persesDashboardCrdReconciler.persesDashboardReconciler
			// to make tests that involve http retries faster, we do not want to wait for one second for each retry
			persesDashboardReconciler.overrideHttpRetryDelay(20 * time.Millisecond)
		})

		AfterEach(func() {
			DeleteMonitoringResourceIfItExists(ctx, k8sClient)
		})

		AfterAll(func() {
			deletePersesDashboardCrdIfItExists(ctx)
			StopProcessingThirdPartySynchronizationQueue(testQueuePersesDashboards, &logger)
		})

		It("it ignores Perses dashboard resource changes if no Dash0 monitoring resource exists in the namespace", func() {
			expectDashboardPutRequest(defaultExpectedPathDashboard)
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
			monitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
			monitoringResource.Spec.SynchronizePersesDashboards = ptr.To(false)
			Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())

			expectDashboardPutRequest(defaultExpectedPathDashboard)
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
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardPutRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			persesDashboardCrdReconciler.RemoveApiEndpointAndDataset(ctx, &logger)

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
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardPutRequest(defaultExpectedPathDashboard)
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
				defaultExpectedPersesSyncResult,
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates a dashboard", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardPutRequest(defaultExpectedPathDashboard)
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
				defaultExpectedPersesSyncResult,
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates a dashboard if dash0.com/enable is set but not to \"false\"", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectDashboardPutRequest(defaultExpectedPathDashboard)
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
				defaultExpectedPersesSyncResult,
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a dashboard", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

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
				defaultExpectedPersesSyncResultNoOriginAndDataset,
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a dashboard on Create (and does not try to create it) if labelled with dash0.com/enable=false", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

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
				defaultExpectedPersesSyncResultNoOriginAndDataset,
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a dashboard on Update (and does not try to update it) if labelled with dash0.com/enable=false", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

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
				defaultExpectedPersesSyncResultNoOriginAndDataset,
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("reports validation issues for a dashboard", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

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
					SynchronizationError:  "",
					ValidationIssues:      []string{"spec.display is not a map"},
				},
			)
		})

		It("reports http errors when synchronizing a dashboard", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

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
					SynchronizationError:  "^unexpected status code 503 when trying to synchronize the dashboard \"test-dashboard\": PUT https://api.dash0.com/api/dashboards/dash0-operator_.*_test-dataset_test-namespace_test-dashboard\\?dataset=test-dataset, response body is {}\n$",
					ValidationIssues:      nil,
				},
			)
			Expect(gock.IsDone()).To(BeTrue())
		})
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

func expectDashboardPutRequest(expectedPath string) {
	gock.New(ApiEndpointTest).
		Put(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(200).
		JSON(dashboardResponseWithOriginAndDataset)
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
		Reply(status).
		JSON(map[string]string{})
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
		g.Expect(result).NotTo(BeNil())
		if expectedResult.SynchronizationError != "" {
			// http errors contain a different random path for each run
			g.Expect(result.SynchronizationError).To(MatchRegexp(expectedResult.SynchronizationError))
			result.SynchronizationError = ""
			expectedResult.SynchronizationError = ""
		}

		// we do not verify the exact timestamp
		expectedResult.SynchronizedAt = result.SynchronizedAt

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
