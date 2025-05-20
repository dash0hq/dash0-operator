// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/controller"
	"github.com/dash0hq/dash0-operator/internal/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The Dash0 webhook and the Dash0 controller", Ordered, func() {
	var reconciler *controller.MonitoringReconciler
	var dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring
	var createdObjects []client.Object

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)

		recorder := manager.GetEventRecorderFor("dash0-monitoring-controller")
		instrumenter := instrumentation.NewInstrumenter(
			k8sClient,
			clientset,
			recorder,
			TestImages,
			OTelCollectorBaseUrlTest,
			false,
			nil,
		)
		oTelColResourceManager := &otelcolresources.OTelColResourceManager{
			Client:                    k8sClient,
			Scheme:                    k8sClient.Scheme(),
			OperatorManagerDeployment: OperatorManagerDeployment,
			OTelCollectorNamePrefix:   OTelCollectorNamePrefixTest,
			ExtraConfig:               &util.ExtraConfigDefaults,
		}
		collectorManager := &collectors.CollectorManager{
			Client:                 k8sClient,
			Clientset:              clientset,
			OTelColResourceManager: oTelColResourceManager,
		}

		reconciler = &controller.MonitoringReconciler{
			Client:                 k8sClient,
			Clientset:              clientset,
			Instrumenter:           instrumenter,
			Images:                 TestImages,
			OperatorNamespace:      OperatorNamespace,
			CollectorManager:       collectorManager,
			DanglingEventsTimeouts: &DanglingEventsTimeoutsTest,
		}

		dash0MonitoringResource = EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
	})

	AfterAll(func() {
		DeleteMonitoringResource(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		DeleteAllEvents(ctx, clientset, TestNamespaceName)
	})

	DescribeTable("when attaching events to their involved objects", func(config WorkloadTestConfig) {
		name := UniqueName(config.WorkloadNamePrefix)
		workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
		createdObjects = append(createdObjects, workload.Get())
		workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
		event := VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")

		Expect(event.InvolvedObject.UID).To(BeEmpty())
		Expect(event.InvolvedObject.ResourceVersion).To(BeEmpty())

		triggerReconcileRequest(ctx, reconciler, dash0MonitoringResource)

		Eventually(func(g Gomega) {
			// refetch event and check that the UID and ResourceVersion are set correctly now
			var err error
			event, err = clientset.CoreV1().Events(TestNamespaceName).Get(ctx, event.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(event.InvolvedObject.UID).To(Equal(workload.Get().GetUID()))
			g.Expect(event.InvolvedObject.ResourceVersion).To(Equal(workload.Get().GetResourceVersion()))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	}, Entry("should attach event to a cron job", WorkloadTestConfig{
		WorkloadNamePrefix: CronJobNamePrefix,
		CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
		GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should attach event to a daemon set", WorkloadTestConfig{
		WorkloadNamePrefix: DaemonSetNamePrefix,
		CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
		GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should attach event to a deployment", WorkloadTestConfig{
		WorkloadNamePrefix: DeploymentNamePrefix,
		CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
		GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should attach event to a job", WorkloadTestConfig{
		WorkloadNamePrefix: JobNamePrefix,
		CreateFn:           WrapJobFnAsTestableWorkload(CreateBasicJob),
		GetFn:              WrapJobFnAsTestableWorkload(GetJob),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedJob(workload.Get().(*batchv1.Job), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should attach event to a ownerless pod", WorkloadTestConfig{
		WorkloadNamePrefix: PodNamePrefix,
		CreateFn:           WrapPodFnAsTestableWorkload(CreateBasicPod),
		GetFn:              WrapPodFnAsTestableWorkload(GetPod),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedPod(workload.Get().(*corev1.Pod), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should attach event to a ownerless replica set", WorkloadTestConfig{
		WorkloadNamePrefix: ReplicaSetNamePrefix,
		CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
		GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
		},
	}), Entry("should attach event to a stateful set", WorkloadTestConfig{
		WorkloadNamePrefix: StatefulSetNamePrefix,
		CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
		GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
		VerifyFn: func(workload TestableWorkload) {
			VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
		},
	}))
})

func triggerReconcileRequest(
	ctx context.Context,
	reconciler *controller.MonitoringReconciler,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
) {
	By("Trigger reconcile request")
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: dash0MonitoringResource.Namespace,
			Name:      dash0MonitoringResource.Name,
		},
	})
	Expect(err).NotTo(HaveOccurred())
}
