// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	olderOperatorControllerImageLabel = "some-registry_com_1234_dash0hq_operator-controller_0.9.8"
	olderInitContainerImageLabel      = "some-registry_com_1234_dash0hq_instrumentation_2.3.4"
)

var (
	namespace = TestNamespaceName

	timeout         = 10 * time.Second
	pollingInterval = 50 * time.Millisecond

	extraDash0MonitoringResourceNames = []types.NamespacedName{}

	operatorNamespace = Dash0OperatorNamespace
)

var _ = Describe("The Dash0 controller", Ordered, func() {
	ctx := context.Background()
	var createdObjects []client.Object

	var reconciler *Dash0Reconciler

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)

		oTelColResourceManager := &otelcolresources.OTelColResourceManager{
			Client:                  k8sClient,
			Scheme:                  k8sClient.Scheme(),
			DeploymentSelfReference: DeploymentSelfReference,
			OTelCollectorNamePrefix: "unit-test",
		}
		backendConnectionManager := &backendconnection.BackendConnectionManager{
			Client:                 k8sClient,
			Clientset:              clientset,
			OTelColResourceManager: oTelColResourceManager,
		}
		reconciler = &Dash0Reconciler{
			Client:                   k8sClient,
			Clientset:                clientset,
			Recorder:                 recorder,
			Scheme:                   k8sClient.Scheme(),
			Images:                   TestImages,
			OTelCollectorBaseUrl:     "http://dash0-operator-opentelemetry-collector.dash0-system.svc.cluster.local:4318",
			OperatorNamespace:        Dash0OperatorNamespace,
			BackendConnectionManager: backendConnectionManager,
			DanglingEventsTimeouts: &DanglingEventsTimeouts{
				InitialTimeout: 0 * time.Second,
				Backoff: wait.Backoff{
					Steps:    1,
					Duration: 0 * time.Second,
					Factor:   1,
					Jitter:   0,
				},
			},
		}
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		DeleteAllEvents(ctx, clientset, namespace)
	})

	Describe("when the Dash0 monitoring resource exists", Ordered, func() {
		BeforeEach(func() {
			EnsureDash0MonitoringResourceExists(ctx, k8sClient)
		})

		AfterEach(func() {
			RemoveDash0MonitoringResource(ctx, k8sClient)
			for _, name := range extraDash0MonitoringResourceNames {
				RemoveDash0MonitoringResourceByName(ctx, k8sClient, name, true)
			}
		})

		Describe("when reconciling", func() {
			It("should successfully run the first reconcile (no modifiable workloads exist)", func() {
				By("Trigger reconcile request")
				triggerReconcileRequest(ctx, reconciler, "")
				verifyDash0MonitoringResourceIsAvailable(ctx)
				VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)
			})

			It("should successfully run multiple reconciles (no modifiable workloads exist)", func() {
				triggerReconcileRequest(ctx, reconciler, "First reconcile request")

				firstAvailableStatusCondition := verifyDash0MonitoringResourceIsAvailable(ctx)
				originalTransitionTimestamp := firstAvailableStatusCondition.LastTransitionTime.Time

				time.Sleep(50 * time.Millisecond)

				triggerReconcileRequest(ctx, reconciler, "Second reconcile request")

				// The LastTransitionTime should not change with subsequent reconciliations.
				secondAvailableCondition := verifyDash0MonitoringResourceIsAvailable(ctx)
				Expect(secondAvailableCondition.LastTransitionTime.Time).To(Equal(originalTransitionTimestamp))

				VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)
			})

			It("should mark only the most recent resource as available and the other ones as degraded when multiple "+
				"resources exist", func() {
				firstDash0MonitoringResource := &dash0v1alpha1.Dash0Monitoring{}
				Expect(k8sClient.Get(ctx, Dash0MonitoringResourceQualifiedName, firstDash0MonitoringResource)).To(Succeed())
				time.Sleep(10 * time.Millisecond)
				secondName := types.NamespacedName{Namespace: TestNamespaceName, Name: "das0-monitoring-test-resource-2"}
				extraDash0MonitoringResourceNames = append(extraDash0MonitoringResourceNames, secondName)
				CreateDash0MonitoringResource(ctx, k8sClient, secondName)
				time.Sleep(10 * time.Millisecond)
				thirdName := types.NamespacedName{Namespace: TestNamespaceName, Name: "das0-monitoring-test-resource-3"}
				extraDash0MonitoringResourceNames = append(extraDash0MonitoringResourceNames, thirdName)
				CreateDash0MonitoringResource(ctx, k8sClient, thirdName)

				triggerReconcileRequestForName(ctx, reconciler, "", Dash0MonitoringResourceQualifiedName)
				triggerReconcileRequestForName(ctx, reconciler, "", secondName)
				triggerReconcileRequestForName(ctx, reconciler, "", thirdName)

				Eventually(func(g Gomega) {
					resource1Available := loadCondition(ctx, Dash0MonitoringResourceQualifiedName, dash0v1alpha1.ConditionTypeAvailable)
					resource1Degraded := loadCondition(ctx, Dash0MonitoringResourceQualifiedName, dash0v1alpha1.ConditionTypeDegraded)
					resource2Available := loadCondition(ctx, secondName, dash0v1alpha1.ConditionTypeAvailable)
					resource2Degraded := loadCondition(ctx, secondName, dash0v1alpha1.ConditionTypeDegraded)
					resource3Available := loadCondition(ctx, thirdName, dash0v1alpha1.ConditionTypeAvailable)
					resource3Degraded := loadCondition(ctx, thirdName, dash0v1alpha1.ConditionTypeDegraded)

					// The first two resource should have been marked as degraded.
					verifyCondition(
						g,
						resource1Available,
						metav1.ConditionFalse,
						"NewerResourceIsPresent",
						"There is a more recently created Dash0 monitoring resource in this namespace, please remove all "+
							"but one resource instance.",
					)
					verifyCondition(
						g,
						resource1Degraded,
						metav1.ConditionTrue,
						"NewerResourceIsPresent",
						"There is a more recently created Dash0 monitoring resource in this namespace, please remove all "+
							"but one resource instance.",
					)
					verifyCondition(g, resource2Available, metav1.ConditionFalse, "NewerResourceIsPresent",
						"There is a more recently created Dash0 monitoring resource in this namespace, please remove all "+
							"but one resource instance.")
					verifyCondition(g, resource2Degraded, metav1.ConditionTrue, "NewerResourceIsPresent",
						"There is a more recently created Dash0 monitoring resource in this namespace, please remove all "+
							"but one resource instance.")

					// The third (and most recent) resource should have been marked as available.
					verifyCondition(
						g,
						resource3Available,
						metav1.ConditionTrue,
						"ReconcileFinished",
						"Dash0 is active in this namespace now.",
					)
					g.Expect(resource3Degraded).To(BeNil())

				}, timeout, pollingInterval).Should(Succeed())
			})
		})

		DescribeTable("when instrumenting existing workloads", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			triggerReconcileRequest(ctx, reconciler, "")

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

		Describe("when instrumenting existing workloads (special cases)", func() {
			It("should record a failure event when attempting to instrument an existing job and add labels", func() {
				name := UniqueName(JobNamePrefix)
				By("Inititalize a job")
				job := CreateBasicJob(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyDash0MonitoringResourceIsAvailable(ctx)
				VerifyFailedInstrumentationEvent(
					ctx,
					clientset,
					namespace,
					name,
					fmt.Sprintf("Dash0 instrumentation of this workload by the controller has not been successful. Error message: "+
						"Dash0 cannot instrument the existing job test-namespace/%s, since this type of workload "+
						"is immutable.", name),
				)
				VerifyImmutableJobCouldNotBeModified(GetJob(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing ownerless pod", func() {
				name := UniqueName(PodNamePrefix)
				By("Inititalize a pod")
				pod := CreateBasicPod(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				triggerReconcileRequest(ctx, reconciler, "")

				// We do not instrument existing pods via the controller, since they cannot be restarted.
				// We only instrument new pods via the webhook.
				verifyDash0MonitoringResourceIsAvailable(ctx)
				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing pod owned by a replicaset", func() {
				name := UniqueName(PodNamePrefix)
				By("Inititalize a pod")
				pod := CreatePodOwnedByReplicaSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyDash0MonitoringResourceIsAvailable(ctx)
				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing replicaset owned by a deployment", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				By("Inititalize a replicaset")
				replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyDash0MonitoringResourceIsAvailable(ctx)
				VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
			})
		})

		DescribeTable("when existing workloads have the opt-out label", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, workload.Get())

			triggerReconcileRequest(ctx, reconciler, "")

			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyFn(config.GetFn(ctx, k8sClient, namespace, name))
		}, Entry("should not instrument an existing cron job with the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateCronJobWithOptOutLabel),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyCronJobWithOptOutLabel(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should not instrument an existing daemon set with the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateDaemonSetWithOptOutLabel),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyDaemonSetWithOptOutLabel(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should not instrument an existing deployment with the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateDeploymentWithOptOutLabel),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyDeploymentWithOptOutLabel(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should not instrument an existing ownerless replicaset with the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateReplicaSetWithOptOutLabel),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyReplicaSetWithOptOutLabel(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should not instrument an existing stateful set with the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateStatefulSetWithOptOutLabel),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyStatefulSetWithOptOutLabel(workload.Get().(*appsv1.StatefulSet))
			},
		}))

		Describe("when existing jobs have the opt-out label", func() {
			It("should not touch an existing job with the opt-out label", func() {
				name := UniqueName(JobNamePrefix)
				By("Inititalize a job")
				job := CreateJobWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyJobWithOptOutLabel(GetJob(ctx, k8sClient, namespace, name))
			})
		})

		DescribeTable("when the opt-out label is added to an already instrumented workload", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())
			AddOptOutLabel(workload.GetObjectMeta())
			UpdateWorkload(ctx, k8sClient, workload.Get())
			triggerReconcileRequest(ctx, reconciler, "")
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
			VerifySuccessfulUninstrumentationEvent(ctx, clientset, TestNamespaceName, name, "controller")
		}, Entry("should remove Dash0 from an instrumented cron job when dash0.com/enable=false is added", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateInstrumentedCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyCronJobWithOptOutLabel(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should remove Dash0 from an instrumented daemon set when dash0.com/enable=false is added", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateInstrumentedDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyDaemonSetWithOptOutLabel(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should remove Dash0 from an instrumented deployment when dash0.com/enable=false is added", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateInstrumentedDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyDeploymentWithOptOutLabel(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should remove Dash0 from an instrumented replica set when dash0.com/enable=false is added", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateInstrumentedReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyReplicaSetWithOptOutLabel(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should remove Dash0 from an instrumented stateful set when dash0.com/enable=false is added", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateInstrumentedStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyStatefulSetWithOptOutLabel(workload.Get().(*appsv1.StatefulSet))
			},
		}),
		)

		Describe("when the opt-out label is added to an already instrumented job", func() {
			It("should report the failure to remove Dash0 from an instrumented job when dash0.com/enable=false is added", func() {
				name := UniqueName(JobNamePrefix)
				workload := CreateInstrumentedJob(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddOptOutLabel(&workload.ObjectMeta)
				UpdateWorkload(ctx, k8sClient, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyModifiedJobAfterUnsuccessfulOptOut(GetJob(ctx, k8sClient, TestNamespaceName, name))
				VerifyFailedUninstrumentationEvent(
					ctx,
					clientset,
					TestNamespaceName,
					name,
					fmt.Sprintf("The controller's attempt to remove the Dash0 instrumentation from this workload has not "+
						"been successful. Error message: Dash0 cannot remove the instrumentation from the existing job "+
						"test-namespace/%s, since this type of workload is immutable.", name),
				)
			})

			It("should remove labels from from a job with a previously failed instrumentation attempt when dash0.com/enable=false is added", func() {
				name := UniqueName(JobNamePrefix)
				workload := CreateJobForWhichAnInstrumentationAttemptHasFailed(
					ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddOptOutLabel(&workload.ObjectMeta)
				UpdateWorkload(ctx, k8sClient, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyJobWithOptOutLabel(GetJob(ctx, k8sClient, TestNamespaceName, name))
				VerifyNoUninstrumentationNecessaryEvent(
					ctx,
					clientset,
					TestNamespaceName,
					name,
					"Dash0 instrumentation was not present on this workload, no modification by the controller has been necessary.",
				)
			})
		})

		DescribeTable("when a workload is already instrumented by the same version", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())
			triggerReconcileRequest(ctx, reconciler, "")
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
			VerifyNoEvents(ctx, clientset, TestNamespaceName)
		}, Entry("should not touch a successfully instrumented cron job", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateInstrumentedCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should not touch a successfully instrumented daemon set", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateInstrumentedDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should not touch a successfully instrumented deployment", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateInstrumentedDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should not touch a successfully instrumented job", WorkloadTestConfig{
			WorkloadNamePrefix: JobNamePrefix,
			CreateFn:           WrapJobFnAsTestableWorkload(CreateInstrumentedJob),
			GetFn:              WrapJobFnAsTestableWorkload(GetJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedJob(workload.Get().(*batchv1.Job), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should not touch a successfully instrumented replica set", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateInstrumentedReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should not touch a successfully instrumented stateful set", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateInstrumentedStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
			},
		}),
		)

		DescribeTable("should instrument existing workloads at startup", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, workload.Get())

			reconciler.InstrumentAtStartup()

			VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
			config.VerifyFn(config.GetFn(ctx, k8sClient, namespace, name))
		}, Entry("should instrument a cron job at startup", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument a daemon set at startup", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument a deployment at startup", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument a replica set at startup", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should instrument a stateful set at startup", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
			},
		}),
		)

		Describe("should not instrument existing jobs at startup", func() {
			It("should record a failure event when attempting to instrument an existing job at startup and add labels", func() {
				name := UniqueName(JobNamePrefix)
				job := CreateBasicJob(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				reconciler.InstrumentAtStartup()

				VerifyFailedInstrumentationEvent(
					ctx,
					clientset,
					namespace,
					name,
					fmt.Sprintf("Dash0 instrumentation of this workload by the controller has not been successful. "+
						"Error message: Dash0 cannot instrument the existing job test-namespace/%s, since this type "+
						"of workload is immutable.", name),
				)
				VerifyImmutableJobCouldNotBeModified(GetJob(ctx, k8sClient, namespace, name))
			})
		})

		DescribeTable("when updating instrumented workloads at startup", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())
			workload.GetObjectMeta().Labels["dash0.com/operator-image"] = olderOperatorControllerImageLabel
			workload.GetObjectMeta().Labels["dash0.com/init-container-image"] = olderInitContainerImageLabel
			UpdateWorkload(ctx, k8sClient, workload.Get())
			reconciler.InstrumentAtStartup()
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
			VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
		}, Entry("should override outdated instrumentation settings for a cron job at startup", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateInstrumentedCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should override outdated instrumentation settings for a daemon set at startup", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateInstrumentedDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should override outdated instrumentation settings for a deployment at startup", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateInstrumentedDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should override outdated instrumentation settings for a replica set at startup", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateInstrumentedReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
			},
		}), Entry("should override outdated instrumentation settings for a stateful set at startup", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateInstrumentedStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
			},
		}),
		)

		Describe("when attempting to update instrumented jobs at startup", func() {
			It("should not override outdated instrumentation settings for a job at startup", func() {
				name := UniqueName(JobNamePrefix)
				workload := CreateInstrumentedJob(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload.ObjectMeta.Labels["dash0.com/operator-image"] = "some-registry.com_1234_dash0hq_operator-controller_0.9.8"
				workload.ObjectMeta.Labels["dash0.com/init-container-image"] = "some-registry.com_1234_dash0hq_instrumentation_2.3.4"
				UpdateWorkload(ctx, k8sClient, workload)
				reconciler.InstrumentAtStartup()

				// we do not attempt to update the instrumentation for jobs, since they are immutable
				workload = GetJob(ctx, k8sClient, TestNamespaceName, name)
				jobLabels := workload.ObjectMeta.Labels
				Expect(jobLabels["dash0.com/instrumented"]).To(Equal("true"))
				Expect(jobLabels["dash0.com/operator-image"]).To(Equal("some-registry.com_1234_dash0hq_operator-controller_0.9.8"))
				Expect(jobLabels["dash0.com/init-container-image"]).To(Equal("some-registry.com_1234_dash0hq_instrumentation_2.3.4"))
				VerifyNoEvents(ctx, clientset, namespace)
			})
		})

		DescribeTable("when deleting the Dash0 monitoring resource and reverting the instrumentation on cleanup", func(config WorkloadTestConfig) {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 monitoring
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			By("deleting the Dash0 monitoring resource")
			dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, Default)
			Expect(k8sClient.Delete(ctx, dash0MonitoringResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "trigger a reconcile request to revert the instrumented workload")

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifyWebhookIgnoreOnceLabelIsPresent(workload.GetObjectMeta())
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

		Describe("when deleting the Dash0 monitoring resource and reverting the instrumentation on cleanup (special cases)", func() {
			It("should record a failure event when attempting to revert an existing instrumenting job (which has been instrumented by the webhook)", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 monitoring
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(JobNamePrefix)
				By("Create an instrumented job")
				job := CreateInstrumentedJob(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				By("Deleting the Dash0 monitoring resource")
				dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0MonitoringResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to attempt to revert the instrumented job")

				VerifyFailedUninstrumentationEvent(
					ctx,
					clientset,
					namespace,
					name,
					fmt.Sprintf("The controller's attempt to remove the Dash0 instrumentation from this workload has not "+
						"been successful. Error message: Dash0 cannot remove the instrumentation from the existing job "+
						"test-namespace/%s, since this type of workload is immutable.", name),
				)
				VerifyModifiedJob(GetJob(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should remove instrumentation labels from an existing job for which an instrumentation attempt has failed", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 monitoring
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(JobNamePrefix)
				By("Create a job with label dash0.com/instrumented=false")
				job := CreateJobForWhichAnInstrumentationAttemptHasFailed(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				By("Deleting the Dash0 monitoring resource")
				dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0MonitoringResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to attempt to revert the instrumented job")

				VerifyNoUninstrumentationNecessaryEvent(ctx, clientset, namespace, name, "Dash0 instrumentation was not present on this workload, no modification by the controller has been necessary.")
				VerifyUnmodifiedJob(GetJob(ctx, k8sClient, namespace, name))
			})

			It("should not revert an instrumented ownerless pod", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 monitoring
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(PodNamePrefix)
				By("Create an instrumented pod")
				pod := CreateInstrumentedPod(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				By("Deleting the Dash0 monitoring resource")
				dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0MonitoringResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyModifiedPod(GetPod(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should leave existing uninstrumented pod owned by a replica set alone", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 monitoring
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(PodNamePrefix)
				By("Create an instrumented pod owned by a deployment")
				pod := CreatePodOwnedByReplicaSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				By("Deleting the Dash0 monitoring resource")
				dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0MonitoringResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should leave existing uninstrumented replica sets owned by deployment alone", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 monitoring
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(ReplicaSetNamePrefix)
				By("Create an instrumented replica set owned by a deployment")
				replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				By("Deleting the Dash0 monitoring resource")
				dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0MonitoringResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
			})
		})

		DescribeTable("when deleting the Dash0 monitoring resource and attempting to revert the instrumentation on cleanup but the resource has an opt-out label", func(config WorkloadTestConfig) {
			// We trigger one reconcile request before creating any workload and before deleting the Dash0 monitoring
			// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
			// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
			// happens in production.
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			By("Deleting the Dash0 monitoring resource")
			dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, Default)
			Expect(k8sClient.Delete(ctx, dash0MonitoringResource)).To(Succeed())

			triggerReconcileRequest(ctx, reconciler, "")

			VerifyNoEvents(ctx, clientset, namespace)
			workload = config.GetFn(ctx, k8sClient, namespace, name)
			config.VerifyFn(workload)
			VerifyWebhookIgnoreOnceLabelIsAbesent(workload.GetObjectMeta())

		}, Entry("should not attempt to revert a cron job that has the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateCronJobWithOptOutLabel),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyCronJobWithOptOutLabel(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should not attempt to revert a daemon set that has the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateDaemonSetWithOptOutLabel),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyDaemonSetWithOptOutLabel(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should not attempt to revert a deployment that has the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateDeploymentWithOptOutLabel),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyDeploymentWithOptOutLabel(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should not attempt to revert a job that has the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: JobNamePrefix,
			CreateFn:           WrapJobFnAsTestableWorkload(CreateJobWithOptOutLabel),
			GetFn:              WrapJobFnAsTestableWorkload(GetJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyJobWithOptOutLabel(workload.Get().(*batchv1.Job))
			},
		}), Entry("should not attempt to revert an ownerless replica set that has the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateReplicaSetWithOptOutLabel),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyReplicaSetWithOptOutLabel(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should not attempt to revert a stateful set that has the opt-out label", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateStatefulSetWithOptOutLabel),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyStatefulSetWithOptOutLabel(workload.Get().(*appsv1.StatefulSet))
			},
		}))
	})

	Describe("when the instrumentWorkloads setting changes on an existing Dash0 monitoring resource", Ordered, func() {
		AfterEach(func() {
			RemoveDash0MonitoringResource(ctx, k8sClient)
		})

		DescribeTable("when switching from instrumentWorkloads=none to instrumentWorkloads=created-and-updated", func(config WorkloadTestConfig) {
			EnsureDash0MonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.None)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			triggerReconcileRequest(ctx, reconciler, "")
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.CreatedAndUpdated)

			// Switching from instrumentWorkloads=none to instrumentWorkloads=created-and-updated has no effect.
			// Existing workloads are still not to be instrumented. The new setting only becomes effective when the next
			// resource is created or updated, and the webhook will take care of that.

			triggerReconcileRequest(ctx, reconciler, "")
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
			EnsureDash0MonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.None)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			triggerReconcileRequest(ctx, reconciler, "")
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyPreFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.All)

			triggerReconcileRequest(ctx, reconciler, "")
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
			EnsureDash0MonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.CreatedAndUpdated)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			triggerReconcileRequest(ctx, reconciler, "")
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyPreFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			DeleteAllEvents(ctx, clientset, namespace)

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.None)

			triggerReconcileRequest(ctx, reconciler, "")
			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifyWebhookIgnoreOnceLabelIsPresent(workload.GetObjectMeta())
		}, Entry("should remove instrumentation from an existing cron job after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateInstrumentedCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should remove instrumentation from an existing daemon set after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateInstrumentedDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should remove instrumentation from an existing deployment after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateInstrumentedDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should remove instrumentation from an existing ownerless replicaset after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateInstrumentedReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should remove instrumentation from an existing stateful set after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateInstrumentedStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
		}),
		)

		DescribeTable("when instrumenting existing workloads after switching from instrumentWorkloads=created-and-updated to instrumentWorkloads=all", func(config WorkloadTestConfig) {
			EnsureDash0MonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.CreatedAndUpdated)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			triggerReconcileRequest(ctx, reconciler, "")
			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyPreFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.All)

			triggerReconcileRequest(ctx, reconciler, "")
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
			EnsureDash0MonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.All)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			triggerReconcileRequest(ctx, reconciler, "")
			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			config.VerifyPreFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			DeleteAllEvents(ctx, clientset, namespace)

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.None)

			triggerReconcileRequest(ctx, reconciler, "")
			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifyWebhookIgnoreOnceLabelIsPresent(workload.GetObjectMeta())
		}, Entry("should remove instrumentation from an existing cron job after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should remove instrumentation from an existing daemon set after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should remove instrumentation from an existing deployment after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should remove instrumentation from an existing ownerless replicaset after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should remove instrumentation from an existing stateful set after switching from instrumentWorkloads=all to instrumentWorkloads=none", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyPreFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations())
			},
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
		}),
		)

		DescribeTable("when switching from instrumentWorkloads=all to instrumentWorkloads=created-and-updated", func(config WorkloadTestConfig) {
			EnsureDash0MonitoringResourceExistsWithInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.All)

			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			triggerReconcileRequest(ctx, reconciler, "")
			verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))

			DeleteAllEvents(ctx, clientset, namespace)

			UpdateInstrumentWorkloadsMode(ctx, k8sClient, dash0v1alpha1.CreatedAndUpdated)

			// Switching from instrumentWorkloads=all to instrumentWorkloads=created-and-updated has no effect.
			// Already instrumented workloads will not be uninstrumented.

			triggerReconcileRequest(ctx, reconciler, "")
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
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(ctx, reconciler, createdObjects)
		})
	})

	Describe("when the Dash0 monitoring resource exists and has InstrumentWorkloads=all set explicitly", Ordered, func() {
		BeforeAll(func() {
			dash0MonitoringResource := EnsureDash0MonitoringResourceExists(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads = dash0v1alpha1.All
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			RemoveDash0MonitoringResource(ctx, k8sClient)
		})

		It("should instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsInstrumented(ctx, reconciler, createdObjects)
		})
	})

	Describe("when the Dash0 monitoring resource exists and has an invalid InstrumentWorkloads setting", Ordered, func() {
		It("should not allow creating the resource with an invalid value", func() {
			By("creating the Dash0 monitoring resource")
			Expect(k8sClient.Create(ctx, &dash0v1alpha1.Dash0Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name:      Dash0MonitoringResourceQualifiedName.Name,
					Namespace: Dash0MonitoringResourceQualifiedName.Namespace,
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					IngressEndpoint:     IngressEndpointTest,
					AuthorizationToken:  AuthorizationTokenTest,
					SecretRef:           SecretRefTest,
					InstrumentWorkloads: "invalid",
				},
			})).ToNot(Succeed())
		})

		It("should not allow to update the resource with an invalid value", func() {
			dash0MonitoringResource := EnsureDash0MonitoringResourceExists(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads = "invalid"
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).ToNot(Succeed())
		})
	})

	Describe("when the Dash0 monitoring resource exists but has InstrumentWorkloads=none set", Ordered, func() {
		BeforeAll(func() {
			dash0MonitoringResource := EnsureDash0MonitoringResourceExists(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads = dash0v1alpha1.None
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			RemoveDash0MonitoringResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(ctx, reconciler, createdObjects)
		})
	})

	Describe("when the Dash0 monitoring resource exists but has InstrumentWorkloads=created-and-updated set", Ordered, func() {
		BeforeAll(func() {
			dash0MonitoringResource := EnsureDash0MonitoringResourceExists(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads = dash0v1alpha1.CreatedAndUpdated
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			RemoveDash0MonitoringResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(ctx, reconciler, createdObjects)
		})
	})

	Describe("when deleting the Dash0 monitoring resource and removing the collector resources", func() {
		BeforeEach(func() {
			EnsureDash0MonitoringResourceExists(ctx, k8sClient)
		})

		AfterEach(func() {
			RemoveDash0MonitoringResource(ctx, k8sClient)
		})

		It("should remove the collector resources", func() {
			triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")
			VerifyCollectorResourcesExist(ctx, k8sClient, operatorNamespace)

			dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, Default)
			Expect(k8sClient.Delete(ctx, dash0MonitoringResource)).To(Succeed())
			triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to trigger removing the collector resources")

			VerifyCollectorResourcesDoNotExist(ctx, k8sClient, operatorNamespace)
		})
	})
})

