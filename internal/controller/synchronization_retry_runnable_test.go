// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"time"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The synchronization retry runnable", func() {

	Context("when classifying synchronization errors", func() {
		DescribeTable(
			"isRetryableHttpStatusCode",
			func(statusCode int, expectedRetryable bool) {
				Expect(isRetryableHttpStatusCode(statusCode)).To(Equal(expectedRetryable))
			},
			Entry("transport-level error (no status code) is retryable", 0, true),
			Entry("rate limiting (429) is retryable", 429, true),
			Entry("internal server error (500) is retryable", 500, true),
			Entry("bad gateway (502) is retryable", 502, true),
			Entry("service unavailable (503) is retryable", 503, true),
			Entry("gateway timeout (504) is retryable", 504, true),
			Entry("bad request (400) is not retryable", 400, false),
			Entry("unauthorized (401) is not retryable", 401, false),
			Entry("forbidden (403) is not retryable", 403, false),
			Entry("not found (404) is not retryable", 404, false),
			Entry("conflict (409) is not retryable", 409, false),
		)

		It("detects retryable errors in a Prometheus rule synchronization result", func() {
			Expect(prometheusRuleSynchronizationResultHasRetryableError(
				dash0common.PrometheusRuleSynchronizationResult{
					SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
						{
							SynchronizationErrors:               map[string]string{"rule-a": "boom"},
							SynchronizationErrorHttpStatusCodes: map[string]int{"rule-a": 503},
						},
					},
				},
			)).To(BeTrue())
		})

		It("does not flag validation/client errors in a Prometheus rule synchronization result as retryable", func() {
			Expect(prometheusRuleSynchronizationResultHasRetryableError(
				dash0common.PrometheusRuleSynchronizationResult{
					SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
						{
							SynchronizationErrors:               map[string]string{"rule-a": "rejected"},
							SynchronizationErrorHttpStatusCodes: map[string]int{"rule-a": 400},
						},
					},
				},
			)).To(BeFalse())
		})

		It("flags a Prometheus rule synchronization result without any error as not retryable", func() {
			Expect(prometheusRuleSynchronizationResultHasRetryableError(
				dash0common.PrometheusRuleSynchronizationResult{
					SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
						{SynchronizedRulesTotal: 2},
					},
				},
			)).To(BeFalse())
		})

		It("detects retryable errors in a Perses dashboard synchronization result", func() {
			Expect(persesDashboardSynchronizationResultHasRetryableError(
				dash0common.PersesDashboardSynchronizationResults{
					SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
						{SynchronizationError: "boom", HttpStatusCode: 500},
					},
				},
			)).To(BeTrue())
		})

		It("does not flag validation/client errors in a Perses dashboard synchronization result as retryable", func() {
			Expect(persesDashboardSynchronizationResultHasRetryableError(
				dash0common.PersesDashboardSynchronizationResults{
					SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
						{SynchronizationError: "rejected", HttpStatusCode: 400},
					},
				},
			)).To(BeFalse())
		})

		It("treats a transport-level Perses dashboard error (no status code) as retryable", func() {
			Expect(persesDashboardSynchronizationResultHasRetryableError(
				dash0common.PersesDashboardSynchronizationResults{
					SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
						{SynchronizationError: "connection refused", HttpStatusCode: 0},
					},
				},
			)).To(BeTrue())
		})

		DescribeTable(
			"isRetryableSynchronizationError",
			func(synchronizationError string, httpStatusCode int, expectedRetryable bool) {
				Expect(isRetryableSynchronizationError(synchronizationError, httpStatusCode)).To(Equal(expectedRetryable))
			},
			Entry("no error is not retryable", "", 0, false),
			Entry("no error with a (stale) status code is not retryable", "", 503, false),
			Entry("server error is retryable", "boom", 503, true),
			Entry("rate limiting is retryable", "slow down", 429, true),
			Entry("transport-level error (no status code) is retryable", "connection refused", 0, true),
			Entry("client error is not retryable", "bad request", 400, false),
		)

		DescribeTable(
			"splitQualifiedName",
			func(input, expectedNamespace, expectedName string, expectedOk bool) {
				namespace, name, ok := splitQualifiedName(input)
				Expect(ok).To(Equal(expectedOk))
				Expect(namespace).To(Equal(expectedNamespace))
				Expect(name).To(Equal(expectedName))
			},
			Entry("valid qualified name", "my-namespace/my-name", "my-namespace", "my-name", true),
			Entry("missing separator", "my-name", "", "", false),
			Entry("empty namespace", "/my-name", "", "", false),
			Entry("empty name", "my-namespace/", "", "", false),
		)
	})

	Context("when retrying failed synchronizations", Ordered, func() {
		ctx := context.Background()
		var queue *workqueue.Typed[ThirdPartyResourceSyncJob]
		var persesDashboardCrdReconciler *PersesDashboardCrdReconciler
		var prometheusRuleCrdReconciler *PrometheusRuleCrdReconciler
		var runnable *SynchronizationRetryRunnable

		BeforeAll(func() {
			EnsureTestNamespaceExists(ctx, k8sClient)
			EnsureOperatorNamespaceExists(ctx, k8sClient)
			ensurePrometheusRuleCrdExists(ctx)
			ensurePersesDashboardCrdExists(ctx)
		})

		AfterAll(func() {
			deletePrometheusRuleCrdIfItExists(ctx)
			deletePersesDashboardCrdIfItExists(ctx)
		})

		BeforeEach(func() {
			queue = workqueue.NewTypedWithConfig(
				workqueue.TypedQueueConfig[ThirdPartyResourceSyncJob]{
					Name: "dash0-synchronization-retry-test-queue",
				},
			)

			prometheusRuleCrdReconciler = NewPrometheusRuleCrdReconciler(
				k8sClient,
				queue,
				&DummyLeaderElectionAware{Leader: true},
				TestHTTPClient(),
			)
			prometheusRuleCrdReconciler.skipNameValidation = true
			prometheusRuleCrdReconciler.CreateThirdPartyResourceReconciler(types.UID("test-cluster-uid"))
			startWatching(ctx, prometheusRuleCrdReconciler.ThirdPartyResourceReconciler())

			persesDashboardCrdReconciler = NewPersesDashboardCrdReconciler(
				k8sClient,
				queue,
				&DummyLeaderElectionAware{Leader: true},
				TestHTTPClient(),
				PersesDashboardConversionWebhookSettings{},
			)
			persesDashboardCrdReconciler.skipNameValidation = true
			persesDashboardCrdReconciler.CreateThirdPartyResourceReconciler(types.UID("test-cluster-uid"))
			persesDashboardCrdReconciler.recordCrdVersion(persesDashboardV1Alpha1)
			startWatching(ctx, persesDashboardCrdReconciler.ThirdPartyResourceReconciler())

			runnable = NewSynchronizationRetryRunnable(
				k8sClient,
				persesDashboardCrdReconciler,
				prometheusRuleCrdReconciler,
				nil,
				10*time.Minute,
				logd.FromContext(ctx),
			)
		})

		AfterEach(func() {
			queue.ShutDown()
			DeleteMonitoringResourceIfItExists(ctx, k8sClient)
			deletePrometheusRuleIfItExists(ctx, "retryable-rule")
			deletePrometheusRuleIfItExists(ctx, "validation-rule")
			deletePersesDashboardIfItExists(ctx, "retryable-dashboard")
			deletePersesDashboardIfItExists(ctx, "validation-dashboard")
		})

		It("re-enqueues only the resources that have a retryable synchronization error", func() {
			createPrometheusRuleInCluster(ctx, "retryable-rule")
			createPrometheusRuleInCluster(ctx, "validation-rule")
			createPersesDashboardInCluster(ctx, "retryable-dashboard")
			createPersesDashboardInCluster(ctx, "validation-dashboard")

			monitoringResource := EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)
			monitoringResource.Status.PrometheusRuleSynchronizationResults =
				map[string]dash0common.PrometheusRuleSynchronizationResult{
					TestNamespaceName + "/retryable-rule": {
						SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
						SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
							{
								SynchronizationErrors:               map[string]string{"group - alert": "server error"},
								SynchronizationErrorHttpStatusCodes: map[string]int{"group - alert": 503},
							},
						},
					},
					TestNamespaceName + "/validation-rule": {
						SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
						SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
							{
								SynchronizationErrors:               map[string]string{"group - alert": "rejected"},
								SynchronizationErrorHttpStatusCodes: map[string]int{"group - alert": 400},
							},
						},
					},
				}
			monitoringResource.Status.PersesDashboardSynchronizationResults =
				map[string]dash0common.PersesDashboardSynchronizationResults{
					TestNamespaceName + "/retryable-dashboard": {
						SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
						SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
							{SynchronizationError: "rate limited", HttpStatusCode: 429},
						},
					},
					TestNamespaceName + "/validation-dashboard": {
						SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
						ValidationIssues:      []string{"invalid dashboard"},
						SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
							{SynchronizationError: "bad request", HttpStatusCode: 400},
						},
					},
				}
			Expect(k8sClient.Status().Update(ctx, monitoringResource)).To(Succeed())

			runnable.retryFailedSynchronizations(ctx)

			enqueued := drainQueue(queue)
			Expect(enqueued).To(ConsistOf(
				"PrometheusRule/retryable-rule",
				"PersesDashboard/retryable-dashboard",
			))
		})

		It("does not re-enqueue anything when the reconcilers are not watching", func() {
			stopWatching(prometheusRuleCrdReconciler.ThirdPartyResourceReconciler())
			stopWatching(persesDashboardCrdReconciler.ThirdPartyResourceReconciler())

			createPrometheusRuleInCluster(ctx, "retryable-rule")
			createPersesDashboardInCluster(ctx, "retryable-dashboard")

			monitoringResource := EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)
			monitoringResource.Status.PrometheusRuleSynchronizationResults =
				map[string]dash0common.PrometheusRuleSynchronizationResult{
					TestNamespaceName + "/retryable-rule": {
						SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
						SynchronizationResults: []dash0common.PrometheusRuleSynchronizationResultPerEndpointAndDataset{
							{
								SynchronizationErrors:               map[string]string{"group - alert": "server error"},
								SynchronizationErrorHttpStatusCodes: map[string]int{"group - alert": 503},
							},
						},
					},
				}
			monitoringResource.Status.PersesDashboardSynchronizationResults =
				map[string]dash0common.PersesDashboardSynchronizationResults{
					TestNamespaceName + "/retryable-dashboard": {
						SynchronizationStatus: dash0common.ThirdPartySynchronizationStatusFailed,
						SynchronizationResults: []dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset{
							{SynchronizationError: "rate limited", HttpStatusCode: 429},
						},
					},
				}
			Expect(k8sClient.Status().Update(ctx, monitoringResource)).To(Succeed())

			runnable.retryFailedSynchronizations(ctx)

			Expect(drainQueue(queue)).To(BeEmpty())
		})
	})

	Context("when retrying failed synchronizations for owned resources", Ordered, func() {
		ctx := context.Background()

		BeforeAll(func() {
			EnsureTestNamespaceExists(ctx, k8sClient)
		})

		It("re-syncs every owned resource that has a retryable synchronization error", func() {
			viewController := &recordingOwnedIacController{
				kind: "view",
				requests: []reconcile.Request{
					{NamespacedName: types.NamespacedName{Namespace: TestNamespaceName, Name: "view-1"}},
					{NamespacedName: types.NamespacedName{Namespace: TestNamespaceName, Name: "view-2"}},
				},
			}
			checkController := &recordingOwnedIacController{
				kind: "synthetic check",
				requests: []reconcile.Request{
					{NamespacedName: types.NamespacedName{Namespace: TestNamespaceName, Name: "check-1"}},
				},
			}
			runnable := NewSynchronizationRetryRunnable(
				k8sClient,
				nil,
				nil,
				[]OwnedIacResourceSynchronizationController{viewController, checkController},
				time.Minute,
				logd.FromContext(ctx),
			)

			runnable.retryFailedSynchronizationsForOwnedResources(ctx)

			Expect(viewController.reconciled).To(ConsistOf(viewController.requests))
			Expect(checkController.reconciled).To(ConsistOf(checkController.requests))
		})

		It("continues with the remaining owned resource types when listing one type fails", func() {
			failingController := &recordingOwnedIacController{kind: "view", listErr: errors.New("list failed")}
			workingController := &recordingOwnedIacController{
				kind: "synthetic check",
				requests: []reconcile.Request{
					{NamespacedName: types.NamespacedName{Namespace: TestNamespaceName, Name: "check-1"}},
				},
			}
			runnable := NewSynchronizationRetryRunnable(
				k8sClient,
				nil,
				nil,
				[]OwnedIacResourceSynchronizationController{failingController, workingController},
				time.Minute,
				logd.FromContext(ctx),
			)

			runnable.retryFailedSynchronizationsForOwnedResources(ctx)

			Expect(failingController.reconciled).To(BeEmpty())
			Expect(workingController.reconciled).To(ConsistOf(workingController.requests))
		})

		It("lists only the owned resources that have a retryable synchronization error", func() {
			viewReconciler := NewViewReconciler(
				k8sClient,
				types.UID("test-cluster-uid"),
				&DummyLeaderElectionAware{Leader: true},
				TestHTTPClient(),
			)

			createViewWithSynchronizationError(ctx, "view-server-error", 503, "server error")
			createViewWithSynchronizationError(ctx, "view-client-error", 400, "bad request")
			createViewWithSynchronizationError(ctx, "view-successful", 0, "")
			DeferCleanup(func() {
				deleteViewIfItExists(ctx, "view-server-error")
				deleteViewIfItExists(ctx, "view-client-error")
				deleteViewIfItExists(ctx, "view-successful")
			})

			requests, err := viewReconciler.CreateReconcileRequestsForRetryableSyncErrors(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(requests).To(ConsistOf(reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: TestNamespaceName, Name: "view-server-error"},
			}))
		})
	})
})

