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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	checkRuleOriginPattern            = "dash0-operator_%s_test-dataset_test-namespace_test-rule_%s_%s"
	checkRuleOriginPatternAlternative = "dash0-operator_%s_test-dataset-alt_test-namespace_test-rule_%s_%s"
)

var (
	prometheusRuleCrd        *apiextensionsv1.CustomResourceDefinition
	testQueuePrometheusRules = workqueue.NewTypedWithConfig(
		workqueue.TypedQueueConfig[ThirdPartyResourceSyncJob]{
			Name: "dash0-third-party-resource-synchronization-queue",
		},
	)

	checkRuleApiBasePath     = "/api/alerting/check-rules/"
	recordingRuleApiBasePath = "/api/recording-rules/"

	defaultRuleObjectMeta = metav1.ObjectMeta{
		Name:      "test-rule",
		Namespace: TestNamespaceName,
	}
)

type checkRuleRequestExpectation struct {
	group string
	alert string
}

var _ = Describe(
	"The Prometheus rule controller", Ordered, func() {
		ctx := context.Background()
		logger := logd.FromContext(ctx)
		var clusterId string

		BeforeAll(
			func() {
				EnsureTestNamespaceExists(ctx, k8sClient)
				EnsureOperatorNamespaceExists(ctx, k8sClient)
				clusterId = string(util.ReadPseudoClusterUid(ctx, k8sClient, logger))
			},
		)

		Describe(
			"the Prometheus rule CRD reconciler", func() {

				AfterEach(
					func() {
						deletePrometheusRuleCrdIfItExists(ctx)
					},
				)

				It(
					"does not start watching Prometheus rules if the CRD does not exist and neither API endpoint nor auth token have been provided",
					func() {
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
					},
				)

				It(
					"does not start watching Prometheus rules if the CRD does not exist and the auth token has not been provided",
					func() {
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
								},
							}, logger,
						)
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
					},
				)

				It(
					"does not start watching Prometheus rules if the CRD does not exist and the API endpoint has not been provided",
					func() {
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Token: AuthorizationTokenTest,
								},
							}, logger,
						)
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
					},
				)

				It(
					"does not start watching Prometheus rules if the API endpoint & auth token have been provided but the CRD does not exist",
					func() {
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							}, logger,
						)
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
					},
				)

				It(
					"does not start watching Prometheus rules if the CRD exists but the auth token has not been provided",
					func() {
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						ensurePrometheusRuleCrdExists(ctx)
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
								},
							}, logger,
						)
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
					},
				)

				It(
					"does not start watching Prometheus rules if the CRD exists but the API endpoint has not been provided",
					func() {
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						ensurePrometheusRuleCrdExists(ctx)
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Token: AuthorizationTokenTest,
								},
							}, logger,
						)
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
					},
				)

				It(
					"starts watching Prometheus rules if the CRD exists and the API endpoint and auth token have been provided",
					func() {
						ensurePrometheusRuleCrdExists(ctx)
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							}, logger,
						)
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
					},
				)

				It(
					"starts watching Prometheus rules if the API endpoint and auth token have been provided and the CRD is created later on",
					func() {
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())

						// provide the API endpoint and the auth token first
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							}, logger,
						)

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
						Eventually(
							func(g Gomega) {
								g.Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
							},
						).Should(Succeed())
					},
				)

				It(
					"stops watching Prometheus rules if the CRD is deleted", func() {
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						ensurePrometheusRuleCrdExists(ctx)
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							}, logger,
						)
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
						Eventually(
							func(g Gomega) {
								Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
							},
						).Should(Succeed())
					},
				)

				It(
					"tracks per-namespace sync-enabled state and clears it on removal",
					func() {
						crdReconciler := createPrometheusRuleCrdReconciler()
						Expect(crdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						reconciler := crdReconciler.prometheusRuleReconciler
						namespace := TestNamespaceName

						syncDisabled := &dash0v1beta1.Dash0Monitoring{}
						syncDisabled.Spec.SynchronizePrometheusRules = new(bool) // false

						syncEnabled := &dash0v1beta1.Dash0Monitoring{}
						// nil means default=true

						By("First call with sync disabled — stores false, no resync")
						crdReconciler.SetSynchronizationEnabled(ctx, namespace, syncDisabled, logger)
						val, ok := reconciler.namespacedSyncEnabled.Load(namespace)
						Expect(ok).To(BeTrue())
						Expect(val).To(BeFalse())

						By("Second call still disabled — no state change")
						crdReconciler.SetSynchronizationEnabled(ctx, namespace, syncDisabled, logger)
						val, _ = reconciler.namespacedSyncEnabled.Load(namespace)
						Expect(val).To(BeFalse())

						By("Transition to enabled — state updated")
						crdReconciler.SetSynchronizationEnabled(ctx, namespace, syncEnabled, logger)
						val, _ = reconciler.namespacedSyncEnabled.Load(namespace)
						Expect(val).To(BeTrue())

						By("Already enabled — no state change")
						crdReconciler.SetSynchronizationEnabled(ctx, namespace, syncEnabled, logger)
						val, _ = reconciler.namespacedSyncEnabled.Load(namespace)
						Expect(val).To(BeTrue())

						By("RemoveNamespacedApiConfigs does not clear the sync-enabled state")
						reconciler.namespacedApiConfigs.Set(namespace, []ApiConfig{{Endpoint: "x", Token: "t"}})
						crdReconciler.RemoveNamespacedApiConfigs(ctx, namespace, logger)
						val, _ = reconciler.namespacedSyncEnabled.Load(namespace)
						Expect(val).To(BeTrue())

						By("RemoveSynchronizationEnabled clears the sync-enabled state")
						crdReconciler.RemoveSynchronizationEnabled(namespace)
						_, ok = reconciler.namespacedSyncEnabled.Load(namespace)
						Expect(ok).To(BeFalse())
					},
				)

				It(
					"can cope with multiple consecutive create & delete events", func() {
						prometheusRuleCrdReconciler := createPrometheusRuleCrdReconciler()
						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							}, logger,
						)

						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
						ensurePrometheusRuleCrdExists(ctx)
						prometheusRuleCrdReconciler.Create(
							ctx,
							event.TypedCreateEvent[client.Object]{
								Object: prometheusRuleCrd,
							},
							&controllertest.TypedQueue[reconcile.Request]{},
						)
						Eventually(
							func(g Gomega) {
								g.Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
							},
						).Should(Succeed())

						deletePrometheusRuleCrdIfItExists(ctx)
						prometheusRuleCrdReconciler.Delete(
							ctx,
							event.TypedDeleteEvent[client.Object]{
								Object: prometheusRuleCrd,
							},
							&controllertest.TypedQueue[reconcile.Request]{},
						)
						Eventually(
							func(g Gomega) {
								g.Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeFalse())
							},
						).Should(Succeed())

						ensurePrometheusRuleCrdExists(ctx)
						prometheusRuleCrdReconciler.Create(
							ctx,
							event.TypedCreateEvent[client.Object]{
								Object: prometheusRuleCrd,
							},
							&controllertest.TypedQueue[reconcile.Request]{},
						)
						Eventually(
							func(g Gomega) {
								g.Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
							},
						).Should(Succeed())
					},
				)
			},
		)

		Describe(
			"the Prometheus rule resource reconciler", func() {
				var prometheusRuleCrdReconciler *PrometheusRuleCrdReconciler
				var prometheusRuleReconciler *PrometheusRuleReconciler

				BeforeAll(
					func() {
						prometheusRuleCrdReconciler = createPrometheusRuleCrdReconciler()
						ensurePrometheusRuleCrdExists(ctx)

						Expect(prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, k8sClient, logger)).To(Succeed())

						StartProcessingThirdPartySynchronizationQueue(testQueuePrometheusRules, logger)
					},
				)

				BeforeEach(
					func() {
						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
							}, logger,
						)
						Expect(isWatchingPrometheusRuleResources(prometheusRuleCrdReconciler)).To(BeTrue())
						prometheusRuleReconciler = prometheusRuleCrdReconciler.prometheusRuleReconciler
					},
				)

				AfterEach(
					func() {
						DeleteMonitoringResourceIfItExists(ctx, k8sClient)
						prometheusRuleCrdReconciler.RemoveNamespacedApiConfigs(ctx, TestNamespaceName, logger)
					},
				)

				AfterAll(
					func() {
						deletePrometheusRuleCrdIfItExists(ctx)
						StopProcessingThirdPartySynchronizationQueue(testQueuePrometheusRules, logger)
					},
				)

				It(
					"it ignores Prometheus rule resource changes if no Dash0 monitoring resource exists in the namespace",
					func() {
						expectCheckRulePutRequests(clusterId, defaultCheckRuleRequests())
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						defer gock.Off()

						ruleResource := createDefaultPrometheusRuleResource()
						prometheusRuleReconciler.Create(
							ctx,
							event.TypedCreateEvent[*unstructured.Unstructured]{
								Object: &ruleResource,
							},
							&controllertest.TypedQueue[reconcile.Request]{},
						)

						Expect(gock.IsPending()).To(BeTrue())
					},
				)

				It(
					"it ignores Prometheus rule resource changes if synchronization is disabled via the Dash0 monitoring resource",
					func() {
						monitoringResource := EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)
						monitoringResource.Spec.SynchronizePrometheusRules = new(false)
						Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())

						expectCheckRulePutRequests(clusterId, defaultCheckRuleRequests())
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						defer gock.Off()

						ruleResource := createDefaultPrometheusRuleResource()
						prometheusRuleReconciler.Create(
							ctx,
							event.TypedCreateEvent[*unstructured.Unstructured]{
								Object: &ruleResource,
							},
							&controllertest.TypedQueue[reconcile.Request]{},
						)

						verifyNoPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
						Expect(gock.IsPending()).To(BeTrue())
					},
				)

				It(
					"it ignores Prometheus rule resource changes if the API endpoint is not configured", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectCheckRulePutRequests(clusterId, defaultCheckRuleRequests())
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						defer gock.Off()

						prometheusRuleCrdReconciler.RemoveDefaultApiConfigs(ctx, logger)

						ruleResource := createDefaultPrometheusRuleResource()
						prometheusRuleReconciler.Create(
							ctx,
							event.TypedCreateEvent[*unstructured.Unstructured]{
								Object: &ruleResource,
							},
							&controllertest.TypedQueue[reconcile.Request]{},
						)

						verifyNoPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(ctx, k8sClient)
						Expect(gock.IsPending()).To(BeTrue())
					},
				)

				It(
					"creates check rules", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectFetchOriginsGetRequest(clusterId)
						expectCheckRulePutRequests(clusterId, defaultCheckRuleRequests())
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						defer gock.Off()

						ruleResource := createDefaultPrometheusRuleResource()
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
					},
				)

				It(
					"creates check rules with namespaced config from the monitoring resource", func() {
						monitoringResource := DefaultMonitoringResourceWithCustomApiConfigAndToken(
							MonitoringResourceQualifiedName,
							ApiEndpointTestAlternative,
							DatasetCustomTestAlternative,
							AuthorizationTokenTestAlternative,
						)
						EnsureMonitoringResourceWithSpecExistsAndIsAvailable(ctx, k8sClient, monitoringResource.Spec)

						expectFetchOriginsGetRequestCustom(
							clusterId,
							ApiEndpointTestAlternative,
							AuthorizationHeaderTestAlternative,
							DatasetCustomTestAlternative,
						)
						expectCheckRulePutRequestsCustom(
							clusterId,
							ApiEndpointTestAlternative,
							AuthorizationHeaderTestAlternative,
							DatasetCustomTestAlternative,
							defaultCheckRuleRequests(),
						)
						expectRecordingRulePutRequestsCustom(
							clusterId,
							ApiEndpointTestAlternative,
							AuthorizationHeaderTestAlternative,
							DatasetCustomTestAlternative,
							defaultRecordingRuleRequests(),
						)
						defer gock.Off()

						prometheusRuleCrdReconciler.SetNamespacedApiConfigs(
							ctx, TestNamespaceName, []ApiConfig{
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, logger,
						)

						ruleResource := createDefaultPrometheusRuleResource()
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
							expectedPrometheusSyncResult(
								clusterId,
								ApiEndpointStandardizedTestAlternative,
								DatasetCustomTestAlternative,
								checkRuleOriginPatternAlternative,
							),
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates check rules with two API configs (multi-export)", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// Expect GET + PUT requests for both API configs
						expectFetchOriginsGetRequest(clusterId)
						expectCheckRulePutRequests(clusterId, defaultCheckRuleRequests())
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						expectFetchOriginsGetRequestCustom(
							clusterId,
							ApiEndpointTestAlternative,
							AuthorizationHeaderTestAlternative,
							DatasetCustomTestAlternative,
						)
						expectCheckRulePutRequestsCustom(
							clusterId,
							ApiEndpointTestAlternative,
							AuthorizationHeaderTestAlternative,
							DatasetCustomTestAlternative,
							defaultCheckRuleRequests(),
						)
						expectRecordingRulePutRequestsCustom(
							clusterId,
							ApiEndpointTestAlternative,
							AuthorizationHeaderTestAlternative,
							DatasetCustomTestAlternative,
							defaultRecordingRuleRequests(),
						)
						defer gock.Off()

						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, logger,
						)

						ruleResource := createDefaultPrometheusRuleResource()
						prometheusRuleReconciler.Create(
							ctx,
							event.TypedCreateEvent[*unstructured.Unstructured]{
								Object: &ruleResource,
							},
							&controllertest.TypedQueue[reconcile.Request]{},
						)

						verifyPrometheusRuleMultiExportSynchronizationResult(
							ctx,
							k8sClient,
							dash0common.PrometheusRuleSynchronizationResult{
								SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusSuccessful,
								AlertingRulesTotal:    4,
								RecordingRulesTotal:   1,
								InvalidRulesTotal:     0,
								InvalidRules:          nil,
								SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
									{
										Dash0ApiEndpoint:       ApiEndpointStandardizedTest,
										Dash0Dataset:           DatasetCustomTest,
										SynchronizedRulesTotal: 5,
										SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
											"dash0/group-1 - rule-1-1": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-1"),
											},
											"dash0/group-1 - rule-1-2": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-2"),
											},
											"dash0/group-1 - rule-1-3": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-3"),
											},
											"dash0/group-2 - rule-2-1": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-1"),
											},
											"dash0/group-2 - rule-2-2": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-2"),
											},
										},
										SynchronizationErrorsTotal: 0,
										SynchronizationErrors:      nil,
									},
									{
										Dash0ApiEndpoint:       ApiEndpointStandardizedTestAlternative,
										Dash0Dataset:           DatasetCustomTestAlternative,
										SynchronizedRulesTotal: 5,
										SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
											"dash0/group-1 - rule-1-1": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPatternAlternative, clusterId, "dash0|group-1", "rule-1-1"),
											},
											"dash0/group-1 - rule-1-2": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPatternAlternative, clusterId, "dash0|group-1", "rule-1-2"),
											},
											"dash0/group-1 - rule-1-3": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPatternAlternative, clusterId, "dash0|group-1", "rule-1-3"),
											},
											"dash0/group-2 - rule-2-1": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPatternAlternative, clusterId, "dash0|group-2", "rule-2-1"),
											},
											"dash0/group-2 - rule-2-2": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPatternAlternative, clusterId, "dash0|group-2", "rule-2-2"),
											},
										},
										SynchronizationErrorsTotal: 0,
										SynchronizationErrors:      nil,
									},
								},
							},
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"creates check rules with two API configs where one fails (partially-successful)", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// First API config succeeds
						expectFetchOriginsGetRequest(clusterId)
						expectCheckRulePutRequests(clusterId, defaultCheckRuleRequests())
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())

						// Second API config fails (all PUT requests return 503)
						expectFetchOriginsGetRequestCustom(
							clusterId,
							ApiEndpointTestAlternative,
							AuthorizationHeaderTestAlternative,
							DatasetCustomTestAlternative,
						)
						for _, expectedRequest := range defaultCheckRuleRequests() {
							origin :=
								fmt.Sprintf(
									"dash0-operator_%s_%s_test-namespace_test-rule_%s_%s",
									clusterId,
									DatasetCustomTestAlternative,
									strings.ReplaceAll(expectedRequest.group, "/", "|"),
									expectedRequest.alert,
								)
							expectedPath := fmt.Sprintf("%s.*%s", checkRuleApiBasePath, origin)
							gock.New(ApiEndpointTestAlternative).
								Put(expectedPath).
								MatchHeader("Authorization", AuthorizationHeaderTestAlternative).
								MatchParam("dataset", DatasetCustomTestAlternative).
								Times(3). // 3 retries
								Reply(503).
								JSON(map[string]string{})
						}
						defer gock.Off()

						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, logger,
						)

						ruleResource := createDefaultPrometheusRuleResource()
						prometheusRuleReconciler.Create(
							ctx,
							event.TypedCreateEvent[*unstructured.Unstructured]{
								Object: &ruleResource,
							},
							&controllertest.TypedQueue[reconcile.Request]{},
						)

						Eventually(
							func(g Gomega) {
								monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
								results := monRes.Status.PrometheusRuleSynchronizationResults
								g.Expect(results).NotTo(BeNil())
								g.Expect(results).To(HaveLen(1))
								result := results[fmt.Sprintf("%s/%s", TestNamespaceName, "test-rule")]
								g.Expect(result.SynchronizationStatus).To(
									Equal(dash0common.ThirdPartySynchronizationStatusPartiallySuccessful),
								)
								g.Expect(result.SynchronizationResults).To(HaveLen(2))
							},
						).Should(Succeed())

						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes check rules with two API configs (multi-export)", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						// Expect DELETE requests for both API configs
						expectCheckRuleDeleteRequests(clusterId, defaultCheckRuleRequests())
						expectCheckRuleDeleteRequestsCustom(
							clusterId,
							ApiEndpointTestAlternative,
							AuthorizationHeaderTestAlternative,
							DatasetCustomTestAlternative,
							defaultCheckRuleRequests(),
							http.StatusOK,
						)
						defer gock.Off()

						prometheusRuleCrdReconciler.SetDefaultApiConfigs(
							ctx, []ApiConfig{
								{
									Endpoint: ApiEndpointTest,
									Dataset:  DatasetCustomTest,
									Token:    AuthorizationTokenTest,
								},
								{
									Endpoint: ApiEndpointTestAlternative,
									Dataset:  DatasetCustomTestAlternative,
									Token:    AuthorizationTokenTestAlternative,
								},
							}, logger,
						)

						ruleResource := createDefaultPrometheusRuleResource()
						prometheusRuleReconciler.Delete(
							ctx,
							event.TypedDeleteEvent[*unstructured.Unstructured]{
								Object: &ruleResource,
							},
							&controllertest.TypedQueue[reconcile.Request]{},
						)

						Eventually(
							func(g Gomega) {
								g.Expect(gock.IsDone()).To(BeTrue())
							},
						).Should(Succeed())
					},
				)

				It(
					"updates check rules", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectFetchOriginsGetRequest(clusterId)
						expectCheckRulePutRequests(clusterId, defaultCheckRuleRequests())
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						defer gock.Off()

						ruleResource := createDefaultPrometheusRuleResource()
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
					},
				)

				It(
					"updates check rules if dash0.com/enable is set but not to \"false\"", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectFetchOriginsGetRequest(clusterId)
						expectCheckRulePutRequests(clusterId, defaultCheckRuleRequests())
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						defer gock.Off()

						ruleResource := createDefaultPrometheusRuleResourceWithEnableLabel("whatever")
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
					},
				)

				It(
					"deletes all check rules on Create (and does not try to create them) if labelled with dash0.com/enable=false",
					func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectCheckRuleDeleteRequestsWithHttpStatus(clusterId, defaultCheckRuleRequests(), http.StatusNotFound)
						expectRecordingRuleDeleteRequests(clusterId, defaultRecordingRuleRequests())
						defer gock.Off()

						ruleResource := createDefaultPrometheusRuleResourceWithEnableLabel("false")
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
					},
				)

				It(
					"deletes all check rules on Update (and does not try to update them) if labelled with dash0.com/enable=false",
					func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectCheckRuleDeleteRequests(clusterId, defaultCheckRuleRequests())
						expectRecordingRuleDeleteRequests(clusterId, defaultRecordingRuleRequests())
						defer gock.Off()

						ruleResource := createDefaultPrometheusRuleResourceWithEnableLabel("false")
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
					},
				)

				It(
					"deletes individual check rules when the rule has been removed from the PrometheusRule resource", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectFetchOriginsGetRequest(clusterId)
						expectCheckRulePutRequests(
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
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						orphanedRules := []checkRuleRequestExpectation{
							{
								group: "dash0/group-1",
								alert: "rule-1-2",
							},
						}
						expectCheckRuleDeleteRequests(clusterId, orphanedRules)
						// The dual-delete sends DELETEs to both APIs; the recording-rules API returns 404 for
						// alerting rule orphans, which is handled gracefully.
						expectRecordingRuleDeleteRequestsWithHttpStatus(clusterId, orphanedRules, http.StatusNotFound)
						defer gock.Off()

						spec := createDefaultSpec()
						spec.Groups[0].Rules = slices.Delete(spec.Groups[0].Rules, 1, 2)
						ruleResource := createPrometheusRuleResource(spec)
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
								SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusSuccessful,
								// 4 upserts (3 alerting + 1 recording) + 2 deletes (orphan sent to both APIs)
								AlertingRulesTotal:  3,
								RecordingRulesTotal: 1,
								InvalidRulesTotal:   0,
								InvalidRules:        nil,
								SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
									{
										Dash0ApiEndpoint: ApiEndpointStandardizedTest,
										Dash0Dataset:     DatasetCustomTest,
										// Both deletes have the same ItemName so the map deduplicates them.
										SynchronizedRulesTotal: 6,
										SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
											"dash0/group-1 - rule-1-1": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-1"),
											},
											"dash0/group-1 - rule-1-3": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-3"),
											},
											"dash0/group-2 - rule-2-1": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-1"),
											},
											"dash0/group-2 - rule-2-2": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-2"),
											},
											"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-2 (deleted)": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-2"),
											},
										},
										SynchronizationErrorsTotal: 0,
										SynchronizationErrors:      nil,
									},
								},
							},
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes individual check rules when the group has been removed from the PrometheusRule resource", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectFetchOriginsGetRequest(clusterId)
						expectCheckRulePutRequests(
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
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						orphanedRules := []checkRuleRequestExpectation{
							{
								group: "dash0/group-2",
								alert: "rule-2-1",
							},
							{
								group: "dash0/group-2",
								alert: "rule-2-2",
							},
						}
						expectCheckRuleDeleteRequests(clusterId, orphanedRules)
						expectRecordingRuleDeleteRequestsWithHttpStatus(clusterId, orphanedRules, http.StatusNotFound)
						defer gock.Off()

						spec := createDefaultSpec()
						spec.Groups = slices.Delete(spec.Groups, 1, 2)
						ruleResource := createPrometheusRuleResource(spec)
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
								SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusSuccessful,
								AlertingRulesTotal:    2,
								RecordingRulesTotal:   1,
								InvalidRulesTotal:     0,
								InvalidRules:          nil,
								SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
									{
										Dash0ApiEndpoint:       ApiEndpointStandardizedTest,
										Dash0Dataset:           DatasetCustomTest,
										SynchronizedRulesTotal: 7,
										SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
											"dash0/group-1 - rule-1-1": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-1"),
											},
											"dash0/group-1 - rule-1-2": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-2"),
											},
											"dash0/group-1 - rule-1-3": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-3"),
											},
											"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-1 (deleted)": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-1"),
											},
											"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-2 (deleted)": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-2"),
											},
										},
										SynchronizationErrorsTotal: 0,
										SynchronizationErrors:      nil,
									},
								},
							},
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes individual check rules when the group in the PrometheusRule resource has been renamed", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectFetchOriginsGetRequest(clusterId)
						expectCheckRulePutRequests(
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
						expectRecordingRulePutRequests(clusterId, defaultRecordingRuleRequests())
						orphanedRules := []checkRuleRequestExpectation{
							{
								group: "dash0/group-2",
								alert: "rule-2-1",
							},
							{
								group: "dash0/group-2",
								alert: "rule-2-2",
							},
						}
						expectCheckRuleDeleteRequests(clusterId, orphanedRules)
						// The dual-delete sends DELETEs to both APIs; the recording-rules API returns 404 for
						// alerting rule orphans, which is handled gracefully.
						expectRecordingRuleDeleteRequestsWithHttpStatus(clusterId, orphanedRules, http.StatusNotFound)
						defer gock.Off()

						spec := createDefaultSpec()
						spec.Groups[1].Name = "renamed"
						ruleResource := createPrometheusRuleResource(spec)
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
								SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusSuccessful,
								AlertingRulesTotal:    4,
								RecordingRulesTotal:   1,
								InvalidRulesTotal:     0,
								InvalidRules:          nil,
								SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
									{
										Dash0ApiEndpoint:       ApiEndpointStandardizedTest,
										Dash0Dataset:           DatasetCustomTest,
										SynchronizedRulesTotal: 9,
										SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
											"dash0/group-1 - rule-1-1": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-1"),
											},
											"dash0/group-1 - rule-1-2": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-2"),
											},
											"dash0/group-1 - rule-1-3": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-1", "rule-1-3"),
											},
											"renamed - rule-2-1": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "renamed", "rule-2-1"),
											},
											"renamed - rule-2-2": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "renamed", "rule-2-2"),
											},
											"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-1 (deleted)": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-1"),
											},
											"dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-2 (deleted)": {
												Dash0Origin: fmt.Sprintf(checkRuleOriginPattern, clusterId, "dash0|group-2", "rule-2-2"),
											},
										},
										SynchronizationErrorsTotal: 0,
										SynchronizationErrors:      nil,
									},
								},
							},
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"deletes all check rules when the whole PrometheusRule resource has been deleted", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectCheckRuleDeleteRequestsWithHttpStatus(clusterId, defaultCheckRuleRequests(), http.StatusNotFound)
						expectRecordingRuleDeleteRequestsWithHttpStatus(clusterId, defaultRecordingRuleRequests(), http.StatusNotFound)
						defer gock.Off()

						ruleResource := createDefaultPrometheusRuleResource()
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
					},
				)

				It(
					"reports validation issues and http errors for Prometheus rules", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectFetchOriginsGetRequest(clusterId)

						// successful check rule requests (HTTP 200)
						for _, pathRegex := range []string{
							fmt.Sprintf(
								"dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0\\|group-1_rule-1-3",
								clusterId,
							),
							fmt.Sprintf(
								"dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0\\|group-2_rule-2-1",
								clusterId,
							),
						} {
							gock.New(ApiEndpointTest).
								Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, pathRegex)).
								MatchParam("dataset", DatasetCustomTest).
								Times(1).
								Reply(200).
								JSON(map[string]string{})
						}
						// successful recording rule requests (HTTP 200)
						expectRecordingRulePutRequests(clusterId, []checkRuleRequestExpectation{
							{
								group: "dash0/group-1",
								alert: "rule_1_4",
							},
						})
						// failed requests (HTTP 401)
						for _, pathRegex := range []string{
							fmt.Sprintf(
								"dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0\\|group-1_rule-1-2",
								clusterId,
							),
							fmt.Sprintf(
								"dash0-operator_%s_test-dataset_test-namespace_test-rule_dash0\\|group-2_rule-2-3",
								clusterId,
							),
						} {
							gock.New(ApiEndpointTest).
								Put(fmt.Sprintf("%s.*%s", checkRuleApiBasePath, pathRegex)).
								MatchParam("dataset", DatasetCustomTest).
								Times(1).
								Reply(401).
								JSON(map[string]string{})
						}
						defer gock.Off()

						ruleResource := createPrometheusRuleResource(
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
												Record: "rule_1_4", // recording rule, should be synchronized successfully
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
							},
						)
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
								SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusPartiallySuccessful,
								AlertingRulesTotal:    7,
								RecordingRulesTotal:   1,
								InvalidRulesTotal:     3,
								InvalidRules: map[string][]string{
									"dash0/group-1 - rule-1-1": {thresholdAnnotationsMissingMessage()},
									"dash0/group-1 - 4":        {"rule has neither the alert nor the record attribute"},
									"dash0/group-2 - rule-2-2": {thresholdAnnotationsMissingMessage()},
								},
								SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
									{
										Dash0ApiEndpoint:       ApiEndpointStandardizedTest,
										Dash0Dataset:           DatasetCustomTest,
										SynchronizedRulesTotal: 3,
										SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
											"dash0/group-1 - rule-1-3": {
												Dash0Origin: fmt.Sprintf(
													checkRuleOriginPattern,
													clusterId,
													"dash0|group-1",
													"rule-1-3",
												),
											},
											"dash0/group-1 - rule_1_4": {
												Dash0Origin: fmt.Sprintf(
													checkRuleOriginPattern,
													clusterId,
													"dash0|group-1",
													"rule_1_4",
												),
											},
											"dash0/group-2 - rule-2-1": {
												Dash0Origin: fmt.Sprintf(
													checkRuleOriginPattern,
													clusterId,
													"dash0|group-2",
													"rule-2-1",
												),
											},
										},
										SynchronizationErrorsTotal: 2,
										SynchronizationErrors: map[string]string{
											"dash0/group-1 - rule-1-2": "^unexpected status code 401 when synchronizing the rule \"dash0/group-1 - rule-1-2\": PUT https://api.dash0.com/api/alerting/check-rules/dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-2\\?dataset=test-dataset, response body is {}\n$",
											"dash0/group-2 - rule-2-3": "^unexpected status code 401 when synchronizing the rule \"dash0/group-2 - rule-2-3\": PUT https://api.dash0.com/api/alerting/check-rules/dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-3\\?dataset=test-dataset, response body is {}\n$",
										},
									},
								},
							},
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)

				It(
					"reports as failed if no Prometheus rule is synchronized succcessul", func() {
						EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)

						expectFetchOriginsGetRequest(clusterId)

						// failed request, HTTP 401, no retry
						gock.New(ApiEndpointTest).
							Put(
								fmt.Sprintf(
									"%s.*%s",
									checkRuleApiBasePath,
									"dash0-operator_"+clusterId+"_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-2",
								),
							).
							MatchParam("dataset", DatasetCustomTest).
							Times(1).
							Reply(401).
							JSON(map[string]string{})
						// failed recording rule request, HTTP 401, no retry
						gock.New(ApiEndpointTest).
							Put(
								fmt.Sprintf(
									"%s.*%s",
									recordingRuleApiBasePath,
									"dash0-operator_"+clusterId+"_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-3",
								),
							).
							MatchParam("dataset", DatasetCustomTest).
							Times(1).
							Reply(401).
							JSON(map[string]string{})
						// failed request, HTTP 500, will be retried 3 times
						gock.New(ApiEndpointTest).
							Put(
								fmt.Sprintf(
									"%s.*%s",
									checkRuleApiBasePath,
									"dash0-operator_"+clusterId+"_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-2",
								),
							).
							MatchParam("dataset", DatasetCustomTest).
							Times(3).
							Reply(500).
							JSON(map[string]string{})
						defer gock.Off()

						ruleResource := createPrometheusRuleResource(
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
												Record: "rule-1-3", // recording rule, PUT requests will receive HTTP 401
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
							},
						)
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
								SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
								AlertingRulesTotal:    5,
								RecordingRulesTotal:   1,
								InvalidRulesTotal:     3,
								InvalidRules: map[string][]string{
									"dash0/group-1 - rule-1-1": {thresholdAnnotationsMissingMessage()},
									"dash0/group-1 - 3":        {"rule has neither the alert nor the record attribute"},
									"dash0/group-2 - rule-2-1": {thresholdAnnotationsMissingMessage()},
								},
								SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
									{
										Dash0ApiEndpoint:            ApiEndpointStandardizedTest,
										Dash0Dataset:                DatasetCustomTest,
										SynchronizedRulesTotal:      0,
										SynchronizedRulesAttributes: nil,
										SynchronizationErrorsTotal:  3,
										SynchronizationErrors: map[string]string{
											"dash0/group-1 - rule-1-2": "^unexpected status code 401 when synchronizing the rule \"dash0/group-1 - rule-1-2\": PUT https://api.dash0.com/api/alerting/check-rules/dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-2\\?dataset=test-dataset, response body is {}\n$",
											"dash0/group-1 - rule-1-3": "^unexpected status code 401 when synchronizing the rule \"dash0/group-1 - rule-1-3\": PUT https://api.dash0.com/api/recording-rules/dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-1_rule-1-3\\?dataset=test-dataset, response body is {}\n$",
											"dash0/group-2 - rule-2-2": "^unexpected status code 500 when synchronizing the rule \"dash0/group-2 - rule-2-2\": PUT https://api.dash0.com/api/alerting/check-rules/dash0-operator_" + clusterId + "_test-dataset_test-namespace_test-rule_dash0|group-2_rule-2-2\\?dataset=test-dataset, response body is {}\n$",
										},
									},
								},
							},
						)
						Expect(gock.IsDone()).To(BeTrue())
					},
				)
			},
		)

		Context(
			"converting a single Prometheus rule to a check rule", func() {
				It(
					"should treat an empty rule as invalid", func() {
						rule, validationIssues, ok := convertAlertingRuleToCheckRule(
							prometheusv1.Rule{},
							upsertAction,
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeFalse())
						Expect(validationIssues).To(HaveLen(1))
						Expect(validationIssues).To(ContainElement("rule has neither the alert nor the record attribute"))
						Expect(rule).To(BeNil())
					},
				)

				It(
					"should treat a rule without alert or record as invalid", func() {
						rule, validationIssues, ok := convertAlertingRuleToCheckRule(
							prometheusv1.Rule{
								Expr:          intstr.FromString("expr"),
								For:           ptr.To(prometheusv1.Duration("10s")),
								KeepFiringFor: ptr.To(prometheusv1.NonEmptyDuration("10s")),
							},
							upsertAction,
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeFalse())
						Expect(validationIssues).To(HaveLen(1))
						Expect(validationIssues).To(ContainElement("rule has neither the alert nor the record attribute"))
						Expect(rule).To(BeNil())
					},
				)

				It(
					"should convert an almost empty rule", func() {
						rule, validationIssues, ok := convertAlertingRuleToCheckRule(
							prometheusv1.Rule{
								Alert: "alert",
							},
							upsertAction,
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeTrue())
						Expect(validationIssues).To(BeEmpty())
						Expect(rule.Name).To(Equal("group - alert"))
						Expect(rule.Expression).To(Equal("0"))
						Expect(rule.For).To(Equal(""))
						Expect(rule.KeepFiringFor).To(Equal(""))
						Expect(rule.Annotations).To(BeEmpty())
						Expect(rule.Labels).To(BeEmpty())
					},
				)

				It(
					"should convert a rule with all attributes", func() {
						rule, validationIssues, ok := convertAlertingRuleToCheckRule(
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
							nil,
							logger,
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
					},
				)

				It(
					"should convert a rule with an int expression", func() {
						rule, validationIssues, ok := convertAlertingRuleToCheckRule(
							prometheusv1.Rule{
								Alert: "alert",
								Expr:  intstr.FromInt32(123),
							},
							upsertAction,
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeTrue())
						Expect(validationIssues).To(BeEmpty())
						Expect(rule.Name).To(Equal("group - alert"))
						Expect(rule.Expression).To(Equal("123"))
					},
				)

				type thresholdValidationTestConfig struct {
					annotations              map[string]string
					expectedValidationIssues []string
				}

				DescribeTable(
					"threshold validation",
					func(config thresholdValidationTestConfig) {
						rule, validationIssues, ok := convertAlertingRuleToCheckRule(
							prometheusv1.Rule{
								Alert:       "alert",
								Expr:        intstr.FromString("foobar $__threshold baz"),
								Annotations: config.annotations,
							},
							upsertAction,
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
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
						},
					),
					Entry(
						"expression with threshold, no threshold annotation -> invalid",
						thresholdValidationTestConfig{
							annotations:              map[string]string{"unrelated": "annotation"},
							expectedValidationIssues: []string{thresholdAnnotationsMissingMessage()},
						},
					),
					Entry(
						"expression with threshold, degraded annotation -> valid",
						thresholdValidationTestConfig{
							annotations: map[string]string{
								thresholdDegradedAnnotation: "10",
							},
							expectedValidationIssues: nil,
						},
					),
					Entry(
						"expression with threshold, legacy degraded annotation -> valid",
						thresholdValidationTestConfig{
							annotations: map[string]string{
								thresholdDegradedAnnotationLegacy: "10",
							},
							expectedValidationIssues: nil,
						},
					),
					Entry(
						"expression with threshold, criticial annotation -> valid",
						thresholdValidationTestConfig{
							annotations: map[string]string{
								thresholdCriticalAnnotation: "10",
							},
							expectedValidationIssues: nil,
						},
					),
					Entry(
						"expression with threshold, legacy criticial annotation -> valid",
						thresholdValidationTestConfig{
							annotations: map[string]string{
								thresholdCriticalAnnotationLegacy: "10",
							},
							expectedValidationIssues: nil,
						},
					),
					Entry(
						"expression with threshold, both annotations -> valid",
						thresholdValidationTestConfig{
							annotations: map[string]string{
								"unrelated":                 "annotation",
								thresholdDegradedAnnotation: "10",
								thresholdCriticalAnnotation: "5",
							},
							expectedValidationIssues: nil,
						},
					),
					Entry(
						"expression with threshold, both legacy annotations -> valid",
						thresholdValidationTestConfig{
							annotations: map[string]string{
								"unrelated":                       "annotation",
								thresholdDegradedAnnotationLegacy: "10",
								thresholdCriticalAnnotationLegacy: "5",
							},
							expectedValidationIssues: nil,
						},
					),
					Entry(
						"expression with threshold, mixed current and legacy annotations -> valid",
						thresholdValidationTestConfig{
							annotations: map[string]string{
								"unrelated":                       "annotation",
								thresholdDegradedAnnotation:       "10",
								thresholdCriticalAnnotationLegacy: "5",
							},
							expectedValidationIssues: nil,
						},
					),
					Entry(
						"degraded annotation is not numerical -> invalid",
						thresholdValidationTestConfig{
							annotations: map[string]string{
								thresholdDegradedAnnotation: "1s",
							},
							expectedValidationIssues: []string{thresholdAnnotationsDegradedNonNumericalMessage("1s")},
						},
					),
					Entry(
						"critical annotation is not numerical -> invalid",
						thresholdValidationTestConfig{
							annotations: map[string]string{
								thresholdCriticalAnnotation: "abc",
							},
							expectedValidationIssues: []string{thresholdAnnotationsCriticalNonNumericalMessage("abc")},
						},
					),
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
						},
					),
				)
			},
		)

		Context(
			"converting a PrometheusRule resource to multiple http requests", func() {
				var prometheusRuleReconciler *PrometheusRuleReconciler

				BeforeEach(
					func() {
						prometheusRuleReconciler = &PrometheusRuleReconciler{}
					},
				)

				It(
					"should route alerting rules to check-rules API and recording rules to recording-rules API",
					func() {
						prometheusRule := map[string]any{}
						prometheusRuleYaml := `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: mixed-rules
spec:
  groups:
  - name: my-group
    interval: 5m
    rules:
    - alert: high-latency
      expr: "histogram_quantile(0.99, rate(http_duration_seconds_bucket[5m])) > 1"
      for: 10m
      labels:
        severity: critical
      annotations:
        summary: "High latency detected"
        dash0-threshold-degraded: "0.5"
        dash0-threshold-critical: "1"
    - record: job:http_requests:rate5m
      expr: "sum(rate(http_requests_total[5m])) by (job)"
      labels:
        env: production
    - alert: high-error-rate
      expr: "rate(http_requests_total{status=~\"5..\"}[5m]) > 0.1"
      labels:
        severity: warning
      annotations:
        summary: "High error rate"
        dash0-threshold-degraded: "0.05"
        dash0-threshold-critical: "0.1"
`
						Expect(yaml.Unmarshal([]byte(prometheusRuleYaml), &prometheusRule)).To(Succeed())
						apiConfig := ApiConfig{
							Endpoint: "https://api.example.com/",
							Dataset:  "my-dataset",
						}
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:             "mixed-rules",
							k8sNamespace:        TestNamespaceName,
							resource:            prometheusRule,
							validatedApiConfigs: []ValidatedApiConfigAndToken{{}},
						}
						resourceToRequestsResult :=
							prometheusRuleReconciler.MapResourceToHttpRequests(
								preconditionValidationResult,
								apiConfig,
								upsertAction,
								logger,
							)

						// All 3 rules should be processed (2 alerting + 1 recording).
						Expect(resourceToRequestsResult.TotalProcessed()).To(Equal(3))
						Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(3))
						Expect(resourceToRequestsResult.OriginsInResource).To(HaveLen(3))
						Expect(resourceToRequestsResult.ValidationIssues).To(BeEmpty())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeEmpty())

						// First rule: alerting rule "high-latency" → routed to check-rules API.
						alertReq1 := resourceToRequestsResult.ApiRequests[0]
						Expect(alertReq1.ItemName).To(Equal("my-group - high-latency"))
						Expect(alertReq1.Request.URL.Path).To(ContainSubstring("/api/alerting/check-rules/"))
						Expect(alertReq1.Request.Method).To(Equal(http.MethodPut))
						verifyRequestBodyContains(alertReq1.Request, "histogram_quantile")

						// Second rule: recording rule "job:http_requests:rate5m" → routed to recording-rules API.
						recordReq := resourceToRequestsResult.ApiRequests[1]
						Expect(recordReq.ItemName).To(Equal("my-group - job:http_requests:rate5m"))
						Expect(recordReq.Request.URL.Path).To(ContainSubstring("/api/recording-rules/"))
						Expect(recordReq.Request.Method).To(Equal(http.MethodPut))
						verifyRequestBodyContains(recordReq.Request, "sum(rate(http_requests_total[5m])) by (job)")

						// Third rule: alerting rule "high-error-rate" → routed to check-rules API.
						alertReq2 := resourceToRequestsResult.ApiRequests[2]
						Expect(alertReq2.ItemName).To(Equal("my-group - high-error-rate"))
						Expect(alertReq2.Request.URL.Path).To(ContainSubstring("/api/alerting/check-rules/"))
						Expect(alertReq2.Request.Method).To(Equal(http.MethodPut))
						verifyRequestBodyContains(alertReq2.Request, "rate(http_requests_total")
					},
				)

				It(
					"should create unique origins even if alert names are not unique", func() {
						prometheusRule := map[string]any{}
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
						apiConfig := ApiConfig{}
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:             "prometheus-rule",
							k8sNamespace:        TestNamespaceName,
							resource:            prometheusRule,
							validatedApiConfigs: []ValidatedApiConfigAndToken{{}},
						}
						resourceToRequestsResult :=
							prometheusRuleReconciler.MapResourceToHttpRequests(
								preconditionValidationResult,
								apiConfig,
								upsertAction,
								logger,
							)
						Expect(resourceToRequestsResult.TotalProcessed()).To(Equal(6))
						Expect(resourceToRequestsResult.OriginsInResource).To(HaveLen(6))

						// verify uniqueness of origins
						origins := make(map[string]bool, len(resourceToRequestsResult.OriginsInResource))
						for _, origin := range resourceToRequestsResult.OriginsInResource {
							if origins[origin] {
								Fail("Duplicate origin string: " + origin)
							}
							origins[origin] = true
						}
						Expect(resourceToRequestsResult.OriginsInResource).To(
							ContainElements(
								"dash0-operator___test-namespace_prometheus-rule_group-1_alert-1",
								"dash0-operator___test-namespace_prometheus-rule_group-1_alert-2",
								"dash0-operator___test-namespace_prometheus-rule_group-1_alert-1_1",
								"dash0-operator___test-namespace_prometheus-rule_group-1_alert-1_2",
								"dash0-operator___test-namespace_prometheus-rule_group-1_alert-2_1",
								"dash0-operator___test-namespace_prometheus-rule_group-2_alert-1",
							),
						)

						Expect(resourceToRequestsResult.ValidationIssues).To(BeEmpty())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeEmpty())
						Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(6))

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
							"group-1 - alert-2",
							"/dash0-operator___test-namespace_prometheus-rule_group-1_alert-2_1",
							"expression 5",
						)
						verifyCheckRuleRequest(
							resourceToRequestsResult.ApiRequests[5],
							"group-2 - alert-1",
							"/dash0-operator___test-namespace_prometheus-rule_group-2_alert-1",
							"expression 6",
						)
					},
				)
			},
		)

		Context(
			"mapping a PrometheusRule resource for the new /api/check-rules endpoint", func() {
				var prometheusRuleReconciler *PrometheusRuleReconciler

				BeforeEach(
					func() {
						prometheusRuleReconciler = &PrometheusRuleReconciler{}
					},
				)

				It(
					"renderCheckRulesListUrl targets /api/check-rules (not /api/alerting/check-rules)",
					func() {
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:      "test-rule",
							k8sNamespace: TestNamespaceName,
						}
						listUrl := prometheusRuleReconciler.renderCheckRulesListUrl(
							preconditionValidationResult,
							"https://api.example.com/",
							"my-dataset",
						)

						Expect(listUrl).To(HavePrefix("https://api.example.com/api/check-rules?"))
						Expect(listUrl).ToNot(ContainSubstring("/api/alerting/"))
						Expect(listUrl).To(ContainSubstring("dataset=my-dataset"))
						Expect(listUrl).To(ContainSubstring("originPrefix=dash0-operator__my-dataset_test-namespace_test-rule_"))
					},
				)

				It(
					"renderCheckRulesUrl targets /api/check-rules/<origin> (not /api/alerting/check-rules/<origin>)",
					func() {
						perOriginUrl := prometheusRuleReconciler.renderCheckRulesUrl(
							"my-origin",
							"https://api.example.com/",
							"my-dataset",
						)

						Expect(perOriginUrl).To(Equal("https://api.example.com/api/check-rules/my-origin?dataset=my-dataset"))
						Expect(perOriginUrl).ToNot(ContainSubstring("/api/alerting/"))
					},
				)

				It(
					"routes alerting rules in an alerts-only PrometheusRule to /api/check-rules",
					func() {
						prometheusRule := map[string]any{}
						prometheusRuleYaml := `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: alerts-only
spec:
  groups:
  - name: my-group
    interval: 5m
    rules:
    - alert: high-latency
      expr: "histogram_quantile(0.99, rate(http_duration_seconds_bucket[5m])) > 1"
      for: 10m
    - alert: high-error-rate
      expr: "rate(http_requests_total{status=~\"5..\"}[5m]) > 0.1"
`
						Expect(yaml.Unmarshal([]byte(prometheusRuleYaml), &prometheusRule)).To(Succeed())
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:             "alerts-only",
							k8sNamespace:        TestNamespaceName,
							resource:            prometheusRule,
							validatedApiConfigs: []ValidatedApiConfigAndToken{{}},
						}

						resourceToRequestsResult :=
							prometheusRuleReconciler.MapResourceToCheckRulesRequests(
								preconditionValidationResult,
								ApiConfig{Endpoint: "https://api.example.com/", Dataset: "my-dataset"},
								upsertAction,
								logger,
							)

						Expect(resourceToRequestsResult.ValidationIssues).To(BeEmpty())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeEmpty())
						Expect(resourceToRequestsResult.ApiRequests).To(HaveLen(2))
						Expect(resourceToRequestsResult.AlertingRulesTotal).To(Equal(2))
						Expect(resourceToRequestsResult.RecordingRulesTotal).To(Equal(0))
						for _, req := range resourceToRequestsResult.ApiRequests {
							Expect(req.Request.Method).To(Equal(http.MethodPut))
							Expect(req.Request.URL.Path).To(HavePrefix("/api/check-rules/"))
							Expect(req.Request.URL.Path).ToNot(ContainSubstring("/api/alerting/"))
						}
					},
				)

				It(
					"rejects the whole resource when any rule has a `record:` entry",
					func() {
						prometheusRule := map[string]any{}
						prometheusRuleYaml := `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: mixed-rules
spec:
  groups:
  - name: my-group
    interval: 5m
    rules:
    - alert: high-latency
      expr: "vector(1)"
    - record: job:http_requests:rate5m
      expr: "sum(rate(http_requests_total[5m])) by (job)"
    - alert: high-error-rate
      expr: "vector(2)"
`
						Expect(yaml.Unmarshal([]byte(prometheusRuleYaml), &prometheusRule)).To(Succeed())
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:             "mixed-rules",
							k8sNamespace:        TestNamespaceName,
							resource:            prometheusRule,
							validatedApiConfigs: []ValidatedApiConfigAndToken{{}},
						}

						resourceToRequestsResult :=
							prometheusRuleReconciler.MapResourceToCheckRulesRequests(
								preconditionValidationResult,
								ApiConfig{Endpoint: "https://api.example.com/", Dataset: "my-dataset"},
								upsertAction,
								logger,
							)

						Expect(resourceToRequestsResult.ApiRequests).To(BeEmpty())
						Expect(resourceToRequestsResult.OriginsInResource).To(BeEmpty())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeEmpty())
						Expect(resourceToRequestsResult.ValidationIssues).To(HaveLen(1))
						Expect(resourceToRequestsResult.ValidationIssues["mixed-rules"]).To(ConsistOf(
							rejectionMessageRecordingRulesNotAcceptedByCheckRules,
						))
					},
				)

				It(
					"rejects a records-only PrometheusRule",
					func() {
						prometheusRule := map[string]any{}
						prometheusRuleYaml := `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: records-only
spec:
  groups:
  - name: my-group
    rules:
    - record: job:http_requests:rate5m
      expr: "sum(rate(http_requests_total[5m])) by (job)"
`
						Expect(yaml.Unmarshal([]byte(prometheusRuleYaml), &prometheusRule)).To(Succeed())
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:             "records-only",
							k8sNamespace:        TestNamespaceName,
							resource:            prometheusRule,
							validatedApiConfigs: []ValidatedApiConfigAndToken{{}},
						}

						resourceToRequestsResult :=
							prometheusRuleReconciler.MapResourceToCheckRulesRequests(
								preconditionValidationResult,
								ApiConfig{Endpoint: "https://api.example.com/", Dataset: "my-dataset"},
								upsertAction,
								logger,
							)

						Expect(resourceToRequestsResult.ApiRequests).To(BeEmpty())
						Expect(resourceToRequestsResult.OriginsInResource).To(BeEmpty())
						Expect(resourceToRequestsResult.SynchronizationErrors).To(BeEmpty())
						Expect(resourceToRequestsResult.ValidationIssues).To(HaveLen(1))
						Expect(resourceToRequestsResult.ValidationIssues["records-only"]).To(ConsistOf(
							rejectionMessageRecordingRulesNotAcceptedByCheckRules,
						))
					},
				)

				It(
					"rejects the whole resource on deleteAction too when any rule has a `record:` entry",
					func() {
						prometheusRule := map[string]any{}
						prometheusRuleYaml := `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: mixed-rules
spec:
  groups:
  - name: my-group
    rules:
    - alert: high-latency
      expr: "vector(1)"
    - record: job:http_requests:rate5m
      expr: "sum(rate(http_requests_total[5m])) by (job)"
`
						Expect(yaml.Unmarshal([]byte(prometheusRuleYaml), &prometheusRule)).To(Succeed())
						preconditionValidationResult := &preconditionValidationResult{
							k8sName:             "mixed-rules",
							k8sNamespace:        TestNamespaceName,
							resource:            prometheusRule,
							validatedApiConfigs: []ValidatedApiConfigAndToken{{}},
						}

						resourceToRequestsResult :=
							prometheusRuleReconciler.MapResourceToCheckRulesRequests(
								preconditionValidationResult,
								ApiConfig{Endpoint: "https://api.example.com/", Dataset: "my-dataset"},
								deleteAction,
								logger,
							)

						Expect(resourceToRequestsResult.ApiRequests).To(BeEmpty())
						Expect(resourceToRequestsResult.OriginsInResource).To(BeEmpty())
						Expect(resourceToRequestsResult.ValidationIssues).To(HaveLen(1))
						Expect(resourceToRequestsResult.ValidationIssues["mixed-rules"]).To(ConsistOf(
							rejectionMessageRecordingRulesNotAcceptedByCheckRules,
						))
					},
				)
			},
		)

		Context(
			"converting a single Prometheus rule to an http request", func() {

				It(
					"should process a rule that has both record and alert as an alerting rule", func() {
						// In the production flow, recording rules (rule.Record != "") are handled
						// separately in MapResourceToHttpRequests before convertAlertingRuleToRequest
						// is called. If a rule somehow has both Record and Alert set,
						// convertAlertingRuleToRequest treats it as an alerting rule.
						req, validationIssues, syncError, ok := convertAlertingRuleToRequest(
							"https://api.dash0.com/alerting/check-rules/rule-origin",
							upsertAction,
							ApiConfig{},
							prometheusv1.Rule{
								Record: "record",
								Alert:  "alert",
							},
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeTrue())
						Expect(validationIssues).To(BeEmpty())
						Expect(syncError).To(BeNil())
						Expect(req).NotTo(BeNil())
					},
				)

				It(
					"should treat an empty rule as invalid", func() {
						req, validationIssues, syncError, ok := convertAlertingRuleToRequest(
							"https://api.dash0.com/alerting/check-rules/rule-origin",
							upsertAction,
							ApiConfig{},
							prometheusv1.Rule{},
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeFalse())
						Expect(validationIssues).To(HaveLen(1))
						Expect(validationIssues).To(ContainElement("rule has neither the alert nor the record attribute"))
						Expect(syncError).To(BeNil())
						Expect(req).To(BeNil())
					},
				)

				It(
					"should treat a rule without alert or record as invalid", func() {
						req, validationIssues, syncError, ok := convertAlertingRuleToRequest(
							"https://api.dash0.com/alerting/check-rules/rule-origin",
							upsertAction,
							ApiConfig{},
							prometheusv1.Rule{
								Expr:          intstr.FromString("expr"),
								For:           ptr.To(prometheusv1.Duration("10s")),
								KeepFiringFor: ptr.To(prometheusv1.NonEmptyDuration("10s")),
							},
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeFalse())
						Expect(validationIssues).To(HaveLen(1))
						Expect(validationIssues).To(ContainElement("rule has neither the alert nor the record attribute"))
						Expect(syncError).To(BeNil())
						Expect(req).To(BeNil())
					},
				)

				It(
					"should treat a rule with empty expression as invalid", func() {
						req, validationIssues, syncError, ok := convertAlertingRuleToRequest(
							"https://api.dash0.com/alerting/check-rules/rule-origin",
							upsertAction,
							ApiConfig{},
							prometheusv1.Rule{
								Alert: "alert",
								Expr:  intstr.FromString(""),
							},
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeFalse())
						Expect(validationIssues).To(HaveLen(1))
						Expect(validationIssues).To(ContainElement("the rule has no expression attribute (or the expression attribute is empty)"))
						Expect(syncError).To(BeNil())
						Expect(req).To(BeNil())
					},
				)

				It(
					"should convert an almost empty rule", func() {
						req, validationIssues, syncError, ok := convertAlertingRuleToRequest(
							"https://api.dash0.com/alerting/check-rules/rule-origin",
							upsertAction,
							ApiConfig{},
							prometheusv1.Rule{
								Alert: "alert",
							},
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeTrue())
						Expect(validationIssues).To(BeEmpty())
						Expect(syncError).To(BeNil())
						Expect(req).ToNot(BeNil())
						Expect(req.URL.String()).To(Equal("https://api.dash0.com/alerting/check-rules/rule-origin"))
					},
				)

				It(
					"should convert a rule with all attributes", func() {
						req, validationIssues, syncError, ok := convertAlertingRuleToRequest(
							"https://api.dash0.com/alerting/check-rules/rule-origin",
							upsertAction,
							ApiConfig{},
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
							"group",
							ptr.To(prometheusv1.Duration("10m")),
							nil,
							logger,
						)

						Expect(ok).To(BeTrue())
						Expect(validationIssues).To(BeEmpty())
						Expect(syncError).To(BeNil())
						Expect(req).ToNot(BeNil())
						Expect(req.URL.String()).To(Equal("https://api.dash0.com/alerting/check-rules/rule-origin"))
					},
				)

				Context(
					"merging metadata annotations with rule annotations", func() {
						It(
							"should use only rule annotations when metadata annotations are nil", func() {
								rule, validationIssues, ok := convertAlertingRuleToCheckRule(
									prometheusv1.Rule{
										Alert: "alert",
										Expr:  intstr.FromString("vector(1)"),
										Annotations: map[string]string{
											"rule-key": "rule-value",
										},
									},
									upsertAction,
									"group",
									ptr.To(prometheusv1.Duration("10m")),
									nil,
									logger,
								)

								Expect(ok).To(BeTrue())
								Expect(validationIssues).To(BeEmpty())
								Expect(rule.Annotations).To(HaveLen(1))
								Expect(rule.Annotations["rule-key"]).To(Equal("rule-value"))
							},
						)

						It(
							"should use only metadata annotations when rule annotations are nil", func() {
								rule, validationIssues, ok := convertAlertingRuleToCheckRule(
									prometheusv1.Rule{
										Alert:       "alert",
										Expr:        intstr.FromString("vector(1)"),
										Annotations: nil,
									},
									upsertAction,
									"group",
									ptr.To(prometheusv1.Duration("10m")),
									map[string]string{
										"metadata-key": "metadata-value",
									},
									logger,
								)

								Expect(ok).To(BeTrue())
								Expect(validationIssues).To(BeEmpty())
								Expect(rule.Annotations).To(HaveLen(1))
								Expect(rule.Annotations["metadata-key"]).To(Equal("metadata-value"))
							},
						)

						It(
							"should use only metadata annotations when rule annotations are empty", func() {
								rule, validationIssues, ok := convertAlertingRuleToCheckRule(
									prometheusv1.Rule{
										Alert:       "alert",
										Expr:        intstr.FromString("vector(1)"),
										Annotations: map[string]string{},
									},
									upsertAction,
									"group",
									ptr.To(prometheusv1.Duration("10m")),
									map[string]string{
										"metadata-key": "metadata-value",
									},
									logger,
								)

								Expect(ok).To(BeTrue())
								Expect(validationIssues).To(BeEmpty())
								Expect(rule.Annotations).To(HaveLen(1))
								Expect(rule.Annotations["metadata-key"]).To(Equal("metadata-value"))
							},
						)

						It(
							"should merge metadata and rule annotations when both have different keys", func() {
								rule, validationIssues, ok := convertAlertingRuleToCheckRule(
									prometheusv1.Rule{
										Alert: "alert",
										Expr:  intstr.FromString("vector(1)"),
										Annotations: map[string]string{
											"rule-key": "rule-value",
										},
									},
									upsertAction,
									"group",
									ptr.To(prometheusv1.Duration("10m")),
									map[string]string{
										"metadata-key": "metadata-value",
									},
									logger,
								)

								Expect(ok).To(BeTrue())
								Expect(validationIssues).To(BeEmpty())
								Expect(rule.Annotations).To(HaveLen(2))
								Expect(rule.Annotations["metadata-key"]).To(Equal("metadata-value"))
								Expect(rule.Annotations["rule-key"]).To(Equal("rule-value"))
							},
						)

						It(
							"should give priority to rule annotations when there are conflicts", func() {
								rule, validationIssues, ok := convertAlertingRuleToCheckRule(
									prometheusv1.Rule{
										Alert: "alert",
										Expr:  intstr.FromString("vector(1)"),
										Annotations: map[string]string{
											"common-key": "rule-value",
											"rule-key":   "rule-value",
										},
									},
									upsertAction,
									"group",
									ptr.To(prometheusv1.Duration("10m")),
									map[string]string{
										"common-key":   "metadata-value",
										"metadata-key": "metadata-value",
									},
									logger,
								)

								Expect(ok).To(BeTrue())
								Expect(validationIssues).To(BeEmpty())
								Expect(rule.Annotations).To(HaveLen(3))
								Expect(rule.Annotations["common-key"]).To(Equal("rule-value"))
								Expect(rule.Annotations["metadata-key"]).To(Equal("metadata-value"))
								Expect(rule.Annotations["rule-key"]).To(Equal("rule-value"))
							},
						)

						It(
							"should handle empty map when both annotations are nil", func() {
								rule, validationIssues, ok := convertAlertingRuleToCheckRule(
									prometheusv1.Rule{
										Alert:       "alert",
										Expr:        intstr.FromString("vector(1)"),
										Annotations: nil,
									},
									upsertAction,
									"group",
									ptr.To(prometheusv1.Duration("10m")),
									nil,
									logger,
								)

								Expect(ok).To(BeTrue())
								Expect(validationIssues).To(BeEmpty())
								Expect(rule.Annotations).NotTo(BeNil())
								Expect(rule.Annotations).To(BeEmpty())
							},
						)

						It(
							"should allow rule to override metadata annotation with empty string", func() {
								rule, validationIssues, ok := convertAlertingRuleToCheckRule(
									prometheusv1.Rule{
										Alert: "alert",
										Expr:  intstr.FromString("vector(1)"),
										Annotations: map[string]string{
											"common-key": "",
										},
									},
									upsertAction,
									"group",
									ptr.To(prometheusv1.Duration("10m")),
									map[string]string{
										"common-key": "metadata-value",
									},
									logger,
								)

								Expect(ok).To(BeTrue())
								Expect(validationIssues).To(BeEmpty())
								Expect(rule.Annotations).To(HaveLen(1))
								Expect(rule.Annotations["common-key"]).To(Equal(""))
							},
						)
					},
				)
			},
		)
	},
)

