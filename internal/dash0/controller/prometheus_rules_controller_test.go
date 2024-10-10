// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"time"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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
	prometheusRulePrometheusRuleCrdReconciler *PrometheusRuleCrdReconciler
	prometheusRuleCrd                         *apiextensionsv1.CustomResourceDefinition
)

var _ = Describe("The Prometheus rule controller", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	Describe("the Prometheus rule CRD reconciler", func() {

		AfterEach(func() {
			ensurePrometheusRuleCrdDoesNotExist(ctx)
		})

		It("does not create a Prometheus rule resource reconciler if there is no auth token", func() {
			createPrometheusRuleCrdReconcilerWithoutAuthToken()
			ensurePrometheusRuleCrdExists(ctx)
			Expect(prometheusRulePrometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler).To(BeNil())
			prometheusRulePrometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler).To(BeNil())
		})

		It("does not start watching Prometheus rules if the CRD does not exist and the API endpoint has not been provided", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			Expect(prometheusRulePrometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
		})

		It("does not start watching Prometheus rules if the API endpoint has been provided but the CRD does not exist", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			Expect(prometheusRulePrometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			prometheusRulePrometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
		})

		It("does not start watching Prometheus rules if the CRD exists but the API endpoint has not been provided", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			ensurePrometheusRuleCrdExists(ctx)
			Expect(prometheusRulePrometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
		})

		It("starts watching Prometheus rules if the CRD exists and the API endpoint has been provided", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			ensurePrometheusRuleCrdExists(ctx)
			Expect(prometheusRulePrometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
			prometheusRulePrometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeTrue())
		})

		It("starts watching Prometheus rules if API endpoint is provided and the CRD is created later on", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			Expect(prometheusRulePrometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())

			// provide the API endpoint first
			prometheusRulePrometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)

			// create the CRD a bit later
			time.Sleep(100 * time.Millisecond)
			Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
			ensurePrometheusRuleCrdExists(ctx)
			// watches are not triggered in unit tests
			prometheusRulePrometheusRuleCrdReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: prometheusRuleCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			// verify that the controller starts watching when it sees the CRD being created
			Eventually(func(g Gomega) {
				g.Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeTrue())
			}).Should(Succeed())
		})
	})

	Describe("the Prometheus rule resource reconciler", func() {
		var prometheusRuleReconciler *PrometheusRuleReconciler

		BeforeAll(func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			ensurePrometheusRuleCrdExists(ctx)

			Expect(prometheusRulePrometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
		})

		BeforeEach(func() {
			prometheusRulePrometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeTrue())
			prometheusRuleReconciler = prometheusRulePrometheusRuleCrdReconciler.prometheusRuleReconciler
		})

		AfterAll(func() {
			ensurePrometheusRuleCrdDoesNotExist(ctx)
		})

		It("creates a Prometheus rule resource", func() {
			expectRulePutRequest()
			defer gock.Off()

			ruleResource := createRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates a Prometheus rule resource", func() {
			expectRulePutRequest()
			defer gock.Off()

			ruleResource := createRuleResource()
			prometheusRuleReconciler.Update(
				ctx,
				event.TypedUpdateEvent[client.Object]{
					ObjectNew: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a Prometheus rule resource", func() {
			expectRuleDeleteRequest()
			defer gock.Off()

			ruleResource := createRuleResource()
			prometheusRuleReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsDone()).To(BeTrue())
		})

		It("it ignores Prometheus rule resource changes if API endpoint is not configured", func() {
			expectRulePutRequest()
			defer gock.Off()

			prometheusRulePrometheusRuleCrdReconciler.RemoveApiEndpointAndDataset()

			ruleResource := createRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsPending()).To(BeTrue())
		})
	})
})

func createPrometheusRuleCrdReconcilerWithoutAuthToken() {
	prometheusRulePrometheusRuleCrdReconciler = &PrometheusRuleCrdReconciler{
		// We create the controller multiple times in tests, this option is required, otherwise the controller
		// runtime will complain.
		skipNameValidation: true,
	}
}

func createPrometheusRuleCrdReconcilerWithAuthToken() {
	prometheusRulePrometheusRuleCrdReconciler = &PrometheusRuleCrdReconciler{
		AuthToken: AuthorizationTokenTest,

		// We create the controller multiple times in tests, this option is required, otherwise the controller
		// runtime will complain.
		skipNameValidation: true,
	}
}

func expectRulePutRequest() {
	gock.New(ApiEndpointTest).
		Put("/api/alerting/check-rules/.*").
		MatchParam("dataset", DatasetTest).
		Reply(200).
		JSON(map[string]string{})
}

func expectRuleDeleteRequest() {
	gock.New(ApiEndpointTest).
		Delete("/api/alerting/check-rules/.*").
		MatchParam("dataset", DatasetTest).
		Reply(200).
		JSON(map[string]string{})
}

func createRuleResource() unstructured.Unstructured {
	rule := prometheusv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "PrometheusRule",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rule",
			Namespace: TestNamespaceName,
		},
		Spec: prometheusv1.PrometheusRuleSpec{},
	}
	marshalled, err := json.Marshal(rule)
	Expect(err).NotTo(HaveOccurred())
	unstructuredObject := unstructured.Unstructured{}
	err = json.Unmarshal(marshalled, &unstructuredObject)
	Expect(err).NotTo(HaveOccurred())
	return unstructuredObject
}

func ensurePrometheusRuleCrdExists(ctx context.Context) {
	prometheusRuleCrd = EnsurePrometheusRuleCrdExists(
		ctx,
		k8sClient,
	)
}

func ensurePrometheusRuleCrdDoesNotExist(ctx context.Context) {
	if prometheusRuleCrd != nil {
		err := k8sClient.Delete(ctx, prometheusRuleCrd, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		})
		if err != nil && apierrors.IsNotFound(err) {
			return
		} else if err != nil {
			Expect(err).NotTo(HaveOccurred())
		}

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, PrometheusRuleCrdQualifiedName, &apiextensionsv1.CustomResourceDefinition{})
			g.Expect(err).To(HaveOccurred())
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}).Should(Succeed())

		prometheusRuleCrd = nil
	}
}
