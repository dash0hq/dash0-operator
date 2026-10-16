// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The third-party resource synchronization queue", func() {
	var queue *ThirdPartyResourceSyncQueue

	BeforeEach(func() {
		queue = NewThirdPartyResourceSyncQueue("test-queue")
	})

	AfterEach(func() {
		queue.ShutDown()
	})

	It("replaces a pending job for the same resource", func() {
		reconciler := &PrometheusRuleReconciler{}
		queue.Add(syncJob(reconciler, "rule", "1", upsertAction))
		queue.Add(syncJob(reconciler, "rule", "2", upsertAction))
		queue.Add(syncJob(reconciler, "rule", "3", deleteAction))
		Expect(queue.Len()).To(Equal(1))

		job, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())
		Expect(job.dash0ApiResource.GetResourceVersion()).To(Equal("3"))
		Expect(job.action).To(Equal(deleteAction))
		queue.Done(job)
		Expect(queue.Len()).To(Equal(0))
	})

	It("keeps separate jobs for different resources and resource types", func() {
		queue.Add(syncJob(&PrometheusRuleReconciler{}, "resource-1", "1", upsertAction))
		queue.Add(syncJob(&PrometheusRuleReconciler{}, "resource-2", "1", upsertAction))
		queue.Add(syncJob(&PersesDashboardReconciler{}, "resource-1", "1", upsertAction))
		Expect(queue.Len()).To(Equal(3))
	})

	It("processes a job that is added while the same resource is in flight after the in-flight job is done", func() {
		reconciler := &PrometheusRuleReconciler{}
		queue.Add(syncJob(reconciler, "rule", "1", upsertAction))
		inFlight, _ := queue.Get()

		queue.Add(syncJob(reconciler, "rule", "2", upsertAction))
		Expect(queue.Len()).To(Equal(2))

		queue.Done(inFlight)
		Expect(queue.Len()).To(Equal(1))
		job, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())
		Expect(job.dash0ApiResource.GetResourceVersion()).To(Equal("2"))
		Expect(queue.Len()).To(Equal(1))
		queue.Done(job)
		Expect(queue.Len()).To(Equal(0))
	})

	It("marks a job as done even if its resource has been modified during processing", func() {
		reconciler := &PrometheusRuleReconciler{}
		queue.Add(syncJob(reconciler, "rule", "1", upsertAction))
		inFlight, _ := queue.Get()
		inFlight.dash0ApiResource.SetName("renamed")
		queue.Done(inFlight)

		queue.Add(syncJob(reconciler, "rule", "2", upsertAction))
		jobs := make(chan ThirdPartyResourceSyncJob, 1)
		go func() {
			job, _ := queue.Get()
			jobs <- job
		}()
		var job ThirdPartyResourceSyncJob
		Eventually(jobs).Should(Receive(&job))
		Expect(job.dash0ApiResource.GetResourceVersion()).To(Equal("2"))
		queue.Done(job)
	})

	It("skips a resource whose latest job has already been taken by an earlier Get", func() {
		reconciler := &PrometheusRuleReconciler{}
		inFlightJob := syncJob(reconciler, "rule", "1", upsertAction)
		queue.Add(inFlightJob)
		inFlight, _ := queue.Get()
		// Re-add the key without a job, as Add does when an earlier Get has taken its job in between.
		queue.keys.Add(inFlightJob.key())
		queue.Done(inFlight)
		queue.Add(syncJob(reconciler, "other-rule", "1", upsertAction))

		job, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())
		Expect(job.dash0ApiResource.GetName()).To(Equal("other-rule"))
		queue.Done(job)
		Expect(queue.Len()).To(Equal(0))
	})

	It("reports a shutdown", func() {
		queue.ShutDown()
		_, shutdown := queue.Get()
		Expect(shutdown).To(BeTrue())
	})

	It("returns jobs added before the shutdown and discards jobs added after it", func() {
		reconciler := &PrometheusRuleReconciler{}
		queue.Add(syncJob(reconciler, "before-shutdown", "1", upsertAction))
		queue.ShutDown()
		queue.Add(syncJob(reconciler, "after-shutdown", "1", upsertAction))
		Expect(queue.Len()).To(Equal(1))

		job, shutdown := queue.Get()
		Expect(shutdown).To(BeFalse())
		Expect(job.dash0ApiResource.GetName()).To(Equal("before-shutdown"))
		queue.Done(job)
		_, shutdown = queue.Get()
		Expect(shutdown).To(BeTrue())
		Expect(queue.Len()).To(Equal(0))
	})
})

var _ = Describe("The update event handlers of the third-party resource reconcilers", func() {
	ctx := context.Background()

	type testCase struct {
		createReconciler func(*ThirdPartyResourceSyncQueue) ThirdPartyResourceReconciler
	}

	prometheusRules := testCase{
		createReconciler: func(queue *ThirdPartyResourceSyncQueue) ThirdPartyResourceReconciler {
			return &PrometheusRuleReconciler{queue: queue}
		},
	}
	persesDashboards := testCase{
		createReconciler: func(queue *ThirdPartyResourceSyncQueue) ThirdPartyResourceReconciler {
			return &PersesDashboardReconciler{queue: queue}
		},
	}

	DescribeTable("enqueue a synchronization job only if the resource version has changed",
		func(tc testCase, oldResourceVersion string, newResourceVersion string, expectedQueueLength int) {
			queue := NewThirdPartyResourceSyncQueue("test-queue")
			defer queue.ShutDown()
			reconciler := tc.createReconciler(queue)

			reconciler.Update(
				ctx,
				event.TypedUpdateEvent[*unstructured.Unstructured]{
					ObjectOld: resourceWithVersion("resource", oldResourceVersion),
					ObjectNew: resourceWithVersion("resource", newResourceVersion),
				},
				&controllertest.TypedQueue[reconcile.Request]{},
			)

			Expect(queue.Len()).To(Equal(expectedQueueLength))
		},
		Entry("Prometheus rule, unchanged resource version", prometheusRules, "1", "1", 0),
		Entry("Prometheus rule, changed resource version", prometheusRules, "1", "2", 1),
		Entry("Perses dashboard, unchanged resource version", persesDashboards, "1", "1", 0),
		Entry("Perses dashboard, changed resource version", persesDashboards, "1", "2", 1),
	)
})