func verifyThatDeploymentIsInstrumented(ctx context.Context, reconciler *Dash0Reconciler, createdObjects []client.Object) []client.Object {
	name := UniqueName(DeploymentNamePrefix)
	By("Inititalize a deployment")
	deployment := CreateBasicDeployment(ctx, k8sClient, namespace, name)
	createdObjects = append(createdObjects, deployment)

	triggerReconcileRequest(ctx, reconciler, "")

	verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
	VerifyModifiedDeployment(GetDeployment(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())

	return createdObjects
}

func verifyThatDeploymentIsNotBeingInstrumented(ctx context.Context, reconciler *Dash0Reconciler, createdObjects []client.Object) []client.Object {
	name := UniqueName(DeploymentNamePrefix)
	By("Inititalize a deployment")
	deployment := CreateDeploymentWithOptOutLabel(ctx, k8sClient, namespace, name)
	createdObjects = append(createdObjects, deployment)

	triggerReconcileRequest(ctx, reconciler, "")

	VerifyNoEvents(ctx, clientset, namespace)
	VerifyDeploymentWithOptOutLabel(GetDeployment(ctx, k8sClient, namespace, name))

	return createdObjects
}

func triggerReconcileRequest(ctx context.Context, reconciler *Dash0Reconciler, stepMessage string) {
	triggerReconcileRequestForName(ctx, reconciler, stepMessage, Dash0MonitoringResourceQualifiedName)
}

func triggerReconcileRequestForName(
	ctx context.Context,
	reconciler *Dash0Reconciler,
	stepMessage string,
	dash0MonitoringResourceName types.NamespacedName,
) {
	if stepMessage == "" {
		stepMessage = "Trigger reconcile request"
	}
	By(stepMessage)
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: dash0MonitoringResourceName,
	})
	Expect(err).NotTo(HaveOccurred())
}

func verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx context.Context, namespace string, name string) {
	verifyDash0MonitoringResourceIsAvailable(ctx)
	VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
}

func verifyDash0MonitoringResourceIsAvailable(ctx context.Context) *metav1.Condition {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		dash0MonitoringResource := LoadDash0MonitoringResourceOrFail(ctx, k8sClient, g)
		availableCondition = meta.FindStatusCondition(dash0MonitoringResource.Status.Conditions, string(dash0v1alpha1.ConditionTypeAvailable))
		g.Expect(availableCondition).NotTo(BeNil())
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
		degraded := meta.FindStatusCondition(dash0MonitoringResource.Status.Conditions, string(dash0v1alpha1.ConditionTypeDegraded))
		g.Expect(degraded).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
	return availableCondition
}

func loadCondition(ctx context.Context, dash0MonitoringResourceName types.NamespacedName, conditionType dash0v1alpha1.ConditionType) *metav1.Condition {
	dash0MonitoringResource := LoadDash0MonitoringResourceByNameOrFail(ctx, k8sClient, Default, dash0MonitoringResourceName)
	return meta.FindStatusCondition(dash0MonitoringResource.Status.Conditions, string(conditionType))
}

func verifyCondition(
	g Gomega,
	condition *metav1.Condition,
	expectedStatus metav1.ConditionStatus,
	expectedReason string,
	expectedMessage string,
) {
	g.Expect(condition).NotTo(BeNil())
	g.Expect(condition.Status).To(Equal(expectedStatus))
	g.Expect(condition.Reason).To(Equal(expectedReason))
	g.Expect(condition.Message).To(Equal(expectedMessage))
}
