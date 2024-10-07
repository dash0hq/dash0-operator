// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"

	"github.com/h2non/gock"
	persesv1alpha1 "github.com/perses/perses-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	crdReconciler *PersesDashboardCrdReconciler
	crd           *apiextensionsv1.CustomResourceDefinition

	crdQualifiedName = types.NamespacedName{
		Name: "persesdashboards.perses.dev",
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
		BeforeEach(func() {
			crdReconciler = &PersesDashboardCrdReconciler{
				AuthToken: AuthorizationTokenTest,

				// We create the controller multiple times in tests, this option is required, otherwise the controller
				// runtime will complain.
				skipNameValidation: true,
			}
		})

		AfterEach(func() {
			ensurePersesDashboardCrdDoesNotExist(ctx)
		})

		It("does not start watching Perses dashboards if the CRD does not exist", func() {
			Expect(crdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(crdReconciler.persesDashboardReconciler.isWatching).To(BeFalse())
		})

		It("starts watching Perses dashboards if the CRD exists", func() {
			ensurePersesDashboardCrdExists(ctx)
			Expect(crdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(crdReconciler.persesDashboardReconciler.isWatching).To(BeTrue())
		})
	})

	Describe("the Perses dashboard resource reconciler", func() {
		var persesDashboardReconciler *PersesDashboardReconciler

		BeforeAll(func() {
			crdReconciler = &PersesDashboardCrdReconciler{
				AuthToken: AuthorizationTokenTest,

				// We create the controller multiple times in tests, this option is required, otherwise the controller
				// runtime will complain.
				skipNameValidation: true,
			}
			ensurePersesDashboardCrdExists(ctx)

			Expect(crdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(crdReconciler.persesDashboardReconciler.isWatching).To(BeTrue())
		})

		BeforeEach(func() {
			crdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			})

			persesDashboardReconciler = crdReconciler.persesDashboardReconciler
		})

		AfterAll(func() {
			ensurePersesDashboardCrdDoesNotExist(ctx)
		})

		It("creates a Perses dashboard resource", func() {
			expectPutRequest()
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

		It("updates a Perses dashboard resource", func() {
			expectPutRequest()
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

		It("deletes a Perses dashboard resource", func() {
			expectDeleteRequest()
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
			expectPutRequest()
			defer gock.Off()

			crdReconciler.SetApiEndpointAndDataset(nil)

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

func expectPutRequest() {
	gock.New(ApiEndpointTest).
		Put("/api/dashboards/.*").
		MatchParam("dataset", DatasetTest).
		Reply(200).
		JSON(map[string]string{})
}

func expectDeleteRequest() {
	gock.New(ApiEndpointTest).
		Delete("/api/dashboards/.*").
		MatchParam("dataset", DatasetTest).
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
	crd_ := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		crdQualifiedName,
		&apiextensionsv1.CustomResourceDefinition{},
		&apiextensionsv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apiextensions.k8s.io/v1",
				Kind:       "CustomResourceDefinition",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "persesdashboards.perses.dev",
			},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: "perses.dev",
				Names: apiextensionsv1.CustomResourceDefinitionNames{
					Kind:     "PersesDashboard",
					ListKind: "PersesDashboardList",
					Plural:   "persesdashboards",
					Singular: "persesdashboard",
				},
				Scope: "Namespaced",
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
					{
						Name: "v1alpha1",
						Schema: &apiextensionsv1.CustomResourceValidation{
							OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
								Type: "object",
								Properties: map[string]apiextensionsv1.JSONSchemaProps{
									"apiVersion": {Type: "string"},
									"kind":       {Type: "string"},
									"metadata":   {Type: "object"},
									"spec":       {Type: "object"},
								},
								Required: []string{
									"kind",
									"spec",
								},
							},
						},
						Served:  true,
						Storage: true,
					},
				},
			},
		},
	)

	crd = crd_.(*apiextensionsv1.CustomResourceDefinition)
}

func ensurePersesDashboardCrdDoesNotExist(ctx context.Context) {
	if crd != nil {
		Expect(k8sClient.Delete(ctx, crd, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		})).To(Succeed())

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, crdQualifiedName, &apiextensionsv1.CustomResourceDefinition{})
			g.Expect(err).To(HaveOccurred())
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}).Should(Succeed())
	}

}