func createPrometheusRuleCrdReconciler() *PrometheusRuleCrdReconciler {
	crdReconciler := NewPrometheusRuleCrdReconciler(
		k8sClient,
		testQueuePrometheusRules,
		&DummyLeaderElectionAware{Leader: true},
		TestHTTPClient(),
	)

	// We create the controller multiple times in tests, this option is required, otherwise the controller
	// runtime will complain.
	crdReconciler.skipNameValidation = true
	return crdReconciler
}

func expectFetchOriginsGetRequestCustom(clusterId string, endpoint string, authHeader string, dataset string) {
	gock.New(endpoint).
		Get("/api/alerting/check-rules").
		MatchHeader("Authorization", authHeader).
		MatchParam("dataset", dataset).
		ParamPresent("originPrefix").
		Times(1).
		Reply(200).
		JSON(
			[]map[string]string{
				{
					"origin": fmt.Sprintf(
						"dash0-operator_%s_%s_test-namespace_test-rule_dash0|group-1_rule-1-1",
						clusterId,
						dataset,
					),
				},
				{
					"origin": fmt.Sprintf(
						"dash0-operator_%s_%s_test-namespace_test-rule_dash0|group-1_rule-1-2",
						clusterId,
						dataset,
					),
				},
				{
					"origin": fmt.Sprintf(
						"dash0-operator_%s_%s_test-namespace_test-rule_dash0|group-2_rule-2-1",
						clusterId,
						dataset,
					),
				},
				{
					"origin": fmt.Sprintf(
						"dash0-operator_%s_%s_test-namespace_test-rule_dash0|group-2_rule-2-2",
						clusterId,
						dataset,
					),
				},
			},
		)
	gock.New(endpoint).
		Get("/api/recording-rules").
		MatchHeader("Authorization", authHeader).
		MatchParam("dataset", dataset).
		ParamPresent("originPrefix").
		Times(1).
		Reply(200).
		JSON([]map[string]string{})
}