var _ = Describe("Writing synchronization results to the Dash0 monitoring resource status", func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	BeforeEach(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	AfterEach(func() {
		DeleteMonitoringResourceIfItExists(ctx, k8sClient)
	})

	It("keeps the results of concurrent writes for different resources", func() {
		monitoringResource := EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)
		reconcilers := []ThirdPartyResourceReconciler{
			&PrometheusRuleReconciler{Client: k8sClient, apiReader: k8sClient},
			&PersesDashboardReconciler{Client: k8sClient, apiReader: k8sClient},
		}
		const writesPerReconciler = 10
		var wg sync.WaitGroup
		for _, reconciler := range reconcilers {
			for i := range writesPerReconciler {
				wg.Go(func() {
					writeSynchronizationResultToDash0MonitoringStatus(
						ctx,
						reconciler,
						monitoringResource.DeepCopy(),
						resourceWithVersion(fmt.Sprintf("resource-%d", i), "1"),
						synchronizationResults{},
						logger,
					)
				})
			}
		}
		wg.Wait()

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(monitoringResource), monitoringResource)).To(Succeed())
		Expect(monitoringResource.Status.PrometheusRuleSynchronizationResults).To(HaveLen(writesPerReconciler))
		Expect(monitoringResource.Status.PersesDashboardSynchronizationResults).To(HaveLen(writesPerReconciler))
	})

	It("removes sub-fields of the previous result for the same resource", func() {
		monitoringResource := EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)
		reconciler := &PrometheusRuleReconciler{Client: k8sClient, apiReader: k8sClient}
		ruleResource := resourceWithVersion("rule", "1")
		qualifiedName := ruleResource.GetNamespace() + "/" + ruleResource.GetName()

		writeSynchronizationResultToDash0MonitoringStatus(
			ctx,
			reconciler,
			monitoringResource.DeepCopy(),
			ruleResource,
			synchronizationResults{validationIssues: map[string][]string{"group - alert": {"invalid"}}},
			logger,
		)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(monitoringResource), monitoringResource)).To(Succeed())
		Expect(monitoringResource.Status.PrometheusRuleSynchronizationResults[qualifiedName].InvalidRules).To(HaveLen(1))

		writeSynchronizationResultToDash0MonitoringStatus(
			ctx,
			reconciler,
			monitoringResource.DeepCopy(),
			ruleResource,
			synchronizationResults{},
			logger,
		)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(monitoringResource), monitoringResource)).To(Succeed())
		result := monitoringResource.Status.PrometheusRuleSynchronizationResults[qualifiedName]
		Expect(result.InvalidRules).To(BeEmpty())
		Expect(result.InvalidRulesTotal).To(Equal(0))
	})

	It("writes the complete result when the previous attempt has failed", func() {
		monitoringResource := EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)
		reconciler := &PrometheusRuleReconciler{
			Client:    &firstStatusPatchFailsClient{Client: k8sClient},
			apiReader: k8sClient,
		}
		ruleResource := resourceWithVersion("rule", "1")
		qualifiedName := ruleResource.GetNamespace() + "/" + ruleResource.GetName()

		writeSynchronizationResultToDash0MonitoringStatus(
			ctx,
			reconciler,
			monitoringResource.DeepCopy(),
			ruleResource,
			synchronizationResults{validationIssues: map[string][]string{"group - alert": {"invalid"}}},
			logger,
		)

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(monitoringResource), monitoringResource)).To(Succeed())
		result := monitoringResource.Status.PrometheusRuleSynchronizationResults[qualifiedName]
		Expect(result.SynchronizationStatus).To(Equal(dash0common.ThirdPartySynchronizationStatusFailed))
		Expect(result.InvalidRulesTotal).To(Equal(1))
		Expect(result.InvalidRules).To(HaveLen(1))
	})
})

// firstStatusPatchFailsClient lets the first status patch fail without sending it to the API server.
type firstStatusPatchFailsClient struct {
	client.Client
	failed atomic.Bool
}

func (c *firstStatusPatchFailsClient) Status() client.SubResourceWriter {
	return &firstPatchFailsStatusWriter{SubResourceWriter: c.Client.Status(), failed: &c.failed}
}

type firstPatchFailsStatusWriter struct {
	client.SubResourceWriter
	failed *atomic.Bool
}

func (w *firstPatchFailsStatusWriter) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.SubResourcePatchOption,
) error {
	if w.failed.CompareAndSwap(false, true) {
		return errors.New("simulated transient error")
	}
	return w.SubResourceWriter.Patch(ctx, obj, patch, opts...)
}

func syncJob(
	reconciler ThirdPartyResourceReconciler,
	name string,
	resourceVersion string,
	action apiAction,
) ThirdPartyResourceSyncJob {
	return ThirdPartyResourceSyncJob{
		thirdPartyResourceReconciler: reconciler,
		dash0ApiResource:             resourceWithVersion(name, resourceVersion),
		action:                       action,
	}
}

func resourceWithVersion(name string, resourceVersion string) *unstructured.Unstructured {
	resource := &unstructured.Unstructured{}
	resource.SetNamespace("test-namespace")
	resource.SetName(name)
	resource.SetResourceVersion(resourceVersion)
	return resource
}
