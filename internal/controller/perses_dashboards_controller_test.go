// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	persesv1alpha1 "github.com/perses/perses-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
			ensurePersesDashboardCrdDoesNotExist(ctx)
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
			Expect(persesDashboardCrdReconciler.persesDashboardReconciler.isWatching.Load()).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the API endpoint has been provided but the CRD does not exist", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(persesDashboardCrdReconciler.persesDashboardReconciler.isWatching.Load()).To(BeFalse())
		})

		It("does not start watching Perses dashboards if the CRD exists but the API endpoint has not been provided", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(persesDashboardCrdReconciler.persesDashboardReconciler.isWatching.Load()).To(BeFalse())
		})

		It("starts watching Perses dashboards if the CRD exists and the API endpoint has been provided", func() {
			createPersesDashboardCrdReconcilerWithAuthToken()
			ensurePersesDashboardCrdExists(ctx)
			Expect(persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(persesDashboardCrdReconciler.persesDashboardReconciler.isWatching.Load()).To(BeFalse())
			persesDashboardCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(persesDashboardCrdReconciler.persesDashboardReconciler.isWatching.Load()).To(BeTrue())
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
			Expect(persesDashboardCrdReconciler.persesDashboardReconciler.isWatching.Load()).To(BeFalse())
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
				g.Expect(persesDashboardCrdReconciler.persesDashboardReconciler.isWatching.Load()).To(BeTrue())
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
			Expect(persesDashboardCrdReconciler.persesDashboardReconciler.isWatching.Load()).To(BeTrue())
			persesDashboardReconciler = persesDashboardCrdReconciler.persesDashboardReconciler
		})

		AfterAll(func() {
			ensurePersesDashboardCrdDoesNotExist(ctx)
		})

		It("creates a dashboard", func() {
			expectDashboardPutRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates a dashboard", func() {
			expectDashboardPutRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Update(
				ctx,
				event.TypedUpdateEvent[client.Object]{
					ObjectNew: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a dashboard", func() {
			expectDashboardDeleteRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[client.Object]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsDone()).To(BeTrue())
		})

		It("it ignores Perses dashboard resource changes if API endpoint is not configured", func() {
			expectDashboardPutRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			persesDashboardCrdReconciler.RemoveApiEndpointAndDataset()

			dashboardResource := createDashboardResource()
			persesDashboardReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &dashboardResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsPending()).To(BeTrue())
		})
	})
})

func createPersesDashboardCrdReconcilerWithoutAuthToken() {
	persesDashboardCrdReconciler = &PersesDashboardCrdReconciler{
		// We create the controller multiple times in tests, this option is required, otherwise the controller
		// runtime will complain.
		skipNameValidation: true,
	}
}

func createPersesDashboardCrdReconcilerWithAuthToken() {
	persesDashboardCrdReconciler = &PersesDashboardCrdReconciler{
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

func createDashboardResource() unstructured.Unstructured {
	dashboard := persesv1alpha1.PersesDashboard{
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

func ensurePersesDashboardCrdDoesNotExist(ctx context.Context) {
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