func expectFetchOriginsGetRequest(clusterId string) {
	expectFetchOriginsGetRequestCustom(clusterId, ApiEndpointTest, AuthorizationHeaderTest, DatasetCustomTest)
}

func expectCheckRulePutRequestsCustom(
	clusterId string,
	endpoint string,
	authHeader string,
	dataset string,
	expectedRequests []checkRuleRequestExpectation,
) {
	for _, expectedRequest := range expectedRequests {
		origin :=
			fmt.Sprintf(
				"dash0-operator_%s_%s_test-namespace_test-rule_%s_%s",
				clusterId,
				dataset,
				strings.ReplaceAll(expectedRequest.group, "/", "|"),
				expectedRequest.alert,
			)
		expectedPath := fmt.Sprintf("%s.*%s", checkRuleApiBasePath, origin)
		gock.New(endpoint).
			Put(expectedPath).
			MatchHeader("Authorization", authHeader).
			MatchParam("dataset", dataset).
			Times(1).
			Reply(200).
			JSON(
				map[string]any{
					"id":      origin,
					"dataset": dataset,
				},
			)
	}
}

func expectCheckRulePutRequests(clusterId string, expectedRequests []checkRuleRequestExpectation) {
	expectCheckRulePutRequestsCustom(clusterId, ApiEndpointTest, AuthorizationHeaderTest, DatasetCustomTest, expectedRequests)
}

