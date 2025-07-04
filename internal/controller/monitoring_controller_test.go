// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

// Maintenance note: the canonical order/grouping for imports in tests is:
// - standard library imports
// - external third-party library imports, except for test libraries
// - internal imports, except for internal test packages
// - external third-party test libraries
// - internal test packages

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	namespace                    = TestNamespaceName
	extraMonitoringResourceNames []types.NamespacedName
	operatorNamespace            = OperatorNamespace
)

var _ = Describe("The monitoring resource controller", Ordered, func() {
	ctx := context.Background()
	var createdObjectsMonitoringControllerTest []client.Object

	var monitoringReconciler *MonitoringReconciler

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjectsMonitoringControllerTest = make([]client.Object, 0)

		instrumenter := instrumentation.NewInstrumenter(
			k8sClient,
			clientset,
			recorder,
			TestImages,
			util.ExtraConfigDefaults,
			OTelCollectorNodeLocalBaseUrlTest,
			nil,
			false,
		)
		oTelColResourceManager := &otelcolresources.OTelColResourceManager{
			Client:                    k8sClient,
			Scheme:                    k8sClient.Scheme(),
			OperatorManagerDeployment: OperatorManagerDeployment,
			OTelCollectorNamePrefix:   OTelCollectorNamePrefixTest,
			ExtraConfig:               util.ExtraConfigDefaults,
		}
		collectorManager := &collectors.CollectorManager{
			Client:                 k8sClient,
			Clientset:              clientset,
			OTelColResourceManager: oTelColResourceManager,
		}
		monitoringReconciler = &MonitoringReconciler{
			Client:                 k8sClient,
			Clientset:              clientset,
			Instrumenter:           instrumenter,
			Images:                 TestImages,
			OperatorNamespace:      OperatorNamespace,
			CollectorManager:       collectorManager,
			DanglingEventsTimeouts: &DanglingEventsTimeoutsTest,
		}
	})

	AfterEach(func() {
		createdObjectsMonitoringControllerTest = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsMonitoringControllerTest)
		DeleteAllEvents(ctx, clientset, namespace)
	})

	Describe("when the Dash0 monitoring resource exists", Ordered, func() {
		BeforeEach(func() {
			EnsureMonitoringResourceExists(ctx, k8sClient)
		})

		AfterEach(func() {
			DeleteMonitoringResource(ctx, k8sClient)
			for _, name := range extraMonitoringResourceNames {
				DeleteMonitoringResourceByName(ctx, k8sClient, name, true)
			}
		})

		Describe("when reconciling", func() {
			It("should successfully run the first reconcile (no modifiable workloads exist)", func() {
				By("Trigger reconcile request")
				triggerReconcileRequest(ctx, monitoringReconciler)
				verifyMonitoringResourceIsAvailable(ctx)
				VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
			})

			It("should successfully run multiple reconciles (no modifiable workloads exist)", func() {
				By("First reconcile request")
				triggerReconcileRequest(ctx, monitoringReconciler)

				firstAvailableStatusCondition := verifyMonitoringResourceIsAvailable(ctx)
				originalTransitionTimestamp := firstAvailableStatusCondition.LastTransitionTime.Time

				time.Sleep(50 * time.Millisecond)

				By("First reconcile request")
				triggerReconcileRequest(ctx, monitoringReconciler)

				// The LastTransitionTime should not change with subsequent reconciliations.
				secondAvailableCondition := verifyMonitoringResourceIsAvailable(ctx)
				Expect(secondAvailableCondition.LastTransitionTime.Time).To(Equal(originalTransitionTimestamp))

				VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)
			})

			It("should mark only the most recent resource as available and the other ones as degraded when multiple resources exist", func() {
				firstName := MonitoringResourceQualifiedName
				firstMonitoringResource := &dash0v1alpha1.Dash0Monitoring{}
				Expect(k8sClient.Get(ctx, firstName, firstMonitoringResource)).To(Succeed())
				time.Sleep(10 * time.Millisecond)
				secondName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-2"}
				extraMonitoringResourceNames = append(extraMonitoringResourceNames, secondName)
				CreateDefaultMonitoringResource(ctx, k8sClient, secondName)
				time.Sleep(10 * time.Millisecond)
				thirdName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-monitoring-test-resource-3"}
				extraMonitoringResourceNames = append(extraMonitoringResourceNames, thirdName)
				CreateDefaultMonitoringResource(ctx, k8sClient, thirdName)

				triggerReconcileRequestForName(ctx, monitoringReconciler, firstName)
				triggerReconcileRequestForName(ctx, monitoringReconciler, secondName)
				triggerReconcileRequestForName(ctx, monitoringReconciler, thirdName)

				Eventually(func(g Gomega) {
					resource1Available := LoadMonitoringResourceStatusCondition(ctx, k8sClient, firstName, dash0v1alpha1.ConditionTypeAvailable)
					resource1Degraded := LoadMonitoringResourceStatusCondition(ctx, k8sClient, firstName, dash0v1alpha1.ConditionTypeDegraded)
					resource2Available := LoadMonitoringResourceStatusCondition(ctx, k8sClient, secondName, dash0v1alpha1.ConditionTypeAvailable)
					resource2Degraded := LoadMonitoringResourceStatusCondition(ctx, k8sClient, secondName, dash0v1alpha1.ConditionTypeDegraded)
					resource3Available := LoadMonitoringResourceStatusCondition(ctx, k8sClient, thirdName, dash0v1alpha1.ConditionTypeAvailable)
					resource3Degraded := LoadMonitoringResourceStatusCondition(ctx, k8sClient, thirdName, dash0v1alpha1.ConditionTypeDegraded)

					// The first two resource should have been marked as degraded.
					VerifyResourceStatusCondition(
						g,
						resource1Available,
						metav1.ConditionFalse,
						"NewerResourceIsPresent",
						"There is a more recently created Dash0 monitoring resource in this namespace, please remove all "+
							"but one resource instance.",
					)
					VerifyResourceStatusCondition(
						g,
						resource1Degraded,
						metav1.ConditionTrue,
						"NewerResourceIsPresent",
						"There is a more recently created Dash0 monitoring resource in this namespace, please remove all "+
							"but one resource instance.",
					)
					VerifyResourceStatusCondition(g, resource2Available, metav1.ConditionFalse, "NewerResourceIsPresent",
						"There is a more recently created Dash0 monitoring resource in this namespace, please remove all "+
							"but one resource instance.")
					VerifyResourceStatusCondition(g, resource2Degraded, metav1.ConditionTrue, "NewerResourceIsPresent",
						"There is a more recently created Dash0 monitoring resource in this namespace, please remove all "+
							"but one resource instance.")

					// The third (and most recent) resource should have been marked as available.
					VerifyResourceStatusCondition(
						g,
						resource3Available,
						metav1.ConditionTrue,
						"ReconcileFinished",
						"Dash0 monitoring is active in this namespace now.",
					)
					g.Expect(resource3Degraded).To(BeNil())

				}, timeout, pollingInterval).Should(Succeed())
			})

			It("should self-heal a degraded resource state", func() {
				monitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, Default)
				monitoringResource.EnsureResourceIsMarkedAsDegraded("TestReason", "This is a test message.")
				Expect(k8sClient.Status().Update(ctx, monitoringResource)).To(Succeed())
				verifyMonitoringResourceIsDegraded(ctx)

				// reconciling the resource should self-heal the degraded state
				triggerReconcileRequest(ctx, monitoringReconciler)
				verifyMonitoringResourceIsAvailable(ctx)
			})
		})

		// Note: This is only one test case for the "instrument existing workloads" scenario, describing the most basic
		// case. All other cases are covered in ../instrumenter/instrumenter_test.go.
		DescribeTable("when instrumenting existing workloads", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsMonitoringControllerTest = append(createdObjectsMonitoringControllerTest, workload.Get())

			triggerReconcileRequest(ctx, monitoringReconciler)

			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
		}, Entry("should instrument an existing cron job", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing daemon set", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing deployment", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing ownerless replicaset", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing stateful set", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
			},
		}),
		)

		// Note: This is only one test case for the "revert instrumented workloads" scenario, describing the most basic
		// case. All other cases are covered in ../instrumenter/instrumenter_test.go.
		DescribeTable("when deleting the Dash0 monitoring resource and reverting the instrumentation on cleanup", func(config WorkloadTestConfig) {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 monitoring
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			By("Trigger first reconcile request")
			triggerReconcileRequest(ctx, monitoringReconciler)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsMonitoringControllerTest = append(createdObjectsMonitoringControllerTest, workload.Get())

			By("deleting the Dash0 monitoring resource")
			monitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, Default)
			Expect(k8sClient.Delete(ctx, monitoringResource)).To(Succeed())

			By("trigger a reconcile request to revert the instrumented workload")
			triggerReconcileRequest(ctx, monitoringReconciler)

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifyWebhookIgnoreOnceLabelIsPresent(workload.GetObjectMeta())

			verifyMonitoringResourceDoesNotExist(ctx)
		}, Entry("should revert an instrumented cron job", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateInstrumentedCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should revert an instrumented daemon set", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateInstrumentedDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should revert an instrumented deployment", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateInstrumentedDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should revert an instrumented ownerless replica set", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateInstrumentedReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should revert an instrumented stateful set", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateInstrumentedStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
		}))
	})

	Describe("when the instrumentWorkloads setting changes on an existing Dash0 monitoring resource", Ordered, func() {
		AfterEach(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		DescribeTable("when switching from instrumentWorkloads=none to instrumentWorkloads=created-and-updated", func(config WorkloadTestConfig) {
			EnsureMonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.None)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsMonitoringControllerTest = append(createdObjectsMonitoringControllerTest, workload.Get())

			triggerReconcileRequest(ctx, monitoringReconciler)
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.CreatedAndUpdated)

			// Switching from instrumentWorkloads=none to instrumentWorkloads=created-and-updated has no effect.
			// Existing workloads are still not to be instrumented. The new setting only becomes effective when the next
			// resource is created or updated, and the webhook will take care of that.

			triggerReconcileRequest(ctx, monitoringReconciler)
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
		}, Entry("should instrument an existing cron job after switching from instrumentWorkloads=none to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should instrument an existing daemon set after switching from instrumentWorkloads=none to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should instrument an existing deployment after switching from instrumentWorkloads=none to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should instrument an existing ownerless replicaset after switching from instrumentWorkloads=none to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should instrument an existing stateful set after switching from instrumentWorkloads=none to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
		}),
		)

		DescribeTable("when instrumenting existing workloads after switching from instrumentWorkloads=none to instrumentWorkloads=all", func(config WorkloadTestConfig) {
			EnsureMonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.None)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsMonitoringControllerTest = append(createdObjectsMonitoringControllerTest, workload.Get())

			triggerReconcileRequest(ctx, monitoringReconciler)
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyPreFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.All)

			triggerReconcileRequest(ctx, monitoringReconciler)
			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
		}, Entry("should instrument an existing cron job after switching from instrumentWorkloads=none to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing daemon set after switching from instrumentWorkloads=none to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing deployment after switching from instrumentWorkloads=none to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing ownerless replicaset after switching from instrumentWorkloads=none to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing stateful set after switching from instrumentWorkloads=none to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
			},
		}),
		)

		DescribeTable("when removing instrumentation from workloads after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", func(config WorkloadTestConfig) {
			EnsureMonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.CreatedAndUpdated)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsMonitoringControllerTest = append(createdObjectsMonitoringControllerTest, workload.Get())

			triggerReconcileRequest(ctx, monitoringReconciler)
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyPreFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			DeleteAllEvents(ctx, clientset, namespace)

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.None)

			triggerReconcileRequest(ctx, monitoringReconciler)
			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifyWebhookIgnoreOnceLabelIsPresent(workload.GetObjectMeta())
		}, Entry("should remove instrumentation from an existing cron job after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateInstrumentedCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should remove instrumentation from an existing daemon set after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateInstrumentedDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should remove instrumentation from an existing deployment after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateInstrumentedDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should remove instrumentation from an existing ownerless replicaset after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateInstrumentedReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should remove instrumentation from an existing stateful set after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateInstrumentedStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
		}),
		)

		DescribeTable("when instrumenting existing workloads after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=all", func(config WorkloadTestConfig) {
			EnsureMonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.CreatedAndUpdated)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsMonitoringControllerTest = append(createdObjectsMonitoringControllerTest, workload.Get())

			triggerReconcileRequest(ctx, monitoringReconciler)
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyPreFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.All)

			triggerReconcileRequest(ctx, monitoringReconciler)
			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
		}, Entry("should instrument an existing cron job after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing daemon set after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing deployment after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing ownerless replicaset after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument an existing stateful set after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=all", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
			},
		}),
		)

		DescribeTable("when removing instrumentation from workloads after switching from instrumentWorkloads=all to instrumentWorkloads=none", func(config WorkloadTestConfig) {
			EnsureMonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.All)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsMonitoringControllerTest = append(createdObjectsMonitoringControllerTest, workload.Get())

			triggerReconcileRequest(ctx, monitoringReconciler)
			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			config.VerifyPreFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			DeleteAllEvents(ctx, clientset, namespace)

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.None)

			triggerReconcileRequest(ctx, monitoringReconciler)
			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifyWebhookIgnoreOnceLabelIsPresent(workload.GetObjectMeta())
		}, Entry("should remove instrumentation from an existing cron job after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should remove instrumentation from an existing daemon set after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should remove instrumentation from an existing deployment after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should remove instrumentation from an existing ownerless replicaset after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should remove instrumentation from an existing stateful set after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
		}),
		)

		DescribeTable("when switching from instrumentWorkloads=all to instrumentWorkloads=created-and-updated", func(config WorkloadTestConfig) {
			EnsureMonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.All)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsMonitoringControllerTest = append(createdObjectsMonitoringControllerTest, workload.Get())

			triggerReconcileRequest(ctx, monitoringReconciler)
			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			DeleteAllEvents(ctx, clientset, namespace)

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.CreatedAndUpdated)

			// Switching from instrumentWorkloads=all to instrumentWorkloads=created-and-updated has no effect.
			// Already instrumented workloads will not be uninstrumented.

			triggerReconcileRequest(ctx, monitoringReconciler)
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
		}, Entry("should remove instrumentation from an existing cron job after switching from instrumentWorkloads=all to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should remove instrumentation from an existing daemon set after switching from instrumentWorkloads=all to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should remove instrumentation from an existing deployment after switching from instrumentWorkloads=all to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should remove instrumentation from an existing ownerless replicaset after switching from instrumentWorkloads=all to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should remove instrumentation from an existing stateful set after switching from instrumentWorkloads=all to instrumentWorkloads=created-and-updated", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
			},
		}),
		)
	})

	Describe("when the Dash0 monitoring resource does not exist", func() {
		It("should not instrument workloads", func() {
			createdObjectsMonitoringControllerTest = verifyThatDeploymentIsNotBeingInstrumented(ctx, monitoringReconciler, createdObjectsMonitoringControllerTest)
		})
	})

	Describe("when the Dash0 monitoring resource exists and has InstrumentWorkloads=all set explicitly", Ordered, func() {
		BeforeAll(func() {
			monitoringResource := EnsureMonitoringResourceExists(ctx, k8sClient)
			monitoringResource.Spec.InstrumentWorkloads = dash0v1alpha1.All
			Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should instrument workloads", func() {
			createdObjectsMonitoringControllerTest = verifyThatDeploymentIsInstrumented(ctx, monitoringReconciler, createdObjectsMonitoringControllerTest)
		})
	})

	Describe("when the Dash0 monitoring resource exists and has an invalid InstrumentWorkloads setting", Ordered, func() {
		It("should not allow creating the resource with an invalid value", func() {
			By("creating the Dash0 monitoring resource")
			Expect(k8sClient.Create(ctx, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MonitoringResourceQualifiedName.Name,
					Namespace: MonitoringResourceQualifiedName.Namespace,
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					InstrumentWorkloads: "invalid",
					Export: &dash0v1alpha1.Export{
						Dash0: &dash0v1alpha1.Dash0Configuration{
							Endpoint: EndpointDash0Test,
							Authorization: dash0v1alpha1.Authorization{
								Token: &AuthorizationTokenTest,
							},
						},
					},
				},
			})).ToNot(Succeed())
		})

		It("should not allow to update the resource with an invalid value", func() {
			monitoringResource := EnsureMonitoringResourceExists(ctx, k8sClient)
			monitoringResource.Spec.InstrumentWorkloads = "invalid"
			Expect(k8sClient.Update(ctx, monitoringResource)).ToNot(Succeed())
		})
	})

	Describe("when the Dash0 monitoring resource exists but has InstrumentWorkloads=none set", Ordered, func() {
		BeforeAll(func() {
			monitoringResource := EnsureMonitoringResourceExists(ctx, k8sClient)
			monitoringResource.Spec.InstrumentWorkloads = dash0v1alpha1.None
			Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjectsMonitoringControllerTest = verifyThatDeploymentIsNotBeingInstrumented(ctx, monitoringReconciler, createdObjectsMonitoringControllerTest)
		})
	})

	Describe("when the Dash0 monitoring resource exists but has InstrumentWorkloads=created-and-updated set", Ordered, func() {
		BeforeAll(func() {
			monitoringResource := EnsureMonitoringResourceExists(ctx, k8sClient)
			monitoringResource.Spec.InstrumentWorkloads = dash0v1alpha1.CreatedAndUpdated
			Expect(k8sClient.Update(ctx, monitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjectsMonitoringControllerTest = verifyThatDeploymentIsNotBeingInstrumented(ctx, monitoringReconciler, createdObjectsMonitoringControllerTest)
		})
	})

	Describe("when managing the collector resources", func() {
		BeforeEach(func() {
			EnsureMonitoringResourceExists(ctx, k8sClient)
		})

		AfterEach(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should add and remove the collector resources", func() {
			By("Trigger first reconcile request")
			triggerReconcileRequest(ctx, monitoringReconciler)
			VerifyCollectorResources(ctx, k8sClient, operatorNamespace, EndpointDash0Test, AuthorizationTokenTest)

			monitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, Default)
			Expect(k8sClient.Delete(ctx, monitoringResource)).To(Succeed())
			By("Trigger a reconcile request to trigger removing the collector resources")
			triggerReconcileRequest(ctx, monitoringReconciler)

			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)

			verifyMonitoringResourceDoesNotExist(ctx)
		})
	})
})