// recordingOwnedIacController is a test double for OwnedIacResourceSynchronizationController that records the reconcile
// requests it is asked to re-synchronize.
type recordingOwnedIacController struct {
	kind       string
	requests   []reconcile.Request
	listErr    error
	reconciled []reconcile.Request
}

func (s *recordingOwnedIacController) KindDisplayName() string { return s.kind }

func (s *recordingOwnedIacController) CreateReconcileRequestsForRetryableSyncErrors(
	_ context.Context,
) ([]reconcile.Request, error) {
	return s.requests, s.listErr
}

func (s *recordingOwnedIacController) Reconcile(
	_ context.Context,
	request reconcile.Request,
) (reconcile.Result, error) {
	s.reconciled = append(s.reconciled, request)
	return reconcile.Result{}, nil
}

func createViewWithSynchronizationError(ctx context.Context, name string, httpStatusCode int, syncError string) {
	view := &dash0v1alpha1.Dash0View{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: TestNamespaceName},
		Spec: dash0v1alpha1.Dash0ViewSpec{
			Type:    "resources",
			Display: dash0v1alpha1.Dash0ViewDisplay{Name: name},
		},
	}
	Expect(k8sClient.Create(ctx, view)).To(Succeed())

	syncStatus := dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
	if syncError != "" {
		syncStatus = dash0common.Dash0ApiResourceSynchronizationStatusFailed
	}
	view.Status = dash0v1alpha1.Dash0ViewStatus{
		SynchronizationStatus: syncStatus,
		SynchronizedAt:        metav1.Now(),
		SynchronizationResults: []dash0v1alpha1.Dash0ViewSynchronizationResultPerEndpointAndDataset{
			{
				SynchronizationStatus: syncStatus,
				Dash0ApiEndpoint:      ApiEndpointTest,
				SynchronizationError:  syncError,
				HttpStatusCode:        httpStatusCode,
			},
		},
	}
	Expect(k8sClient.Status().Update(ctx, view)).To(Succeed())
}