func expectRecordingRulePutRequestsCustom(
	clusterId string,
	endpoint string,
	authHeader string,
	dataset string,
	expectedRequests []checkRuleRequestExpectation,
) {
	for _, expectedRequest := range expectedRequests {
		origin :=
			fmt.Sprintf(
				"dash0-operator_%s_%s_test-namespace_test-rule_%s_%s",
				clusterId,
				dataset,
				strings.ReplaceAll(expectedRequest.group, "/", "|"),
				expectedRequest.alert,
			)
		expectedPath := fmt.Sprintf("%s.*%s", recordingRuleApiBasePath, origin)
		gock.New(endpoint).
			Put(expectedPath).
			MatchHeader("Authorization", authHeader).
			MatchParam("dataset", dataset).
			Times(1).
			Reply(200).
			JSON(
				map[string]any{
					"id":      origin,
					"dataset": dataset,
				},
			)
	}
}

func expectRecordingRulePutRequests(clusterId string, expectedRequests []checkRuleRequestExpectation) {
	expectRecordingRulePutRequestsCustom(clusterId, ApiEndpointTest, AuthorizationHeaderTest, DatasetCustomTest, expectedRequests)
}

func defaultRecordingRuleRequests() []checkRuleRequestExpectation {
	return []checkRuleRequestExpectation{
		{
			group: "dash0/group-1",
			alert: "rule-1-3",
		},
	}
}

