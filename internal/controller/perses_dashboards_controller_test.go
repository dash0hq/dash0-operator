// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	persesv1alpha1 "github.com/perses/perses-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	persesDashboardCrdReconciler *PersesDashboardCrdReconciler
	persesDashboardCrd           *apiextensionsv1.CustomResourceDefinition

	dashboardApiBasePath = "/api/dashboards/"

	defaultExpectedPathDashboard = fmt.Sprintf("%s.*%s", dashboardApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-dashboard")

	defaultExpectedPersesSyncResult = dash0v1alpha1.PersesDashboardSynchronizationResults{
		SynchronizationStatus: dash0v1alpha1.Successful,
		SynchronizationError:  "",
		ValidationIssues:      nil,
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

		It("does not create a Perses dashboard resource reconciler if there is no auth token", func() {
			createPersesDashboardCrdReconcilerWithoutAuthToken()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(persesDashboardCrdReconciler.persesDashboardReconciler).To(BeNil())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(persesDashboardCrdReconciler.persesDashboardReconciler).To(BeNil())
		})

		It("does not start watching Perses dashboards if the CRD does not exist and the API endpoint has not been provided", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPersesDashboardResources()).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the API endpoint has been provided but the CRD does not exist", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(isWatchingPersesDashboardResources()).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the CRD exists but the API endpoint has not been provided", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPersesDashboardResources()).To(BeFalse())
		})

		It("starts watching Perses dashboards if the CRD exists and the API endpoint has been provided", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPersesDashboardResources()).To(BeFalse())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(isWatchingPersesDashboardResources()).To(BeTrue())
		})

		It("starts watching Perses dashboards if API endpoint is provided and the CRD is created later on", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())

			// provide the API endpoint first
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)

			// create the CRD a bit later
			time.Sleep(100 * time.Millisecond)
			Expect(isWatchingPersesDashboardResources()).To(BeFalse())
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
				g.Expect(isWatchingPersesDashboardResources()).To(BeTrue())
			}).Should(Succeed())
		})

		It("stops watching Perses dashboards if the CRD is deleted", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPersesDashboardResources()).To(BeFalse())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(isWatchingPersesDashboardResources()).To(BeTrue())

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
				Expect(isWatchingPersesDashboardResources()).To(BeFalse())
			}).Should(Succeed())
		})

		It("can cope with multiple consecutive create & delete events", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)

			Expect(isWatchingPersesDashboardResources()).To(BeFalse())
			ensurePersesDashboardCrdExists(ctx)
			persesDashboardCrdReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: persesDashboardCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)
			Eventually(func(g Gomega) {
				g.Expect(isWatchingPersesDashboardResources()).To(BeTrue())
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
				g.Expect(isWatchingPersesDashboardResources()).To(BeFalse())
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
				g.Expect(isWatchingPersesDashboardResources()).To(BeTrue())
			}).Should(Succeed())
		})
	})

	Describe("the Perses dashboard resource reconciler", func() {
		var persesDashboardReconciler *PersesDashboardReconciler

		BeforeAll(func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			ensurePersesDashboardCrdExists(ctx)

			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
		})

		BeforeEach(func() {
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(isWatchingPersesDashboardResources()).To(BeTrue())
			persesDashboardReconciler = persesDashboardCrdReconciler.persesDashboardReconciler
			// to make tests that involve http retries faster, we do not want to wait for one second for each retry
			persesDashboardReconciler.overrideHttpRetryDelay(20 * time.Millisecond)
		})

		AfterEach(func() {
			DeleteMonitoringResourceIfItExists(ctx, k8sClient)
		})

		AfterAll(func() {
			deletePersesDashboardCrdIfItExists(ctx)
		})

		It("it ignores Perses dashboard resource changes if no Dash0 monitoring resource exists in the namespace", func() {
			expectDashboardPutRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: dashboardResource,
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
				event.TypedCreateEvent[client.Object]{
					Object: dashboardResource,
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

			persesDashboardCrdReconciler.RemoveApiEndpointAndDataset()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: dashboardResource,
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
				event.TypedCreateEvent[client.Object]{
					Object: dashboardResource,
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
				event.TypedUpdateEvent[client.Object]{
					ObjectNew: dashboardResource,
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

			expectDashboardDeleteRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[client.Object]{
					Object: dashboardResource,
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

		It("reports http errors when synchronizing a dashboard", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			gock.New(ApiEndpointTest).
				Put(defaultExpectedPathDashboard).
				MatchParam("dataset", DatasetTest).
				Times(3).
				Reply(503).
				JSON(map[string]string{})
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0v1alpha1.PersesDashboardSynchronizationResults{
					SynchronizationStatus: dash0v1alpha1.Failed,
					SynchronizationError:  "^unexpected status code 503 when updating/creating/deleting the dashboard \"test-dashboard\" at https://api.dash0.com/api/dashboards/dash0-operator_.*_test-dataset_test-namespace_test-dashboard\\?dataset=test-dataset, response body is {}\n$",
					ValidationIssues:      nil,
				},
			)
			Expect(gock.IsDone()).To(BeTrue())
		})
	})
})

func createPersesDashboardCrdReconcilerWithoutAuthToken() {
	persesDashboardCrdReconciler = &PersesDashboardCrdReconciler{
		Client: k8sClient,

		// We create the controller multiple times in tests, this option is required, otherwise the controller
		// runtime will complain.
		skipNameValidation: true,
	}
}

func createPersesDashboardCrdReconcilerWithAuthToken() {
	persesDashboardCrdReconciler = &PersesDashboardCrdReconciler{
		Client:    k8sClient,
		AuthToken: AuthorizationTokenTest,

		// We create the controller multiple times in tests, this option is required, otherwise the controller
		// runtime will complain.
		skipNameValidation: true,
	}
}

func expectDashboardPutRequest(expectedPath string) {
	gock.New(ApiEndpointTest).
		Put(expectedPath).
		MatchParam("dataset", DatasetTest).
		Times(1).
		Reply(200).
		JSON(map[string]string{})
}

func expectDashboardDeleteRequest(expectedPath string) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchParam("dataset", DatasetTest).
		Times(1).
		Reply(200).
		JSON(map[string]string{})
}

func createDashboardResource() *persesv1alpha1.PersesDashboard {
	return &persesv1alpha1.PersesDashboard{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "perses.dev/v1alpha1",
			Kind:       "PersesDashboard",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dashboard",
			Namespace: TestNamespaceName,
		},
		Spec: persesv1alpha1.Dashboard{},
	}
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

func isWatchingPersesDashboardResources() bool {
	dashboardReconciler := persesDashboardCrdReconciler.persesDashboardReconciler
	dashboardReconciler.ControllerStopFunctionLock().Lock()
	defer dashboardReconciler.ControllerStopFunctionLock().Unlock()
	return dashboardReconciler.IsWatching()
}

func verifyPersesDashboardSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
	expectedResult dash0v1alpha1.PersesDashboardSynchronizationResults,
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