func deleteViewIfItExists(ctx context.Context, name string) {
	view := &dash0v1alpha1.Dash0View{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: TestNamespaceName}}
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, view))).To(Succeed())
}

func startWatching(ctx context.Context, resourceReconciler ThirdPartyResourceReconciler) {
	_, cancel := context.WithCancel(ctx)
	resourceReconciler.SetControllerStopFunction(&cancel)
}

func stopWatching(resourceReconciler ThirdPartyResourceReconciler) {
	resourceReconciler.SetControllerStopFunction(nil)
}

func drainQueue(queue *workqueue.Typed[ThirdPartyResourceSyncJob]) []string {
	enqueued := make([]string, 0)
	for queue.Len() > 0 {
		item, shutdown := queue.Get()
		if shutdown {
			break
		}
		enqueued = append(
			enqueued,
			item.dash0ApiResource.GetKind()+"/"+item.dash0ApiResource.GetName(),
		)
		queue.Done(item)
	}
	return enqueued
}

func createPrometheusRuleInCluster(ctx context.Context, name string) {
	ruleResource := createPrometheusRuleResourceWithObjectMeta(
		prometheusv1.PrometheusRuleSpec{
			Groups: []prometheusv1.RuleGroup{
				{
					Name: "group",
					Rules: []prometheusv1.Rule{
						{
							Alert: "alert",
							Expr:  intstr.FromString("vector(1)"),
						},
					},
				},
			},
		},
		metav1.ObjectMeta{
			Name:      name,
			Namespace: TestNamespaceName,
		},
	)
	Expect(k8sClient.Create(ctx, &ruleResource)).To(Succeed())
}

func createPersesDashboardInCluster(ctx context.Context, name string) {
	dashboardResource := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "perses.dev/" + persesDashboardV1Alpha1,
			"kind":       "PersesDashboard",
			"metadata": map[string]any{
				"name":      name,
				"namespace": TestNamespaceName,
			},
			"spec": map[string]any{
				"display":  map[string]any{"name": name},
				"duration": "1h",
				"layouts":  []any{},
				"panels":   map[string]any{},
			},
		},
	}
	Expect(k8sClient.Create(ctx, &dashboardResource)).To(Succeed())
}

func deletePrometheusRuleIfItExists(ctx context.Context, name string) {
	deleteThirdPartyResourceIfItExists(ctx, "monitoring.coreos.com", "v1", "PrometheusRule", name)
}

func deletePersesDashboardIfItExists(ctx context.Context, name string) {
	deleteThirdPartyResourceIfItExists(ctx, "perses.dev", persesDashboardV1Alpha1, "PersesDashboard", name)
}

func deleteThirdPartyResourceIfItExists(ctx context.Context, group, version, kind, name string) {
	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
	resource.SetNamespace(TestNamespaceName)
	resource.SetName(name)
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, resource))).To(Succeed())
}