func expectCheckRuleDeleteRequests(clusterId string, expectedRequests []checkRuleRequestExpectation) {
	expectCheckRuleDeleteRequestsWithHttpStatus(clusterId, expectedRequests, http.StatusOK)
}

func expectCheckRuleDeleteRequestsWithHttpStatus(
	clusterId string,
	expectedRequests []checkRuleRequestExpectation,
	status int,
) {
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

func expectRecordingRuleDeleteRequests(clusterId string, expectedRequests []checkRuleRequestExpectation) {
	expectRecordingRuleDeleteRequestsWithHttpStatus(clusterId, expectedRequests, http.StatusOK)
}

func expectRecordingRuleDeleteRequestsWithHttpStatus(
	clusterId string,
	expectedRequests []checkRuleRequestExpectation,
	status int,
) {
	for _, expectedRequest := range expectedRequests {
		origin :=
			fmt.Sprintf(
				"dash0-operator_%s_test-dataset_test-namespace_test-rule_%s_%s",
				clusterId,
				strings.ReplaceAll(expectedRequest.group, "/", "|"),
				expectedRequest.alert,
			)
		expectedPath := fmt.Sprintf("%s.*%s", recordingRuleApiBasePath, origin)
		gock.New(ApiEndpointTest).
			Delete(expectedPath).
			MatchHeader("Authorization", AuthorizationHeaderTest).
			MatchParam("dataset", DatasetCustomTest).
			Times(1).
			Reply(status)
	}
}

func createDefaultPrometheusRuleResource() unstructured.Unstructured {
	return createDefaultPrometheusRuleResourceWithEnableLabel("")
}

func createDefaultPrometheusRuleResourceWithEnableLabel(dash0EnableLabelValue string) unstructured.Unstructured {
	objectMeta := defaultRuleObjectMeta
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return createPrometheusRuleResourceWithObjectMeta(createDefaultSpec(), objectMeta)
}

func createPrometheusRuleResource(spec prometheusv1.PrometheusRuleSpec) unstructured.Unstructured {
	return createPrometheusRuleResourceWithObjectMeta(spec, defaultRuleObjectMeta)
}

func createPrometheusRuleResourceWithObjectMeta(
	spec prometheusv1.PrometheusRuleSpec,
	objectMeta metav1.ObjectMeta,
) unstructured.Unstructured {
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
		err := k8sClient.Delete(
			ctx, prometheusRuleCrd, &client.DeleteOptions{
				GracePeriodSeconds: new(int64),
			},
		)
		if err != nil && apierrors.IsNotFound(err) {
			return
		} else if err != nil {
			Expect(err).NotTo(HaveOccurred())
		}

		Eventually(
			func(g Gomega) {
				err := k8sClient.Get(ctx, PrometheusRuleCrdQualifiedName, &apiextensionsv1.CustomResourceDefinition{})
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			},
		).Should(Succeed())

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

func expectedPrometheusSyncResult(
	clusterId string,
	apiEndpoint string,
	dataset string,
	originPattern string,
) dash0common.PrometheusRuleSynchronizationResult {
	return dash0common.PrometheusRuleSynchronizationResult{
		SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusSuccessful,
		AlertingRulesTotal:    4,
		RecordingRulesTotal:   1,
		InvalidRulesTotal:     0,
		InvalidRules:          nil,
		SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
			{
				Dash0ApiEndpoint:       apiEndpoint,
				Dash0Dataset:           dataset,
				SynchronizedRulesTotal: 5,
				SynchronizedRulesAttributes: map[string]dash0common.PrometheusRuleSynchronizedRuleAttributes{
					"dash0/group-1 - rule-1-1": {
						Dash0Origin: fmt.Sprintf(originPattern, clusterId, "dash0|group-1", "rule-1-1"),
					},
					"dash0/group-1 - rule-1-2": {
						Dash0Origin: fmt.Sprintf(originPattern, clusterId, "dash0|group-1", "rule-1-2"),
					},
					"dash0/group-1 - rule-1-3": {
						Dash0Origin: fmt.Sprintf(originPattern, clusterId, "dash0|group-1", "rule-1-3"),
					},
					"dash0/group-2 - rule-2-1": {
						Dash0Origin: fmt.Sprintf(originPattern, clusterId, "dash0|group-2", "rule-2-1"),
					},
					"dash0/group-2 - rule-2-2": {
						Dash0Origin: fmt.Sprintf(originPattern, clusterId, "dash0|group-2", "rule-2-2"),
					},
				},
				SynchronizationErrorsTotal: 0,
				SynchronizationErrors:      nil,
			},
		},
	}
}

func defaultExpectedPrometheusSyncResult(clusterId string) dash0common.PrometheusRuleSynchronizationResult {
	return expectedPrometheusSyncResult(clusterId, ApiEndpointStandardizedTest, DatasetCustomTest, checkRuleOriginPattern)
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
	Eventually(
		func(g Gomega) {
			monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
			results := monRes.Status.PrometheusRuleSynchronizationResults
			g.Expect(results).NotTo(BeNil())
			g.Expect(results).To(HaveLen(1))
			result := results[fmt.Sprintf("%s/%s", TestNamespaceName, "test-rule")]
			g.Expect(result).NotTo(BeNil())

			actualSyncResult := &result.SynchronizationResults[0]
			expectedSyncResult := &expectedResult.SynchronizationResults[0]

			if len(expectedSyncResult.SynchronizationErrors) > 0 {
				// http errors contain a different random path for each run
				g.Expect(actualSyncResult.SynchronizationErrors).To(HaveLen(len(expectedSyncResult.SynchronizationErrors)))
				for _, expectedSyncErrRegex := range expectedSyncResult.SynchronizationErrors {
					g.Expect(actualSyncResult.SynchronizationErrors).To(ContainElement(MatchRegexp(expectedSyncErrRegex)))
				}
			}

			// we do not verify the exact timestamp
			expectedResult.SynchronizedAt = result.SynchronizedAt
			// errors have been verified using regex
			expectedSyncResult.SynchronizationErrors = nil
			actualSyncResult.SynchronizationErrors = nil

			g.Expect(result).To(Equal(expectedResult))
		},
	).Should(Succeed())
}

func verifyNoPrometheusRuleSynchronizationResultHasBeenWrittenToMonitoringResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
) {
	Consistently(
		func(g Gomega) {
			monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
			results := monRes.Status.PrometheusRuleSynchronizationResults
			g.Expect(results).To(BeNil())
		}, 200*time.Millisecond, 50*time.Millisecond,
	).Should(Succeed())
}

