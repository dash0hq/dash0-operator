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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

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

	defaultExpectedPrometheusSyncResult = dash0v1alpha1.PrometheusRuleSynchronizationResult{
		SynchronizationStatus:  dash0v1alpha1.Successful,
		AlertingRulesTotal:     4,
		SynchronizedRulesTotal: 4,
		SynchronizedRules: []string{
			"group_1 - rule_1_1", "group_1 - rule_1_2", "group_2 - rule_2_1", "group_2 - rule_2_2",
		},
		SynchronizationErrorsTotal: 0,
		SynchronizationErrors:      nil,
		InvalidRulesTotal:          0,
		InvalidRules:               nil,
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
			// to make tests that involve http retries faster, we do not want to wait for one second for each retry
			prometheusRuleReconciler.overrideHttpRetryDelay(20 * time.Millisecond)
		})

		AfterEach(func() {
			DeleteMonitoringResourceIfItExists(ctx, k8sClient)
		})

		AfterAll(func() {
			ensurePrometheusRuleCrdDoesNotExist(ctx)
		})

		It("it ignores Prometheus rule resource changes if no Dash0 monitoring resource exists in the namespace", func() {
			expectRulePutRequests(defaultExpectedPathsCheckRules)
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(gock.IsPending()).To(BeTrue())
		})

		It("it ignores Prometheus rule resource changes if synchronization is disabled via the Dash0 monitoring resource", func() {
			monitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
			monitoringResource.Spec.SynchronizePrometheusRules = ptr.To(false)
			Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())

			expectRulePutRequests(defaultExpectedPathsCheckRules)
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyNoPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
			Expect(gock.IsPending()).To(BeTrue())
		})

		It("it ignores Prometheus rule resource changes if the API endpoint is not configured", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectRulePutRequests(defaultExpectedPathsCheckRules)
			defer gock.Off()

			prometheusRuleCrdReconciler.RemoveApiEndpointAndDataset()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyNoPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
			Expect(gock.IsPending()).To(BeTrue())
		})

		It("creates check rules", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectRulePutRequests(defaultExpectedPathsCheckRules)
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPrometheusSyncResult,
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates check rules", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectRulePutRequests(defaultExpectedPathsCheckRules)
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Update(
				ctx,
				event.TypedUpdateEvent[client.Object]{
					ObjectNew: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPrometheusSyncResult,
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes check rules", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectRuleDeleteRequests(defaultExpectedPathsCheckRules)
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPrometheusSyncResult,
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("reports validation issues and http errors for Prometheus rules", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			// successful requests (HTTP 200)
			for _, pathRegex := range []string{
				"dash0-operator_.*_test-dataset_test-namespace_test-rule_group_1_2",
				"dash0-operator_.*_test-dataset_test-namespace_test-rule_group_2_0",
			} {
				gock.New(ApiEndpointTest).
					Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, pathRegex)).
					MatchParam("dataset", DatasetTest).
					Times(1).
					Reply(200).
					JSON(map[string]string{})
			}
			// failed requests (HTTP 401)
			for _, pathRegex := range []string{
				"dash0-operator_.*_test-dataset_test-namespace_test-rule_group_1_1",
				"dash0-operator_.*_test-dataset_test-namespace_test-rule_group_2_2",
			} {
				gock.New(ApiEndpointTest).
					Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, pathRegex)).
					MatchParam("dataset", DatasetTest).
					Times(1).
					Reply(401).
					JSON(map[string]string{})
			}
			defer gock.Off()

			ruleResource := createRuleResource(
				prometheusv1.PrometheusRuleSpec{
					Groups: []prometheusv1.RuleGroup{
						{
							Name: "group_1",
							Rules: []prometheusv1.Rule{
								{
									Alert: "rule_1_1", // invalid due to missing threshold annotations
									Expr:  intstr.FromString("something something $__threshold something"),
								},
								{
									Alert: "rule_1_2", // PUT requests will receive HTTP 401
									Expr:  intstr.FromString("vector(1)"),
								},
								{
									Alert: "rule_1_3", // should be synchronized successfully
									Expr:  intstr.FromString("vector(1)"),
								},
								{
									Record: "rule_1_4", // will be ignored as it is a record rule
									Expr:   intstr.FromString("vector(1)"),
								},
								{
									// invalid, since it has neither record nor alert
									Expr: intstr.FromString("vector(1)"),
								},
							},
						},
						{
							Name: "group_2",
							Rules: []prometheusv1.Rule{
								{
									Alert: "rule_2_1", // should be synchronized successfully
									Expr:  intstr.FromString("vector(1)"),
								},
								{
									Alert: "rule_2_2", // invalid due to missing threshold annotations
									Expr:  intstr.FromString("something something $__threshold something"),
								},
								{
									Alert: "rule_2_3", // PUT requests will receive HTTP 401
									Expr:  intstr.FromString("vector(1)"),
								},
							},
						},
					},
				})
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0v1alpha1.PrometheusRuleSynchronizationResult{
					SynchronizationStatus:  dash0v1alpha1.PartiallySuccessful,
					AlertingRulesTotal:     7,
					SynchronizedRulesTotal: 2,
					SynchronizedRules: []string{
						"group_1 - rule_1_3",
						"group_2 - rule_2_1",
					},
					SynchronizationErrorsTotal: 2,
					SynchronizationErrors: map[string]string{
						"group_1 - rule_1_2": "^unexpected status code 401 when updating/creating/deleting the rule \"group_1 - rule_1_2\" at https://api.dash0.com/api/alerting/check-rules/dash0-operator_.*_test-dataset_test-namespace_test-rule_group_1_1\\?dataset=test-dataset, response body is {}\n$",
						"group_2 - rule_2_3": "^unexpected status code 401 when updating/creating/deleting the rule \"group_2 - rule_2_3\" at https://api.dash0.com/api/alerting/check-rules/dash0-operator_.*_test-dataset_test-namespace_test-rule_group_2_2\\?dataset=test-dataset, response body is {}\n$",
					},
					InvalidRulesTotal: 3,
					InvalidRules: map[string][]string{
						"group_1 - rule_1_1": {thresholdAnnotationsMissingMessage()},
						"group_1 - 4":        {"rule has neither the alert nor the record attribute"},
						"group_2 - rule_2_2": {thresholdAnnotationsMissingMessage()},
					},
				},
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("reports as failed if no Prometheus rule is synchronized succcessul", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			// failed request, HTTP 401, no retry
			gock.New(ApiEndpointTest).
				Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-rule_group_1_1")).
				MatchParam("dataset", DatasetTest).
				Times(1).
				Reply(401).
				JSON(map[string]string{})
			// failed request, HTTP 500, will be retried 3 times
			gock.New(ApiEndpointTest).
				Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-rule_group_2_1")).
				MatchParam("dataset", DatasetTest).
				Times(3).
				Reply(500).
				JSON(map[string]string{})
			defer gock.Off()

			ruleResource := createRuleResource(
				prometheusv1.PrometheusRuleSpec{
					Groups: []prometheusv1.RuleGroup{
						{
							Name: "group_1",
							Rules: []prometheusv1.Rule{
								{
									Alert: "rule_1_1", // invalid due to missing threshold annotations
									Expr:  intstr.FromString("something something $__threshold something"),
								},
								{
									Alert: "rule_1_2", // PUT requests will receive HTTP 401
									Expr:  intstr.FromString("vector(1)"),
								},
								{
									Record: "rule_1_3", // will be ignored as it is a record rule
									Expr:   intstr.FromString("vector(1)"),
								},
								{
									// invalid, since it has neither record nor alert
									Expr: intstr.FromString("vector(1)"),
								},
							},
						},
						{
							Name: "group_2",
							Rules: []prometheusv1.Rule{
								{
									Alert: "rule_2_1", // invalid due to missing threshold annotations
									Expr:  intstr.FromString("something something $__threshold something"),
								},
								{
									Alert: "rule_2_2", // PUT requests will receive HTTP 500
									Expr:  intstr.FromString("vector(1)"),
								},
							},
						},
					},
				})
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0v1alpha1.PrometheusRuleSynchronizationResult{
					SynchronizationStatus:      dash0v1alpha1.Failed,
					AlertingRulesTotal:         5,
					SynchronizedRulesTotal:     0,
					SynchronizedRules:          nil,
					SynchronizationErrorsTotal: 2,
					SynchronizationErrors: map[string]string{
						"group_1 - rule_1_2": "^unexpected status code 401 when updating/creating/deleting the rule \"group_1 - rule_1_2\" at https://api.dash0.com/api/alerting/check-rules/dash0-operator_.*_test-dataset_test-namespace_test-rule_group_1_1\\?dataset=test-dataset, response body is {}\n$",
						"group_2 - rule_2_2": "^unexpected status code 500 when updating/creating/deleting the rule \"group_2 - rule_2_2\" at https://api.dash0.com/api/alerting/check-rules/dash0-operator_.*_test-dataset_test-namespace_test-rule_group_2_1\\?dataset=test-dataset, response body is {}\n$",
					},
					InvalidRulesTotal: 3,
					InvalidRules: map[string][]string{
						"group_1 - rule_1_1": {thresholdAnnotationsMissingMessage()},
						"group_1 - 3":        {"rule has neither the alert nor the record attribute"},
						"group_2 - rule_2_1": {thresholdAnnotationsMissingMessage()},
					},
				},
			)
			Expect(gock.IsDone()).To(BeTrue())
		})
	})

	Describe("converting a single Prometheus rule to a check rule", func() {
		It("should ignore/skip record rules", func() {
			rule, validationIssues, ok := convertRuleToCheckRule(
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
			Expect(validationIssues).To(BeEmpty())
			Expect(rule).To(BeNil())
		})

		It("should treat an empty rule as invalid", func() {
			rule, validationIssues, ok := convertRuleToCheckRule(
				prometheusv1.Rule{},
				upsert,
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeFalse())
			Expect(validationIssues).To(HaveLen(1))
			Expect(validationIssues).To(ContainElement("rule has neither the alert nor the record attribute"))
			Expect(rule).To(BeNil())
		})

		It("should treat a rule without alert or record as invalid", func() {
			rule, validationIssues, ok := convertRuleToCheckRule(
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
			Expect(validationIssues).To(HaveLen(1))
			Expect(validationIssues).To(ContainElement("rule has neither the alert nor the record attribute"))
			Expect(rule).To(BeNil())
		})

		It("should convert an almost empty rule", func() {
			rule, validationIssues, ok := convertRuleToCheckRule(
				prometheusv1.Rule{
					Alert: "alert",
				},
				upsert,
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeTrue())
			Expect(validationIssues).To(BeEmpty())
			Expect(rule.Name).To(Equal("group - alert"))
			Expect(rule.Expression).To(Equal("0"))
			Expect(rule.For).To(Equal(""))
			Expect(rule.KeepFiringFor).To(Equal(""))
			Expect(rule.Annotations).To(BeEmpty())
			Expect(rule.Labels).To(BeEmpty())
		})

		It("should convert a rule with all attributes", func() {
			rule, validationIssues, ok := convertRuleToCheckRule(
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
			Expect(validationIssues).To(BeEmpty())
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
			rule, validationIssues, ok := convertRuleToCheckRule(
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
			Expect(validationIssues).To(BeEmpty())
			Expect(rule.Name).To(Equal("group - alert"))
			Expect(rule.Expression).To(Equal("123"))
		})

		type thresholdValidationTestConfig struct {
			annotations              map[string]string
			expectedValidationIssues []string
		}

		DescribeTable(
			"threshold validation",
			func(config thresholdValidationTestConfig) {
				rule, validationIssues, ok := convertRuleToCheckRule(
					prometheusv1.Rule{
						Alert:       "alert",
						Expr:        intstr.FromString("foobar $__threshold baz"),
						Annotations: config.annotations,
					},
					upsert,
					"group",
					ptr.To(prometheusv1.Duration("10m")),
					&logger,
				)

				if config.expectedValidationIssues == nil {
					Expect(ok).To(BeTrue())
					Expect(validationIssues).To(BeNil())
					Expect(rule).ToNot(BeNil())
				} else {
					Expect(ok).To(BeFalse())
					Expect(validationIssues).To(Equal(config.expectedValidationIssues))
					Expect(rule).To(BeNil())
				}
			}, []TableEntry{
				Entry(
					"expression with $__threshold, no annotations -> invalid",
					thresholdValidationTestConfig{
						annotations:              nil,
						expectedValidationIssues: []string{thresholdAnnotationsMissingMessage()},
					}),
				Entry(
					"expression with $__threshold, no threshold annotation -> invalid",
					thresholdValidationTestConfig{
						annotations:              map[string]string{"unrelated": "annotation"},
						expectedValidationIssues: []string{thresholdAnnotationsMissingMessage()},
					}),
				Entry(
					"expression with $__threshold, degraded annotation -> valid",
					thresholdValidationTestConfig{
						annotations: map[string]string{
							thresholdDegradedAnnotation: "10",
						},
						expectedValidationIssues: nil,
					}),
				Entry(
					"expression with $__threshold, criticial annotation -> valid",
					thresholdValidationTestConfig{
						annotations: map[string]string{
							thresholdCriticalAnnotation: "10",
						},
						expectedValidationIssues: nil,
					}),
				Entry(
					"expression with $__threshold, both annotations -> valid",
					thresholdValidationTestConfig{
						annotations: map[string]string{
							"unrelated":                 "annotation",
							thresholdDegradedAnnotation: "10",
							thresholdCriticalAnnotation: "5",
						},
						expectedValidationIssues: nil,
					}),
				Entry(
					"degraded annotation is not numerical -> invalid",
					thresholdValidationTestConfig{
						annotations: map[string]string{
							thresholdDegradedAnnotation: "1s",
						},
						expectedValidationIssues: []string{thresholdAnnotationsDegradedNonNumericalMessage("1s")},
					}),
				Entry(
					"critical annotation is not numerical -> invalid",
					thresholdValidationTestConfig{
						annotations: map[string]string{
							thresholdCriticalAnnotation: "abc",
						},
						expectedValidationIssues: []string{thresholdAnnotationsCriticalNonNumericalMessage("abc")},
					}),
				Entry(
					"both annotation are not numerical -> invalid",
					thresholdValidationTestConfig{
						annotations: map[string]string{
							thresholdDegradedAnnotation: "1s",
							thresholdCriticalAnnotation: "abc",
						},
						expectedValidationIssues: []string{
							thresholdAnnotationsDegradedNonNumericalMessage("1s"),
							thresholdAnnotationsCriticalNonNumericalMessage("abc"),
						},
					}),
			})
	})

	Describe("converting Prometheus rule resources to http requests", func() {
		It("should ignore/skip a record rule", func() {
			req, validationIssues, syncError, ok := convertRuleToRequest(
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
			Expect(validationIssues).To(BeEmpty())
			Expect(syncError).To(BeNil())
			Expect(req).To(BeNil())
		})

		It("should treat an empty rule as invalid", func() {
			req, validationIssues, syncError, ok := convertRuleToRequest(
				"https://api.dash0.com/alerting/check-rules/rule-id",
				upsert,
				prometheusv1.Rule{},
				&preconditionValidationResult{},
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeFalse())
			Expect(validationIssues).To(HaveLen(1))
			Expect(validationIssues).To(ContainElement("rule has neither the alert nor the record attribute"))
			Expect(syncError).To(BeNil())
			Expect(req).To(BeNil())
		})

		It("should treat a rule without alert or record as invalid", func() {
			req, validationIssues, syncError, ok := convertRuleToRequest(
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
			Expect(validationIssues).To(HaveLen(1))
			Expect(validationIssues).To(ContainElement("rule has neither the alert nor the record attribute"))
			Expect(syncError).To(BeNil())
			Expect(req).To(BeNil())
		})

		It("should treat a rule with empty expression as invalid", func() {
			req, validationIssues, syncError, ok := convertRuleToRequest(
				"https://api.dash0.com/alerting/check-rules/rule-id",
				upsert,
				prometheusv1.Rule{
					Alert: "alert",
					Expr:  intstr.FromString(""),
				},
				&preconditionValidationResult{},
				"group",
				ptr.To(prometheusv1.Duration("10m")),
				&logger,
			)

			Expect(ok).To(BeFalse())
			Expect(validationIssues).To(HaveLen(1))
			Expect(validationIssues).To(ContainElement("the rule has no expression attribute (or the expression attribute is empty)"))
			Expect(syncError).To(BeNil())
			Expect(req).To(BeNil())
		})

		It("should convert an almost empty rule", func() {
			req, validationIssues, syncError, ok := convertRuleToRequest(
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
			Expect(validationIssues).To(BeEmpty())
			Expect(syncError).To(BeNil())
			Expect(req).ToNot(BeNil())
			Expect(req.URL.String()).To(Equal("https://api.dash0.com/alerting/check-rules/rule-id"))
		})

		It("should convert a rule with all attributes", func() {
			req, validationIssues, syncError, ok := convertRuleToRequest(
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
			Expect(validationIssues).To(BeEmpty())
			Expect(syncError).To(BeNil())
			Expect(req).ToNot(BeNil())
			Expect(req.URL.String()).To(Equal("https://api.dash0.com/alerting/check-rules/rule-id"))
		})
	})
})

func createPrometheusRuleCrdReconcilerWithoutAuthToken() {
	prometheusRuleCrdReconciler = &PrometheusRuleCrdReconciler{
		Client: k8sClient,

		// We create the controller multiple times in tests, this option is required, otherwise the controller
		// runtime will complain.
		skipNameValidation: true,
	}
}

func createPrometheusRuleCrdReconcilerWithAuthToken() {
	prometheusRuleCrdReconciler = &PrometheusRuleCrdReconciler{
		Client:    k8sClient,
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

func createDefaultRuleResource() unstructured.Unstructured {
	return createRuleResource(createDefaultSpec())
}

func createRuleResource(spec prometheusv1.PrometheusRuleSpec) unstructured.Unstructured {
	rule := prometheusv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "PrometheusRule",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rule",
			Namespace: TestNamespaceName,
		},
		Spec: spec,
	}
	marshalled, err := json.Marshal(rule)
	Expect(err).NotTo(HaveOccurred())
	unstructuredObject := unstructured.Unstructured{}
	err = json.Unmarshal(marshalled, &unstructuredObject)
	Expect(err).NotTo(HaveOccurred())
	return unstructuredObject
}

func createDefaultSpec() prometheusv1.PrometheusRuleSpec {
	return prometheusv1.PrometheusRuleSpec{
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
	}
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

func verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
	expectedResult dash0v1alpha1.PrometheusRuleSynchronizationResult,
) {
	Eventually(func(g Gomega) {
		monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
		results := monRes.Status.PrometheusRuleSynchronizationResults
		g.Expect(results).NotTo(BeNil())
		g.Expect(results).To(HaveLen(1))
		result := results[fmt.Sprintf("%s/%s", TestNamespaceName, "test-rule")]
		g.Expect(result).NotTo(BeNil())

		if len(expectedResult.SynchronizationErrors) > 0 {
			// http errors contain a different random path for each run
			g.Expect(result.SynchronizationErrors).To(HaveLen(len(expectedResult.SynchronizationErrors)))
			for _, expectedSyncErrRegex := range expectedResult.SynchronizationErrors {
				g.Expect(result.SynchronizationErrors).To(ContainElement(MatchRegexp(expectedSyncErrRegex)))
			}
			expectedResult.SynchronizationErrors = nil
			result.SynchronizationErrors = nil
		}

		// we do not verify the exact timestamp
		expectedResult.SynchronizedAt = result.SynchronizedAt

		g.Expect(result).To(Equal(expectedResult))
	}).Should(Succeed())
}

func verifyNoPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
) {
	Consistently(func(g Gomega) {
		monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
		results := monRes.Status.PrometheusRuleSynchronizationResults
		g.Expect(results).To(BeNil())
	}, 200*time.Millisecond, 50*time.Millisecond).Should(Succeed())
}

func thresholdAnnotationsMissingMessage() string {
	return fmt.Sprintf(
		thresholdAnnotationsMissingMessagePattern,
		thresholdReference,
		thresholdDegradedAnnotation,
		thresholdCriticalAnnotation,
	)
}

func thresholdAnnotationsDegradedNonNumericalMessage(value string) string {
	return fmt.Sprintf(thresholdAnnotationsNonNumericalMessagePattern, thresholdReference, "degraded", value)
}

func thresholdAnnotationsCriticalNonNumericalMessage(value string) string {
	return fmt.Sprintf(thresholdAnnotationsNonNumericalMessagePattern, thresholdReference, "critical", value)
}