func verifyThatDeploymentIsInstrumented(ctx context.Context, monitoringReconciler *MonitoringReconciler, createdObjects []client.Object) []client.Object {
	name := UniqueName(DeploymentNamePrefix)
	By("Inititalize a deployment")
	deployment := CreateBasicDeployment(ctx, k8sClient, namespace, name)
	createdObjects = append(createdObjects, deployment)

	triggerReconcileRequest(ctx, monitoringReconciler)

	verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
	VerifyModifiedDeployment(GetDeployment(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())

	return createdObjects
}

func verifyThatDeploymentIsNotBeingInstrumented(ctx context.Context, monitoringReconciler *MonitoringReconciler, createdObjects []client.Object) []client.Object {
	name := UniqueName(DeploymentNamePrefix)
	By("Inititalize a deployment")
	deployment := CreateDeploymentWithOptOutLabel(ctx, k8sClient, namespace, name)
	createdObjects = append(createdObjects, deployment)

	triggerReconcileRequest(ctx, monitoringReconciler)

	VerifyNoEvents(ctx, clientset, namespace)
	VerifyDeploymentWithOptOutLabel(GetDeployment(ctx, k8sClient, namespace, name))

	return createdObjects
}

func triggerReconcileRequest(ctx context.Context, monitoringReconciler *MonitoringReconciler) {
	triggerReconcileRequestForName(ctx, monitoringReconciler, MonitoringResourceQualifiedName)
}

func triggerReconcileRequestForName(
	ctx context.Context,
	monitoringReconciler *MonitoringReconciler,
	monitoringResourceName types.NamespacedName,
) {
	By("Trigger a monitoring resource reconcile request")
	_, err := monitoringReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: monitoringResourceName,
	})
	Expect(err).NotTo(HaveOccurred())
}

func verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx context.Context, namespace string, name string) {
	verifyMonitoringResourceIsAvailable(ctx)
	VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
}

func verifyMonitoringResourceIsAvailable(ctx context.Context) *metav1.Condition {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		monitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
		availableCondition = meta.FindStatusCondition(monitoringResource.Status.Conditions, string(dash0v1alpha1.ConditionTypeAvailable))
		g.Expect(availableCondition).NotTo(BeNil())
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
		degradedCondition := meta.FindStatusCondition(monitoringResource.Status.Conditions, string(dash0v1alpha1.ConditionTypeDegraded))
		g.Expect(degradedCondition).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
	return availableCondition
}

func verifyMonitoringResourceIsDegraded(ctx context.Context) *metav1.Condition {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		monitoringResource := LoadMonitoringResourceOrFail(ctx, k8sClient, g)
		availableCondition = meta.FindStatusCondition(monitoringResource.Status.Conditions, string(dash0v1alpha1.ConditionTypeAvailable))
		g.Expect(availableCondition).NotTo(BeNil())
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
		degradedCondition := meta.FindStatusCondition(monitoringResource.Status.Conditions, string(dash0v1alpha1.ConditionTypeDegraded))
		g.Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
	}, timeout, pollingInterval).Should(Succeed())
	return availableCondition
}

func verifyMonitoringResourceDoesNotExist(ctx context.Context) {
	Eventually(func(g Gomega) {
		VerifyMonitoringResourceDoesNotExist(ctx, k8sClient, g)
	}, timeout, pollingInterval).Should(Succeed())
}
