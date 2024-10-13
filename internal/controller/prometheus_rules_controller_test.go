// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
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
	prometheusRuleCrdReconciler *PrometheusRuleCrdReconciler
	prometheusRuleCrd           *apiextensionsv1.CustomResourceDefinition

	checkRuleApiBasePath = "/api/alerting/check-rules/"

	defaultExpectedPathsCheckRules = []string{
		fmt.Sprintf("%s.*%s", checkRuleApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-rule_group_1_0"),
		fmt.Sprintf("%s.*%s", checkRuleApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-rule_group_1_1"),
		fmt.Sprintf("%s.*%s", checkRuleApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-rule_group_2_0"),
		fmt.Sprintf("%s.*%s", checkRuleApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-rule_group_2_1"),
	}
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
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler).To(BeNil())
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler).To(BeNil())
		})

		It("does not start watching Prometheus rules if the CRD does not exist and the API endpoint has not been provided", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
		})

		It("does not start watching Prometheus rules if the API endpoint has been provided but the CRD does not exist", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
		})

		It("does not start watching Prometheus rules if the CRD exists but the API endpoint has not been provided", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			ensurePrometheusRuleCrdExists(ctx)
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
		})

		It("starts watching Prometheus rules if the CRD exists and the API endpoint has been provided", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			ensurePrometheusRuleCrdExists(ctx)
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeTrue())
		})

		It("starts watching Prometheus rules if API endpoint is provided and the CRD is created later on", func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())

			// provide the API endpoint first
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)

			// create the CRD a bit later
			time.Sleep(100 * time.Millisecond)
			Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeFalse())
			ensurePrometheusRuleCrdExists(ctx)
			// watches are not triggered in unit tests
			prometheusRuleCrdReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: prometheusRuleCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			// verify that the controller starts watching when it sees the CRD being created
			Eventually(func(g Gomega) {
				g.Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeTrue())
			}).Should(Succeed())
		})
	})

	Describe("the Prometheus rule resource reconciler", func() {
		var prometheusRuleReconciler *PrometheusRuleReconciler

		BeforeAll(func() {
			createPrometheusRuleCrdReconcilerWithAuthToken()
			ensurePrometheusRuleCrdExists(ctx)

			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
		})

		BeforeEach(func() {
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetTest,
			}, &logger)
			Expect(prometheusRuleCrdReconciler.prometheusRuleReconciler.isWatching.Load()).To(BeTrue())
			prometheusRuleReconciler = prometheusRuleCrdReconciler.prometheusRuleReconciler
		})

		AfterAll(func() {
			ensurePrometheusRuleCrdDoesNotExist(ctx)
		})

		It("creates check rules", func() {
			expectRulePutRequests(defaultExpectedPathsCheckRules)
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

		It("updates check rules", func() {
			expectRulePutRequests(defaultExpectedPathsCheckRules)
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

		It("deletes check rules", func() {
			expectRuleDeleteRequests(defaultExpectedPathsCheckRules)
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
			expectRulePutRequests(defaultExpectedPathsCheckRules)
			defer gock.Off()

			prometheusRuleCrdReconciler.RemoveApiEndpointAndDataset()

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

	Describe("converting a single Prometheus rule to a check rule", func() {
		It("should convert an empty rule to nil", func() {
			rule, ok := convertRuleToCheckRule(
				prometheusv1.Rule{},
				upsert,
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeFalse())
			Expect(rule).To(BeNil())
		})

		It("should convert a record rule to nil", func() {
			rule, ok := convertRuleToCheckRule(
				prometheusv1.Rule{
					Record: "record",
					Alert:  "alert",
				},
				upsert,
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeFalse())
			Expect(rule).To(BeNil())
		})

		It("should convert a rule without alert to nil", func() {
			rule, ok := convertRuleToCheckRule(
				prometheusv1.Rule{
					Expr:          intstr.FromString("expr"),
					For:           ptr.To(prometheusv1.Duration("10s")),
					KeepFiringFor: ptr.To(prometheusv1.NonEmptyDuration("10s")),
				},
				upsert,
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeFalse())
			Expect(rule).To(BeNil())
		})

		It("should convert an almost empty rule", func() {
			rule, ok := convertRuleToCheckRule(
				prometheusv1.Rule{
					Alert: "alert",
				},
				upsert,
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeTrue())
			Expect(rule.Name).To(Equal("group - alert"))
			Expect(rule.Expression).To(Equal("0"))
			Expect(rule.For).To(Equal(""))
			Expect(rule.KeepFiringFor).To(Equal(""))
			Expect(rule.Annotations).To(BeEmpty())
			Expect(rule.Labels).To(BeEmpty())
		})

		It("should convert a rule with all attributes", func() {
			rule, ok := convertRuleToCheckRule(
				prometheusv1.Rule{
					Alert:         "alert",
					Expr:          intstr.FromString("expr"),
					For:           ptr.To(prometheusv1.Duration("10s")),
					KeepFiringFor: ptr.To(prometheusv1.NonEmptyDuration("20s")),
					Annotations: map[string]string{
						"annotation1": "annotation value 1",
						"annotation2": "annotation value 2",
					},
					Labels: map[string]string{
						"label1": "label value 1",
						"label2": "label value 2",
					},
				},
				upsert,
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeTrue())
			Expect(rule.Name).To(Equal("group - alert"))
			Expect(rule.Expression).To(Equal("expr"))
			Expect(rule.For).To(Equal("10s"))
			Expect(rule.KeepFiringFor).To(Equal("20s"))
			Expect(rule.Annotations).To(HaveLen(2))
			Expect(rule.Annotations["annotation1"]).To(Equal("annotation value 1"))
			Expect(rule.Annotations["annotation2"]).To(Equal("annotation value 2"))
			Expect(rule.Labels).To(HaveLen(2))
			Expect(rule.Labels["label1"]).To(Equal("label value 1"))
			Expect(rule.Labels["label2"]).To(Equal("label value 2"))
		})

		It("should convert a rule with an int expression", func() {
			rule, ok := convertRuleToCheckRule(
				prometheusv1.Rule{
					Alert: "alert",
					Expr:  intstr.FromInt32(123),
				},
				upsert,
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeTrue())
			Expect(rule.Name).To(Equal("group - alert"))
			Expect(rule.Expression).To(Equal("123"))
		})
	})

	Describe("converting Prometheus rule resources to http requests", func() {
		It("should convert an empty rule to nil", func() {
			req, ok := convertRuleToRequest(
				"https://api.dash0.com/alerting/check-rules/rule-id",
				upsert,
				prometheusv1.Rule{},
				&preconditionValidationResult{},
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeFalse())
			Expect(req).To(BeNil())
		})

		It("should convert a record rule to nil", func() {
			req, ok := convertRuleToRequest(
				"https://api.dash0.com/alerting/check-rules/rule-id",
				upsert,
				prometheusv1.Rule{
					Record: "record",
					Alert:  "alert",
				},
				&preconditionValidationResult{},
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeFalse())
			Expect(req).To(BeNil())
		})

		It("should convert a rule without alert to nil", func() {
			req, ok := convertRuleToRequest(
				"https://api.dash0.com/alerting/check-rules/rule-id",
				upsert,
				prometheusv1.Rule{
					Expr:          intstr.FromString("expr"),
					For:           ptr.To(prometheusv1.Duration("10s")),
					KeepFiringFor: ptr.To(prometheusv1.NonEmptyDuration("10s")),
				},
				&preconditionValidationResult{},
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeFalse())
			Expect(req).To(BeNil())
		})

		It("should convert an almost empty rule", func() {
			req, ok := convertRuleToRequest(
				"https://api.dash0.com/alerting/check-rules/rule-id",
				upsert,
				prometheusv1.Rule{
					Alert: "alert",
				},
				&preconditionValidationResult{},
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeTrue())
			Expect(req).ToNot(BeNil())
			Expect(req.URL.String()).To(Equal("https://api.dash0.com/alerting/check-rules/rule-id"))
		})

		It("should convert a rule with all attributes", func() {
			req, ok := convertRuleToRequest(
				"https://api.dash0.com/alerting/check-rules/rule-id",
				upsert,
				prometheusv1.Rule{
					Alert:         "alert",
					Expr:          intstr.FromString("expr"),
					For:           ptr.To(prometheusv1.Duration("10s")),
					KeepFiringFor: ptr.To(prometheusv1.NonEmptyDuration("20s")),
					Annotations: map[string]string{
						"annotation1": "annotation value 1",
						"annotation2": "annotation value 2",
					},
					Labels: map[string]string{
						"label1": "label value 1",
						"label2": "label value 2",
					},
				},
				&preconditionValidationResult{},
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeTrue())
			Expect(req).ToNot(BeNil())
			Expect(req.URL.String()).To(Equal("https://api.dash0.com/alerting/check-rules/rule-id"))
		})
	})
})

func createPrometheusRuleCrdReconcilerWithoutAuthToken() {
	prometheusRuleCrdReconciler = &PrometheusRuleCrdReconciler{
		// We create the controller multiple times in tests, this option is required, otherwise the controller
		// runtime will complain.
		skipNameValidation: true,
	}
}

func createPrometheusRuleCrdReconcilerWithAuthToken() {
	prometheusRuleCrdReconciler = &PrometheusRuleCrdReconciler{
		AuthToken: AuthorizationTokenTest,

		// We create the controller multiple times in tests, this option is required, otherwise the controller
		// runtime will complain.
		skipNameValidation: true,
	}
}

func expectRulePutRequests(expectedPaths []string) {
	for _, expectedPath := range expectedPaths {
		gock.New(ApiEndpointTest).
			Put(expectedPath).
			MatchParam("dataset", DatasetTest).
			Times(1).
			Reply(200).
			JSON(map[string]string{})
	}
}

func expectRuleDeleteRequests(expectedPaths []string) {
	for _, expectedPath := range expectedPaths {
		gock.New(ApiEndpointTest).
			Delete(expectedPath).
			MatchParam("dataset", DatasetTest).
			Times(1).
			Reply(200).
			JSON(map[string]string{})
	}
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
		Spec: prometheusv1.PrometheusRuleSpec{
			Groups: []prometheusv1.RuleGroup{
				{
					Name: "group_1",
					Rules: []prometheusv1.Rule{
						{
							Alert: "rule_1_1",
							Expr:  intstr.FromString("vector(1)"),
						},
						{
							Alert: "rule_1_2",
							Expr:  intstr.FromString("vector(1)"),
						},
						{
							Record: "rule_1_3",
							Expr:   intstr.FromString("vector(1)"),
						},
					},
				},
				{
					Name: "group_2",
					Rules: []prometheusv1.Rule{
						{
							Alert: "rule_2_1",
							Expr:  intstr.FromString("vector(1)"),
						},
						{
							Alert: "rule_2_2",
							Expr:  intstr.FromString("vector(1)"),
						},
					},
				},
			},
		},
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