func expectCheckRuleDeleteRequestsCustom(
	clusterId string,
	endpoint string,
	authHeader string,
	dataset string,
	expectedRequests []checkRuleRequestExpectation,
	status int,
) {
	for _, expectedRequest := range expectedRequests {
		origin :=
			fmt.Sprintf(
				"dash0-operator_%s_%s_test-namespace_test-rule_%s_%s",
				clusterId,
				dataset,
				strings.ReplaceAll(expectedRequest.group, "/", "|"),
				expectedRequest.alert,
			)
		expectedPath := fmt.Sprintf("%s.*%s", checkRuleApiBasePath, origin)
		gock.New(endpoint).
			Delete(expectedPath).
			MatchHeader("Authorization", authHeader).
			MatchParam("dataset", dataset).
			Times(1).
			Reply(status)
	}
}

func verifyPrometheusRuleMultiExportSynchronizationResult(
	ctx context.Context,
	k8sClient client.Client,
	expectedResult dash0common.PrometheusRuleSynchronizationResult,
) {
	Eventually(
		func(g Gomega) {
			monRes := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
			results := monRes.Status.PrometheusRuleSynchronizationResults
			g.Expect(results).NotTo(BeNil())
			g.Expect(results).To(HaveLen(1))
			result := results[fmt.Sprintf("%s/%s", TestNamespaceName, "test-rule")]
			g.Expect(result).NotTo(BeNil())

			g.Expect(result.SynchronizationStatus).To(Equal(expectedResult.SynchronizationStatus))
			g.Expect(result.AlertingRulesTotal).To(Equal(expectedResult.AlertingRulesTotal))
			g.Expect(result.RecordingRulesTotal).To(Equal(expectedResult.RecordingRulesTotal))
			g.Expect(result.InvalidRulesTotal).To(Equal(expectedResult.InvalidRulesTotal))
			g.Expect(result.SynchronizationResults).To(HaveLen(len(expectedResult.SynchronizationResults)))

			for i, expectedSyncResult := range expectedResult.SynchronizationResults {
				actualSyncResult := result.SynchronizationResults[i]
				g.Expect(actualSyncResult.Dash0ApiEndpoint).To(Equal(expectedSyncResult.Dash0ApiEndpoint))
				g.Expect(actualSyncResult.Dash0Dataset).To(Equal(expectedSyncResult.Dash0Dataset))
				g.Expect(actualSyncResult.SynchronizedRulesTotal).To(Equal(expectedSyncResult.SynchronizedRulesTotal))
				g.Expect(actualSyncResult.SynchronizationErrorsTotal).To(Equal(expectedSyncResult.SynchronizationErrorsTotal))
				g.Expect(actualSyncResult.SynchronizedRulesAttributes).To(Equal(expectedSyncResult.SynchronizedRulesAttributes))
			}
		},
	).Should(Succeed())
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
