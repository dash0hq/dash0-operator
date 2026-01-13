// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	checkRuleOriginPattern = "dash0-operator_%s_test-dataset_test-namespace_test-rule_%s_%s"
)

var (
	prometheusRuleCrd        *apiextensionsv1.CustomResourceDefinition
	testQueuePrometheusRules = workqueue.NewTypedWithConfig(workqueue.TypedQueueConfig[ThirdPartyResourceSyncJob]{
		Name: "dash0-third-party-resource-synchronization-queue",
	})

	checkRuleApiBasePath = "/api/alerting/check-rules/"

	defaultRuleObjectMeta = metav1.ObjectMeta{
		Name:      "test-rule",
		Namespace: TestNamespaceName,
	}
)

type checkRuleRequestExpectation struct {
	group string
	alert string
}

var _ = Describe("The Prometheus rule controller", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	var clusterId string

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
		clusterId = string(util.ReadPseudoClusterUid(ctx, k8sClient, &logger))
	})

	Describe("the Prometheus rule CRD reconciler", func() {

		AfterEach(func() {
			deletePrometheusRuleCrdIfItExists(ctx)
		})

		It("does not start watching Prometheus rules if the CRD does not exist and neither API endpoint nor auth token have been provided", func() {
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Prometheus rules if the CRD does not exist and the auth token has not been provided", func() {
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Prometheus rules if the CRD does not exist and the API endpoint has not been provided", func() {
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			prometheusRuleCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Prometheus rules if the API endpoint & auth token have been provided but the CRD does not exist", func() {
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			prometheusRuleCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Prometheus rules if the CRD exists but the auth token has not been provided", func() {
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			ensurePrometheusRuleCrdExists(ctx)
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
		})

		It("does not start watching Prometheus rules if the CRD exists but the API endpoint has not been provided", func() {
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			ensurePrometheusRuleCrdExists(ctx)
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			prometheusRuleCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
		})

		It("starts watching Prometheus rules if the CRD exists and the API endpoint and auth token have been provided", func() {
			ensurePrometheusRuleCrdExists(ctx)
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			prometheusRuleCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
		})

		It("starts watching Prometheus rules if the API endpoint and auth token have been provided and the CRD is created later on", func() {
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())

			// provide the API endpoint and the auth token first
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			prometheusRuleCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)

			// create the CRD a bit later
			time.Sleep(100 * time.Millisecond)
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
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
				g.Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
			}).Should(Succeed())
		})

		It("stops watching Prometheus rules if the CRD is deleted", func() {
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			ensurePrometheusRuleCrdExists(ctx)
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
			prometheusRuleCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())

			deletePrometheusRuleCrdIfItExists(ctx)
			// watches are not triggered in unit tests
			prometheusRuleCrdReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[client.Object]{
					Object: prometheusRuleCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)
			Eventually(func(g Gomega) {
				Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
			}).Should(Succeed())
		})

		It("can cope with multiple consecutive create & delete events", func() {
			prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			prometheusRuleCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)

			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
			ensurePrometheusRuleCrdExists(ctx)
			prometheusRuleCrdReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: prometheusRuleCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)
			Eventually(func(g Gomega) {
				g.Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
			}).Should(Succeed())

			deletePrometheusRuleCrdIfItExists(ctx)
			prometheusRuleCrdReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[client.Object]{
					Object: prometheusRuleCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)
			Eventually(func(g Gomega) {
				g.Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
			}).Should(Succeed())

			ensurePrometheusRuleCrdExists(ctx)
			prometheusRuleCrdReconciler.Create(
				ctx,
				event.TypedCreateEvent[client.Object]{
					Object: prometheusRuleCrd,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)
			Eventually(func(g Gomega) {
				g.Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
			}).Should(Succeed())
		})
	})

	Describe("the Prometheus rule resource reconciler", func() {
		var prometheusRuleCrdReconciler *PrometheusRuleCrdReconciler
		var prometheusRuleReconciler *PrometheusRuleReconciler

		BeforeAll(func() {
			prometheusRuleCrdReconciler = createPrometheusRuleCrdReconciler()
			ensurePrometheusRuleCrdExists(ctx)

			Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, &logger)).To(Succeed())

			StartProcessingThirdPartySynchronizationQueue(testQueuePrometheusRules, &logger)
		})

		BeforeEach(func() {
			prometheusRuleCrdReconciler.SetApiEndpointAndDataset(ctx, &ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			}, &logger)
			prometheusRuleCrdReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
			Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
			prometheusRuleReconciler = prometheusRuleCrdReconciler.prometheusRuleReconciler
			// to make tests that involve http retries faster, we do not want to wait for one second for each retry
			prometheusRuleReconciler.overrideHttpRetryDelay(20 * time.Millisecond)
		})

		AfterEach(func() {
			DeleteMonitoringResourceIfItExists(ctx, k8sClient)
		})

		AfterAll(func() {
			deletePrometheusRuleCrdIfItExists(ctx)
			StopProcessingThirdPartySynchronizationQueue(testQueuePrometheusRules, &logger)
		})

		It("it ignores Prometheus rule resource changes if no Dash0 monitoring resource exists in the namespace", func() {
			expectRulePutRequests(clusterId, defaultCheckRuleRequests())
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
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

			expectRulePutRequests(clusterId, defaultCheckRuleRequests())
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyNoPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
			Expect(gock.IsPending()).To(BeTrue())
		})

		It("it ignores Prometheus rule resource changes if the API endpoint is not configured", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectRulePutRequests(clusterId, defaultCheckRuleRequests())
			defer gock.Off()

			prometheusRuleCrdReconciler.RemoveApiEndpointAndDataset(ctx, &logger)

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyNoPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
			Expect(gock.IsPending()).To(BeTrue())
		})

		It("creates check rules", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectFetchOriginsGetRequest(clusterId)
			expectRulePutRequests(clusterId, defaultCheckRuleRequests())
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPrometheusSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates check rules", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectFetchOriginsGetRequest(clusterId)
			expectRulePutRequests(clusterId, defaultCheckRuleRequests())
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectNew: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPrometheusSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates check rules if dash0.com/enable is set but not to \"false\"", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectFetchOriginsGetRequest(clusterId)
			expectRulePutRequests(clusterId, defaultCheckRuleRequests())
			defer gock.Off()

			ruleResource := createDefaultRuleResourceWithEnableLabel("whatever")
			prometheusRuleReconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectNew: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPrometheusSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes all check rules on Create (and does not try to create them) if labelled with dash0.com/enable=false", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectRuleDeleteRequestsWithHttpStatus(clusterId, defaultCheckRuleRequests(), http.StatusNotFound)
			defer gock.Off()

			ruleResource := createDefaultRuleResourceWithEnableLabel("false")
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPrometheusSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes all check rules on Update (and does not try to update them) if labelled with dash0.com/enable=false", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectRuleDeleteRequests(clusterId, defaultCheckRuleRequests())
			defer gock.Off()

			ruleResource := createDefaultRuleResourceWithEnableLabel("false")
			prometheusRuleReconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectNew: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPrometheusSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes individual check rules when the rule has been removed from the PrometheusRule resource", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectFetchOriginsGetRequest(clusterId)
			expectRulePutRequests(
				clusterId,
				[]checkRuleRequestExpectation{
					{
						group: "dash0/group-1",
						alert: "rule-1-1",
					},
					{
						group: "dash0/group-2",
						alert: "rule-2-1",
					},
					{
						group: "dash0/group-2",
						alert: "rule-2-2",
					},
				},
			)
			expectRuleDeleteRequests(clusterId, []checkRuleRequestExpectation{
				{
					group: "dash0/group-1",
					alert: "rule-1-2",
				},
			})
			defer gock.Off()

			spec := createDefaultSpec()
			spec.Groups[0].Rules = slices.Delete(spec.Groups[0].Rules, 1, 2)
			ruleResource := createRuleResource(spec)
			prometheusRuleReconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectNew: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0common.PrometheusRuleSynchronizationResult{
					SynchronizationStatus:  dash0common.ThirdPartySynchronizationStatusSuccessful,
					AlertingRulesTotal:     4,
					SynchronizedRulesTotal: 4,
					SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
						"dash0/group-1 - rule-1-1": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-1"),
							Dash0Dataset: DatasetCustomTest,
						},
						"dash0/group-2 - rule-2-1": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-1"),
							Dash0Dataset: DatasetCustomTest,
						},
						"dash0/group-2 - rule-2-2": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-2"),
							Dash0Dataset: DatasetCustomTest,
						},
						"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-2 (deleted)": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-2"),
							Dash0Dataset: DatasetCustomTest,
						},
					},
					SynchronizationErrorsTotal: 0,
					SynchronizationErrors:      nil,
					InvalidRulesTotal:          0,
					InvalidRules:               nil,
				},
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes individual check rules when the group has been removed from the PrometheusRule resource", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectFetchOriginsGetRequest(clusterId)
			expectRulePutRequests(
				clusterId,
				[]checkRuleRequestExpectation{
					{
						group: "dash0/group-1",
						alert: "rule-1-1",
					},
					{
						group: "dash0/group-1",
						alert: "rule-1-2",
					},
				},
			)
			expectRuleDeleteRequests(clusterId, []checkRuleRequestExpectation{
				{
					group: "dash0/group-2",
					alert: "rule-2-1",
				},
				{
					group: "dash0/group-2",
					alert: "rule-2-2",
				},
			},
			)
			defer gock.Off()

			spec := createDefaultSpec()
			spec.Groups = slices.Delete(spec.Groups, 1, 2)
			ruleResource := createRuleResource(spec)
			prometheusRuleReconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectNew: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0common.PrometheusRuleSynchronizationResult{
					SynchronizationStatus:  dash0common.ThirdPartySynchronizationStatusSuccessful,
					AlertingRulesTotal:     4,
					SynchronizedRulesTotal: 4,
					SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
						"dash0/group-1 - rule-1-1": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-1"),
							Dash0Dataset: DatasetCustomTest,
						},
						"dash0/group-1 - rule-1-2": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-2"),
							Dash0Dataset: DatasetCustomTest,
						},
						"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-1 (deleted)": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-1"),
							Dash0Dataset: DatasetCustomTest,
						},
						"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-2 (deleted)": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-2"),
							Dash0Dataset: DatasetCustomTest,
						},
					},
					SynchronizationErrorsTotal: 0,
					SynchronizationErrors:      nil,
					InvalidRulesTotal:          0,
					InvalidRules:               nil,
				},
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes individual check rules when the group in the PrometheusRule resource has been renamed", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectFetchOriginsGetRequest(clusterId)
			expectRulePutRequests(
				clusterId,
				[]checkRuleRequestExpectation{
					{
						group: "dash0/group-1",
						alert: "rule-1-1",
					},
					{
						group: "dash0/group-1",
						alert: "rule-1-2",
					},
					{
						group: "renamed",
						alert: "rule-2-1",
					},
					{
						group: "renamed",
						alert: "rule-2-2",
					},
				},
			)
			expectRuleDeleteRequests(
				clusterId,
				[]checkRuleRequestExpectation{
					{
						group: "dash0/group-2",
						alert: "rule-2-1",
					},
					{
						group: "dash0/group-2",
						alert: "rule-2-2",
					},
				},
			)
			defer gock.Off()

			spec := createDefaultSpec()
			spec.Groups[1].Name = "renamed"
			ruleResource := createRuleResource(spec)
			prometheusRuleReconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectNew: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0common.PrometheusRuleSynchronizationResult{
					SynchronizationStatus:  dash0common.ThirdPartySynchronizationStatusSuccessful,
					AlertingRulesTotal:     6,
					SynchronizedRulesTotal: 6,
					SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
						"dash0/group-1 - rule-1-1": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-1"),
							Dash0Dataset: DatasetCustomTest,
						},
						"dash0/group-1 - rule-1-2": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-2"),
							Dash0Dataset: DatasetCustomTest,
						},
						"renamed - rule-2-1": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "renamed", "rule-2-1"),
							Dash0Dataset: DatasetCustomTest,
						},
						"renamed - rule-2-2": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "renamed", "rule-2-2"),
							Dash0Dataset: DatasetCustomTest,
						},
						"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-1 (deleted)": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-1"),
							Dash0Dataset: DatasetCustomTest,
						},
						"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-2 (deleted)": {
							Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-2"),
							Dash0Dataset: DatasetCustomTest,
						},
					},
					SynchronizationErrorsTotal: 0,
					SynchronizationErrors:      nil,
					InvalidRulesTotal:          0,
					InvalidRules:               nil,
				},
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes all check rules when the whole PrometheusRule resource has been deleted", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectRuleDeleteRequestsWithHttpStatus(clusterId, defaultCheckRuleRequests(), http.StatusNotFound)
			defer gock.Off()

			ruleResource := createDefaultRuleResource()
			prometheusRuleReconciler.Delete(
				ctx,
				event.TypedDeleteEvent[*unstructured.Unstructured]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				defaultExpectedPrometheusSyncResult(clusterId),
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("reports validation issues and http errors for Prometheus rules", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectFetchOriginsGetRequest(clusterId)

			// successful requests (HTTP 200)
			for _, pathRegex := range []string{
				fmt.Sprintf("dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0\\|group-1_rule-1-3", clusterId),
				fmt.Sprintf("dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0\\|group-2_rule-2-1", clusterId),
			} {
				gock.New(ApiEndpointTest).
					Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, pathRegex)).
					MatchParam("dataset", DatasetCustomTest).
					Times(1).
					Reply(200).
					JSON(map[string]string{})
			}
			// failed requests (HTTP 401)
			for _, pathRegex := range []string{
				fmt.Sprintf("dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0\\|group-1_rule-1-2", clusterId),
				fmt.Sprintf("dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0\\|group-2_rule-2-3", clusterId),
			} {
				gock.New(ApiEndpointTest).
					Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, pathRegex)).
					MatchParam("dataset", DatasetCustomTest).
					Times(1).
					Reply(401).
					JSON(map[string]string{})
			}
			defer gock.Off()

			ruleResource := createRuleResource(
				prometheusv1.PrometheusRuleSpec{
					Groups: []prometheusv1.RuleGroup{
						{
							Name: "dash0/group-1",
							Rules: []prometheusv1.Rule{
								{
									Alert: "rule-1-1", // invalid due to missing threshold annotations
									Expr:  intstr.FromString("something something $__threshold something"),
								},
								{
									Alert: "rule-1-2", // PUT requests will receive HTTP 401
									Expr:  intstr.FromString("vector(1)"),
								},
								{
									Alert: "rule-1-3", // should be synchronized successfully
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
							Name: "dash0/group-2",
							Rules: []prometheusv1.Rule{
								{
									Alert: "rule-2-1", // should be synchronized successfully
									Expr:  intstr.FromString("vector(1)"),
								},
								{
									Alert: "rule-2-2", // invalid due to missing threshold annotations
									Expr:  intstr.FromString("something something $__threshold something"),
								},
								{
									Alert: "rule-2-3", // PUT requests will receive HTTP 401
									Expr:  intstr.FromString("vector(1)"),
								},
							},
						},
					},
				})
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0common.PrometheusRuleSynchronizationResult{
					SynchronizationStatus:  dash0common.ThirdPartySynchronizationStatusPartiallySuccessful,
					AlertingRulesTotal:     7,
					SynchronizedRulesTotal: 2,
					SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
						"dash0/group-1 - rule-1-3": {Dash0Origin: "", Dash0Dataset: ""},
						"dash0/group-2 - rule-2-1": {Dash0Origin: "", Dash0Dataset: ""},
					},
					SynchronizationErrorsTotal: 2,
					SynchronizationErrors: map[string]string{
						"dash0/group-1 - rule-1-2": "^unexpected status code 401 when synchronizing the rule \"dash0/group-1 - rule-1-2\": PUT https://api.dash0.com/api/alerting/check-rules/dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-2\\?dataset=test-dataset, response body is {}\n$",
						"dash0/group-2 - rule-2-3": "^unexpected status code 401 when synchronizing the rule \"dash0/group-2 - rule-2-3\": PUT https://api.dash0.com/api/alerting/check-rules/dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-3\\?dataset=test-dataset, response body is {}\n$",
					},
					InvalidRulesTotal: 3,
					InvalidRules: map[string][]string{
						"dash0/group-1 - rule-1-1": {thresholdAnnotationsMissingMessage()},
						"dash0/group-1 - 4":        {"rule has neither the alert nor the record attribute"},
						"dash0/group-2 - rule-2-2": {thresholdAnnotationsMissingMessage()},
					},
				},
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("reports as failed if no Prometheus rule is synchronized succcessul", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectFetchOriginsGetRequest(clusterId)

			// failed request, HTTP 401, no retry
			gock.New(ApiEndpointTest).
				Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, "dash0-operator_"+clusterId+"_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-2")).
				MatchParam("dataset", DatasetCustomTest).
				Times(1).
				Reply(401).
				JSON(map[string]string{})
			// failed request, HTTP 500, will be retried 3 times
			gock.New(ApiEndpointTest).
				Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, "dash0-operator_"+clusterId+"_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-2")).
				MatchParam("dataset", DatasetCustomTest).
				Times(3).
				Reply(500).
				JSON(map[string]string{})
			defer gock.Off()

			ruleResource := createRuleResource(
				prometheusv1.PrometheusRuleSpec{
					Groups: []prometheusv1.RuleGroup{
						{
							Name: "dash0/group-1",
							Rules: []prometheusv1.Rule{
								{
									Alert: "rule-1-1", // invalid due to missing threshold annotations
									Expr:  intstr.FromString("something something $__threshold something"),
								},
								{
									Alert: "rule-1-2", // PUT requests will receive HTTP 401
									Expr:  intstr.FromString("vector(1)"),
								},
								{
									Record: "rule-1-3", // will be ignored as it is a record rule
									Expr:   intstr.FromString("vector(1)"),
								},
								{
									// invalid, since it has neither record nor alert
									Expr: intstr.FromString("vector(1)"),
								},
							},
						},
						{
							Name: "dash0/group-2",
							Rules: []prometheusv1.Rule{
								{
									Alert: "rule-2-1", // invalid due to missing threshold annotations
									Expr:  intstr.FromString("something something $__threshold something"),
								},
								{
									Alert: "rule-2-2", // PUT requests will receive HTTP 500
									Expr:  intstr.FromString("vector(1)"),
								},
							},
						},
					},
				})
			prometheusRuleReconciler.Create(
				ctx,
				event.TypedCreateEvent[*unstructured.Unstructured]{
					Object: &ruleResource,
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
				ctx,
				k8sClient,
				dash0common.PrometheusRuleSynchronizationResult{
					SynchronizationStatus:       dash0common.ThirdPartySynchronizationStatusFailed,
					AlertingRulesTotal:          5,
					SynchronizedRulesTotal:      0,
					SynchronizedRulesAttributes: nil,
					SynchronizationErrorsTotal:  2,
					SynchronizationErrors: map[string]string{
						"dash0/group-1 - rule-1-2": "^unexpected status code 401 when synchronizing the rule \"dash0/group-1 - rule-1-2\": PUT https://api.dash0.com/api/alerting/check-rules/dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-2\\?dataset=test-dataset, response body is {}\n$",
						"dash0/group-2 - rule-2-2": "^unexpected status code 500 when synchronizing the rule \"dash0/group-2 - rule-2-2\": PUT https://api.dash0.com/api/alerting/check-rules/dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-2\\?dataset=test-dataset, response body is {}\n$",
					},
					InvalidRulesTotal: 3,
					InvalidRules: map[string][]string{
						"dash0/group-1 - rule-1-1": {thresholdAnnotationsMissingMessage()},
						"dash0/group-1 - 3":        {"rule has neither the alert nor the record attribute"},
						"dash0/group-2 - rule-2-1": {thresholdAnnotationsMissingMessage()},
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
				upsertAction,
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
				upsertAction,
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
				upsertAction,
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
				upsertAction,
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
				upsertAction,
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
				upsertAction,
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
					upsertAction,
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
			},
			// Note: running a focussed test in Idea, which uses --focus under the hood does not work when the
			// test label contains a "$" character. Thus, we refer to $__threshold as threshold in the test labels.
			// That is:
			//   go run github.com/onsi/ginkgo/v2/ginkgo -v "--focus=foo bar baz $__threshold whatever"
			// will run no tests at all.
			Entry(
				"expression with threshold, no annotations -> invalid",
				thresholdValidationTestConfig{
					annotations:              nil,
					expectedValidationIssues: []string{thresholdAnnotationsMissingMessage()},
				}),
			Entry(
				"expression with threshold, no threshold annotation -> invalid",
				thresholdValidationTestConfig{
					annotations:              map[string]string{"unrelated": "annotation"},
					expectedValidationIssues: []string{thresholdAnnotationsMissingMessage()},
				}),
			Entry(
				"expression with threshold, degraded annotation -> valid",
				thresholdValidationTestConfig{
					annotations: map[string]string{
						thresholdDegradedAnnotation: "10",
					},
					expectedValidationIssues: nil,
				}),
			Entry(
				"expression with threshold, legacy degraded annotation -> valid",
				thresholdValidationTestConfig{
					annotations: map[string]string{
						thresholdDegradedAnnotationLegacy: "10",
					},
					expectedValidationIssues: nil,
				}),
			Entry(
				"expression with threshold, criticial annotation -> valid",
				thresholdValidationTestConfig{
					annotations: map[string]string{
						thresholdCriticalAnnotation: "10",
					},
					expectedValidationIssues: nil,
				}),
			Entry(
				"expression with threshold, legacy criticial annotation -> valid",
				thresholdValidationTestConfig{
					annotations: map[string]string{
						thresholdCriticalAnnotationLegacy: "10",
					},
					expectedValidationIssues: nil,
				}),
			Entry(
				"expression with threshold, both annotations -> valid",
				thresholdValidationTestConfig{
					annotations: map[string]string{
						"unrelated":                 "annotation",
						thresholdDegradedAnnotation: "10",
						thresholdCriticalAnnotation: "5",
					},
					expectedValidationIssues: nil,
				}),
			Entry(
				"expression with threshold, both legacy annotations -> valid",
				thresholdValidationTestConfig{
					annotations: map[string]string{
						"unrelated":                       "annotation",
						thresholdDegradedAnnotationLegacy: "10",
						thresholdCriticalAnnotationLegacy: "5",
					},
					expectedValidationIssues: nil,
				}),
			Entry(
				"expression with threshold, mixed current and legacy annotations -> valid",
				thresholdValidationTestConfig{
					annotations: map[string]string{
						"unrelated":                       "annotation",
						thresholdDegradedAnnotation:       "10",
						thresholdCriticalAnnotationLegacy: "5",
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
		)
	})

	Describe("converting a PrometheusRule resource to multiple http requests", func() {
		var prometheusRuleReconciler *PrometheusRuleReconciler

		BeforeEach(func() {
			prometheusRuleReconciler = &PrometheusRuleReconciler{}
		})

		It("should create unique origins even if alert names are not unique", func() {
			prometheusRule := map[string]interface{}{}
			prometheusRuleYaml := `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: prometheus-rule
spec:
  groups:
  - name: group-1
    interval: 5m
    rules:
    - alert: alert-1
      expr: "expression 1"
    - alert: alert-2
      expr: "expression 2"
    - alert: alert-1
      expr: "expression 3"
    - alert: alert-1
      expr: "expression 4"
    - record: alert-2
      expr: "expression 5"
  - name: group-2
    interval: 10m
    rules:
    - alert: alert-1
      expr: "expression 6"
`
			Expect(yaml.Unmarshal([]byte(prometheusRuleYaml), &prometheusRule)).To(Succeed())
			preconditionValidationResult := &preconditionValidationResult{
				k8sName:      "prometheus-rule",
				k8sNamespace: TestNamespaceName,
				resource:     prometheusRule,
			}
			resourceToRequestsResult :=
				prometheusRuleReconciler.MapResourceToHttpRequests(
					preconditionValidationResult,
					upsertAction,
					&logger,
				)
			Expect(resourceToRequestsResult.ItemsTotal).To(Equal(5))
			Expect(resourceToRequestsResult.OriginsInResource).To(HaveLen(5))

			// verify uniqueness of origins
			origins := make(map[string]bool, len(resourceToRequestsResult.OriginsInResource))
			for _, origin := range resourceToRequestsResult.OriginsInResource {
				if origins[origin] {
					Fail("Duplicate origin string: " + origin)
				}
				origins[origin] = true
			}
			Expect(resourceToRequestsResult.OriginsInResource).To(ContainElements(
				"dash0-operator___test-namespace_prometheus-rule_group-1_alert-1",
				"dash0-operator___test-namespace_prometheus-rule_group-1_alert-2",
				"dash0-operator___test-namespace_prometheus-rule_group-1_alert-1_1",
				"dash0-operator___test-namespace_prometheus-rule_group-1_alert-1_2",
				"dash0-operator___test-namespace_prometheus-rule_group-2_alert-1",
			))

			Expect(resourceToRequestsResult.ValidationIssues).To(BeEmpty())
			Expect(resourceToRequestsResult.SynchronizationErrors).To(BeEmpty())
			Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(5))

			verifyCheckRuleRequest(
				resourceToRequestsResult.ApiRequests[0],
				"group-1 - alert-1",
				"/dash0-operator___test-namespace_prometheus-rule_group-1_alert-1",
				"expression 1",
			)
			verifyCheckRuleRequest(
				resourceToRequestsResult.ApiRequests[1],
				"group-1 - alert-2",
				"/dash0-operator___test-namespace_prometheus-rule_group-1_alert-2",
				"expression 2",
			)
			verifyCheckRuleRequest(
				resourceToRequestsResult.ApiRequests[2],
				"group-1 - alert-1",
				"/dash0-operator___test-namespace_prometheus-rule_group-1_alert-1_1",
				"expression 3",
			)
			verifyCheckRuleRequest(
				resourceToRequestsResult.ApiRequests[3],
				"group-1 - alert-1",
				"/dash0-operator___test-namespace_prometheus-rule_group-1_alert-1_2",
				"expression 4",
			)
			verifyCheckRuleRequest(
				resourceToRequestsResult.ApiRequests[4],
				"group-2 - alert-1",
				"/dash0-operator___test-namespace_prometheus-rule_group-2_alert-1",
				"expression 6",
			)
		})
	})

	Describe("converting a single Prometheus rule to an http request", func() {

		It("should ignore/skip a record rule", func() {
			req, validationIssues, syncError, ok := convertRuleToRequest(
				"https://api.dash0.com/alerting/check-rules/rule-origin",
				upsertAction,
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
				"https://api.dash0.com/alerting/check-rules/rule-origin",
				upsertAction,
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
				"https://api.dash0.com/alerting/check-rules/rule-origin",
				upsertAction,
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
				"https://api.dash0.com/alerting/check-rules/rule-origin",
				upsertAction,
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
				"https://api.dash0.com/alerting/check-rules/rule-origin",
				upsertAction,
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
			Expect(req.URL.String()).To(Equal("https://api.dash0.com/alerting/check-rules/rule-origin"))
		})

		It("should convert a rule with all attributes", func() {
			req, validationIssues, syncError, ok := convertRuleToRequest(
				"https://api.dash0.com/alerting/check-rules/rule-origin",
				upsertAction,
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
			Expect(req.URL.String()).To(Equal("https://api.dash0.com/alerting/check-rules/rule-origin"))
		})
	})
})

func createPrometheusRuleCrdReconciler() *PrometheusRuleCrdReconciler {
	crdReconciler := NewPrometheusRuleCrdReconciler(
		k8sClient,
		testQueuePrometheusRules,
		&DummyLeaderElectionAware{Leader: true},
		&http.Client{},
	)

	// We create the controller multiple times in tests, this option is required, otherwise the controller
	// runtime will complain.
	crdReconciler.skipNameValidation = true
	return crdReconciler
}

func expectFetchOriginsGetRequest(clusterId string) {
	gock.New(ApiEndpointTest).
		Get("/api/alerting/check-rules").
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		ParamPresent("idPrefix").
		Times(1).
		Reply(200).
		JSON([]map[string]string{
			{
				"origin": fmt.Sprintf("dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-1", clusterId),
			},
			{
				"origin": fmt.Sprintf("dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-2", clusterId),
			},
			{
				"origin": fmt.Sprintf("dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-1", clusterId),
			},
			{
				"origin": fmt.Sprintf("dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-2", clusterId),
			},
		})
}

func expectRulePutRequests(clusterId string, expectedRequests []checkRuleRequestExpectation) {
	for _, expectedRequest := range expectedRequests {
		origin :=
			fmt.Sprintf(
				"dash0-operator_%s_test-dataset_test-namespace_test-rule_%s_%s",
				clusterId,
				strings.ReplaceAll(expectedRequest.group, "/", "|"),
				expectedRequest.alert,
			)
		expectedPath := fmt.Sprintf("%s.*%s", checkRuleApiBasePath, origin)
		gock.New(ApiEndpointTest).
			Put(expectedPath).
			MatchHeader("Authorization", AuthorizationHeaderTest).
			MatchParam("dataset", DatasetCustomTest).
			Times(1).
			Reply(200).
			JSON(map[string]interface{}{
				"id":      origin,
				"dataset": DatasetCustomTest,
			})
	}
}

func expectRuleDeleteRequests(clusterId string, expectedRequests []checkRuleRequestExpectation) {
	expectRuleDeleteRequestsWithHttpStatus(clusterId, expectedRequests, http.StatusOK)
}

func expectRuleDeleteRequestsWithHttpStatus(clusterId string, expectedRequests []checkRuleRequestExpectation, status int) {
	for _, expectedRequest := range expectedRequests {
		origin :=
			fmt.Sprintf(
				"dash0-operator_%s_test-dataset_test-namespace_test-rule_%s_%s",
				clusterId,
				strings.ReplaceAll(expectedRequest.group, "/", "|"),
				expectedRequest.alert,
			)
		expectedPath := fmt.Sprintf("%s.*%s", checkRuleApiBasePath, origin)
		gock.New(ApiEndpointTest).
			Delete(expectedPath).
			MatchHeader("Authorization", AuthorizationHeaderTest).
			MatchParam("dataset", DatasetCustomTest).
			Times(1).
			Reply(status)
	}
}

func createDefaultRuleResource() unstructured.Unstructured {
	return createDefaultRuleResourceWithEnableLabel("")
}

func createDefaultRuleResourceWithEnableLabel(dash0EnableLabelValue string) unstructured.Unstructured {
	objectMeta := defaultRuleObjectMeta
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return createRuleResourceWithObjectMeta(createDefaultSpec(), objectMeta)
}

func createRuleResource(spec prometheusv1.PrometheusRuleSpec) unstructured.Unstructured {
	return createRuleResourceWithObjectMeta(spec, defaultRuleObjectMeta)
}

func createRuleResourceWithObjectMeta(spec prometheusv1.PrometheusRuleSpec, objectMeta metav1.ObjectMeta) unstructured.Unstructured {
	rule := prometheusv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "PrometheusRule",
		},
		ObjectMeta: objectMeta,
		Spec:       spec,
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
				Name: "dash0/group-1",
				Rules: []prometheusv1.Rule{
					{
						Alert: "rule-1-1",
						Expr:  intstr.FromString("vector(1)"),
					},
					{
						Alert: "rule-1-2",
						Expr:  intstr.FromString("vector(1)"),
					},
					{
						Record: "rule-1-3",
						Expr:   intstr.FromString("vector(1)"),
					},
				},
			},
			{
				Name: "dash0/group-2",
				Rules: []prometheusv1.Rule{
					{
						Alert: "rule-2-1",
						Expr:  intstr.FromString("vector(1)"),
					},
					{
						Alert: "rule-2-2",
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

func deletePrometheusRuleCrdIfItExists(ctx context.Context) {
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

func isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler *PrometheusRuleCrdReconciler) bool {
	ruleReconciler := prometheusRuleCrdReconciler.prometheusRuleReconciler
	ruleReconciler.ControllerStopFunctionLock().Lock()
	defer ruleReconciler.ControllerStopFunctionLock().Unlock()
	return ruleReconciler.IsWatching()
}

func defaultCheckRuleRequests() []checkRuleRequestExpectation {
	return []checkRuleRequestExpectation{
		{
			group: "dash0/group-1",
			alert: "rule-1-1",
		},
		{
			group: "dash0/group-1",
			alert: "rule-1-2",
		},
		{
			group: "dash0/group-2",
			alert: "rule-2-1",
		},
		{
			group: "dash0/group-2",
			alert: "rule-2-2",
		},
	}
}

func defaultExpectedPrometheusSyncResult(clusterId string) dash0common.PrometheusRuleSynchronizationResult {
	return dash0common.PrometheusRuleSynchronizationResult{
		SynchronizationStatus:  dash0common.ThirdPartySynchronizationStatusSuccessful,
		AlertingRulesTotal:     4,
		SynchronizedRulesTotal: 4,
		SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
			"dash0/group-1 - rule-1-1": {
				Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-1"),
				Dash0Dataset: DatasetCustomTest,
			},
			"dash0/group-1 - rule-1-2": {
				Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-2"),
				Dash0Dataset: DatasetCustomTest,
			},
			"dash0/group-2 - rule-2-1": {
				Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-1"),
				Dash0Dataset: DatasetCustomTest,
			},
			"dash0/group-2 - rule-2-2": {
				Dash0Origin:  fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-2"),
				Dash0Dataset: DatasetCustomTest,
			},
		},
		SynchronizationErrorsTotal: 0,
		SynchronizationErrors:      nil,
		InvalidRulesTotal:          0,
		InvalidRules:               nil,
	}
}

func verifyCheckRuleRequest(apiRequest WrappedApiRequest, itemName string, origin string, expression string) {
	Expect(apiRequest.ItemName).To(Equal(itemName))
	req := apiRequest.Request
	Expect(req.URL.Path).To(ContainSubstring(origin))
	verifyRequestBodyContains(req, expression)
}

func verifyRequestBodyContains(req *http.Request, substring string) {
	defer func() {
		_ = req.Body.Close()
	}()
	body, err := io.ReadAll(req.Body)
	Expect(err).ToNot(HaveOccurred())
	Expect(string(body)).To(ContainSubstring(substring))
}

func verifyPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
	expectedResult dash0common.PrometheusRuleSynchronizationResult,
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
