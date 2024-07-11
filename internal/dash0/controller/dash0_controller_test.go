// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"

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

	images = util.Images{
		OperatorImage:                "some-registry.com:1234/dash0hq/operator-controller:1.2.3",
		InitContainerImage:           "some-registry.com:1234/dash0hq/instrumentation:4.5.6",
		InitContainerImagePullPolicy: corev1.PullAlways,
	}

	extraDash0CustomResourceNames = []types.NamespacedName{}
)

var _ = Describe("The Dash0 controller", Ordered, func() {

	ctx := context.Background()
	var createdObjects []client.Object

	var reconciler *Dash0Reconciler

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
		reconciler = &Dash0Reconciler{
			Client:               k8sClient,
			ClientSet:            clientset,
			Recorder:             recorder,
			Scheme:               k8sClient.Scheme(),
			Images:               images,
			OtelCollectorBaseUrl: "http://dash0-operator-opentelemetry-collector.dash0-system.svc.cluster.local:4318",
		}
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		DeleteAllEvents(ctx, clientset, namespace)
	})

	Describe("when the Dash0 resource exists", Ordered, func() {
		BeforeEach(func() {
			EnsureDash0CustomResourceExists(ctx, k8sClient)
		})

		AfterEach(func() {
			RemoveDash0CustomResource(ctx, k8sClient)
			for _, name := range extraDash0CustomResourceNames {
				RemoveDash0CustomResourceByName(ctx, k8sClient, name, true)
			}
		})

		Describe("when reconciling", func() {
			It("should successfully run the first reconcile (no modifiable workloads exist)", func() {
				By("Trigger reconcile request")
				triggerReconcileRequest(ctx, reconciler, "")
				verifyDash0CustomResourceIsAvailable(ctx)
			})

			It("should successfully run multiple reconciles (no modifiable workloads exist)", func() {
				triggerReconcileRequest(ctx, reconciler, "First reconcile request")

				firstAvailableStatusCondition := verifyDash0CustomResourceIsAvailable(ctx)
				originalTransitionTimestamp := firstAvailableStatusCondition.LastTransitionTime.Time

				time.Sleep(50 * time.Millisecond)

				triggerReconcileRequest(ctx, reconciler, "Second reconcile request")

				// The LastTransitionTime should not change with subsequent reconciliations.
				secondAvailableCondition := verifyDash0CustomResourceIsAvailable(ctx)
				Expect(secondAvailableCondition.LastTransitionTime.Time).To(Equal(originalTransitionTimestamp))
			})

			It("should mark only the most recent resource as available and the other ones as degraded when multiple "+
				"resources exist", func() {
				firstDash0CustomResource := &dash0v1alpha1.Dash0{}
				Expect(k8sClient.Get(ctx, Dash0CustomResourceQualifiedName, firstDash0CustomResource)).To(Succeed())
				time.Sleep(10 * time.Millisecond)
				secondName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-test-resource-2"}
				extraDash0CustomResourceNames = append(extraDash0CustomResourceNames, secondName)
				CreateDash0CustomResource(ctx, k8sClient, secondName)
				time.Sleep(10 * time.Millisecond)
				thirdName := types.NamespacedName{Namespace: TestNamespaceName, Name: "dash0-test-resource-3"}
				extraDash0CustomResourceNames = append(extraDash0CustomResourceNames, thirdName)
				CreateDash0CustomResource(ctx, k8sClient, thirdName)

				triggerReconcileRequestForName(ctx, reconciler, "", Dash0CustomResourceQualifiedName)
				triggerReconcileRequestForName(ctx, reconciler, "", secondName)
				triggerReconcileRequestForName(ctx, reconciler, "", thirdName)

				Eventually(func(g Gomega) {
					resource1Available := loadCondition(ctx, Dash0CustomResourceQualifiedName, util.ConditionTypeAvailable)
					resource1Degraded := loadCondition(ctx, Dash0CustomResourceQualifiedName, util.ConditionTypeDegraded)
					resource2Available := loadCondition(ctx, secondName, util.ConditionTypeAvailable)
					resource2Degraded := loadCondition(ctx, secondName, util.ConditionTypeDegraded)
					resource3Available := loadCondition(ctx, thirdName, util.ConditionTypeAvailable)
					resource3Degraded := loadCondition(ctx, thirdName, util.ConditionTypeDegraded)

					// The first two resource should have been marked as degraded.
					verifyCondition(
						g,
						resource1Available,
						metav1.ConditionFalse,
						"NewerResourceIsPresent",
						"There is a more recently created Dash0 custom resource in this namespace, please remove all "+
							"but one resource instance.",
					)
					verifyCondition(
						g,
						resource1Degraded,
						metav1.ConditionTrue,
						"NewerResourceIsPresent",
						"There is a more recently created Dash0 custom resource in this namespace, please remove all "+
							"but one resource instance.",
					)
					verifyCondition(g, resource2Available, metav1.ConditionFalse, "NewerResourceIsPresent",
						"There is a more recently created Dash0 custom resource in this namespace, please remove all "+
							"but one resource instance.")
					verifyCondition(g, resource2Degraded, metav1.ConditionTrue, "NewerResourceIsPresent",
						"There is a more recently created Dash0 custom resource in this namespace, please remove all "+
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

		Describe("when instrumenting existing workloads", func() {
			It("should instrument an existing cron job", func() {
				name := UniqueName(CronJobNamePrefix)
				By("Inititalize a cron job")
				cronJob := CreateBasicCronJob(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, cronJob)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
				VerifyModifiedCronJob(GetCronJob(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should instrument an existing daemon set", func() {
				name := UniqueName(DaemonSetNamePrefix)
				By("Inititalize a daemon set")
				daemonSet := CreateBasicDaemonSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, daemonSet)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
				VerifyModifiedDaemonSet(GetDaemonSet(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should instrument an existing deployment", func() {
				createdObjects = verifyDeploymentIsBeingInstrumented(ctx, reconciler, createdObjects)
			})

			It("should record a failure event when attempting to instrument an existing job and add labels", func() {
				name := UniqueName(JobNamePrefix)
				By("Inititalize a job")
				job := CreateBasicJob(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyDash0CustomResourceIsAvailable(ctx)
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
				verifyDash0CustomResourceIsAvailable(ctx)
				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing pod owned by a replicaset", func() {
				name := UniqueName(PodNamePrefix)
				By("Inititalize a pod")
				pod := CreatePodOwnedByReplicaSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyDash0CustomResourceIsAvailable(ctx)
				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should instrument an existing ownerless replicaset", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				By("Inititalize a replicaset")
				replicaSet := CreateBasicReplicaSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
				VerifyModifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should not instrument an existing replicaset owned by a deployment", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				By("Inititalize a replicaset")
				replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyDash0CustomResourceIsAvailable(ctx)
				VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
			})

			It("should instrument an existing stateful set", func() {
				name := UniqueName(StatefulSetNamePrefix)
				By("Inititalize a stateful set")
				statefulSet := CreateBasicStatefulSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, statefulSet)

				triggerReconcileRequest(ctx, reconciler, "")

				verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx, namespace, name)
				VerifyModifiedStatefulSet(GetStatefulSet(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})
		})

		Describe("when existing workloads have the opt-out label", func() {
			It("should not instrument an existing cron job with the opt-out label", func() {
				name := UniqueName(CronJobNamePrefix)
				By("Inititalize a cron job")
				job := CreateCronJobWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyCronJobWithOptOutLabel(GetCronJob(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing daemon set with the opt-out label", func() {
				name := UniqueName(DaemonSetNamePrefix)
				By("Inititalize a daemon set")
				daemonSet := CreateDaemonSetWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, daemonSet)

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyDaemonSetWithOptOutLabel(GetDaemonSet(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing deployment with the opt-out label", func() {
				createdObjects = verifyThatDeploymentIsNotBeingInstrumented(ctx, reconciler, createdObjects)
			})

			It("should not touch an existing job with the opt-out label", func() {
				name := UniqueName(JobNamePrefix)
				By("Inititalize a job")
				job := CreateJobWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyJobWithOptOutLabel(GetJob(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing ownerless replicaset with the opt-out label", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				By("Inititalize a replicaset")
				replicaSet := CreateReplicaSetWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyReplicaSetWithOptOutLabel(GetReplicaSet(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an stateful set with the opt-out label", func() {
				name := UniqueName(StatefulSetNamePrefix)
				By("Inititalize a stateful set")
				statefulSet := CreateStatefulSetWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, statefulSet)

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyStatefulSetWithOptOutLabel(GetStatefulSet(ctx, k8sClient, namespace, name))
			})
		})

		Describe("when the opt-out label is added to an already instrumented workload", func() {
			It("should remove Dash0 from an instrumented cron job when dash0.com/enable=false is added", func() {
				name := UniqueName(CronJobNamePrefix)
				workload := CreateInstrumentedCronJob(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddOptOutLabel(&workload.ObjectMeta)
				UpdateWorkload(ctx, k8sClient, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyCronJobWithOptOutLabel(GetCronJob(ctx, k8sClient, TestNamespaceName, name))
				VerifySuccessfulUninstrumentationEvent(ctx, clientset, TestNamespaceName, name, "controller")
			})

			It("should remove Dash0 from an instrumented daemon set when dash0.com/enable=false is added", func() {
				name := UniqueName(DaemonSetNamePrefix)
				workload := CreateInstrumentedDaemonSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddOptOutLabel(&workload.ObjectMeta)
				UpdateWorkload(ctx, k8sClient, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyDaemonSetWithOptOutLabel(GetDaemonSet(ctx, k8sClient, TestNamespaceName, name))
				VerifySuccessfulUninstrumentationEvent(ctx, clientset, TestNamespaceName, name, "controller")
			})

			It("should remove Dash0 from an instrumented deployment when dash0.com/enable=false is added", func() {
				name := UniqueName(DeploymentNamePrefix)
				workload := CreateInstrumentedDeployment(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddOptOutLabel(&workload.ObjectMeta)
				UpdateWorkload(ctx, k8sClient, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyDeploymentWithOptOutLabel(GetDeployment(ctx, k8sClient, TestNamespaceName, name))
				VerifySuccessfulUninstrumentationEvent(ctx, clientset, TestNamespaceName, name, "controller")
			})

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

			It("should remove Dash0 from an instrumented replica set when dash0.com/enable=false is added", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := CreateInstrumentedReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddOptOutLabel(&workload.ObjectMeta)
				UpdateWorkload(ctx, k8sClient, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyReplicaSetWithOptOutLabel(GetReplicaSet(ctx, k8sClient, TestNamespaceName, name))
				VerifySuccessfulUninstrumentationEvent(ctx, clientset, TestNamespaceName, name, "controller")
			})

			It("should remove Dash0 from an instrumented stateful set when dash0.com/enable=false is added", func() {
				name := UniqueName(StatefulSetNamePrefix)
				workload := CreateInstrumentedStatefulSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddOptOutLabel(&workload.ObjectMeta)
				UpdateWorkload(ctx, k8sClient, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyStatefulSetWithOptOutLabel(GetStatefulSet(ctx, k8sClient, TestNamespaceName, name))
				VerifySuccessfulUninstrumentationEvent(ctx, clientset, TestNamespaceName, name, "controller")
			})
		})

		Describe("when a workload is already instrumented by the same version", func() {
			It("should not touch a successfully instrumented cron job", func() {
				name := UniqueName(CronJobNamePrefix)
				workload := CreateInstrumentedCronJob(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyModifiedCronJob(GetCronJob(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not touch a successfully instrumented daemon set", func() {
				name := UniqueName(DaemonSetNamePrefix)
				workload := CreateInstrumentedDaemonSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyModifiedDaemonSet(GetDaemonSet(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not touch a successfully instrumented deployment", func() {
				name := UniqueName(DeploymentNamePrefix)
				workload := CreateInstrumentedDeployment(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyModifiedDeployment(GetDeployment(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not touch a successfully instrumented job", func() {
				name := UniqueName(JobNamePrefix)
				workload := CreateInstrumentedJob(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyModifiedJob(GetJob(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not touch a successfully instrumented replica set", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := CreateInstrumentedReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyModifiedReplicaSet(GetReplicaSet(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not touch a successfully instrumented stateful set", func() {
				name := UniqueName(StatefulSetNamePrefix)
				workload := CreateInstrumentedStatefulSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				triggerReconcileRequest(ctx, reconciler, "")
				VerifyModifiedStatefulSet(GetStatefulSet(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})
		})

		Describe("should instrument existing workloads at startup", func() {

			It("should instrument cron job at startup", func() {
				name := UniqueName(CronJobNamePrefix)
				workload := CreateBasicCronJob(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, workload)

				reconciler.InstrumentAtStartup()

				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
				VerifyModifiedCronJob(GetCronJob(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should instrument deployment at startup", func() {
				name := UniqueName(DaemonSetNamePrefix)
				workload := CreateBasicDaemonSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, workload)

				reconciler.InstrumentAtStartup()

				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
				VerifyModifiedDaemonSet(GetDaemonSet(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should instrument deployment at startup", func() {
				name := UniqueName(DeploymentNamePrefix)
				workload := CreateBasicDeployment(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, workload)

				reconciler.InstrumentAtStartup()

				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
				VerifyModifiedDeployment(GetDeployment(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

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

			It("should instrument replica set at startup", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := CreateBasicReplicaSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, workload)

				reconciler.InstrumentAtStartup()

				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
				VerifyModifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should instrument stateful set at startup", func() {
				name := UniqueName(StatefulSetNamePrefix)
				workload := CreateBasicStatefulSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, workload)

				reconciler.InstrumentAtStartup()

				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
				VerifyModifiedStatefulSet(GetStatefulSet(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})
		})

		Describe("when updating instrumented workloads at startup", func() {
			It("should override outdated instrumentation settings for a cron job at startup", func() {
				name := UniqueName(CronJobNamePrefix)
				workload := CreateInstrumentedCronJob(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload.ObjectMeta.Labels["dash0.com/operator-image"] = olderOperatorControllerImageLabel
				workload.ObjectMeta.Labels["dash0.com/init-container-image"] = olderInitContainerImageLabel
				UpdateWorkload(ctx, k8sClient, workload)
				reconciler.InstrumentAtStartup()
				VerifyModifiedCronJob(GetCronJob(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
			})

			It("should override outdated instrumentation settings for a daemon set at startup", func() {
				name := UniqueName(DaemonSetNamePrefix)
				workload := CreateInstrumentedDaemonSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload.ObjectMeta.Labels["dash0.com/operator-image"] = olderOperatorControllerImageLabel
				workload.ObjectMeta.Labels["dash0.com/init-container-image"] = olderInitContainerImageLabel
				UpdateWorkload(ctx, k8sClient, workload)
				reconciler.InstrumentAtStartup()
				VerifyModifiedDaemonSet(GetDaemonSet(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
			})

			It("should override outdated instrumentation settings for a deployment at startup", func() {
				name := UniqueName(DeploymentNamePrefix)
				workload := CreateInstrumentedDeployment(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload.ObjectMeta.Labels["dash0.com/operator-image"] = olderOperatorControllerImageLabel
				workload.ObjectMeta.Labels["dash0.com/init-container-image"] = olderInitContainerImageLabel
				UpdateWorkload(ctx, k8sClient, workload)
				reconciler.InstrumentAtStartup()
				VerifyModifiedDeployment(GetDeployment(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
			})

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

			It("should override outdated instrumentation settings for a replica set at startup", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := CreateInstrumentedReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload.ObjectMeta.Labels["dash0.com/operator-image"] = olderOperatorControllerImageLabel
				workload.ObjectMeta.Labels["dash0.com/init-container-image"] = olderInitContainerImageLabel
				UpdateWorkload(ctx, k8sClient, workload)
				reconciler.InstrumentAtStartup()
				VerifyModifiedReplicaSet(GetReplicaSet(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
			})

			It("should override outdated instrumentation settings for a stateful set at startup", func() {
				name := UniqueName(StatefulSetNamePrefix)
				workload := CreateInstrumentedStatefulSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload.ObjectMeta.Labels["dash0.com/operator-image"] = olderOperatorControllerImageLabel
				workload.ObjectMeta.Labels["dash0.com/init-container-image"] = olderInitContainerImageLabel
				UpdateWorkload(ctx, k8sClient, workload)
				reconciler.InstrumentAtStartup()
				VerifyModifiedStatefulSet(GetStatefulSet(ctx, k8sClient, TestNamespaceName, name), BasicInstrumentedPodSpecExpectations())
				VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
			})

		})

		Describe("when reverting the instrumentation on cleanup", func() {
			It("should revert an instrumented cron job", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(CronJobNamePrefix)
				By("Create an instrumented cron job")
				cronJob := CreateInstrumentedCronJob(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, cronJob)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
				cronJob = GetCronJob(ctx, k8sClient, namespace, name)
				VerifyUnmodifiedCronJob(cronJob)
				VerifyWebhookIgnoreOnceLabelIsPresent(&cronJob.ObjectMeta)
			})

			It("should revert an instrumented daemon set", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(DaemonSetNamePrefix)
				By("Create an instrumented daemon set")
				daemonSet := CreateInstrumentedDaemonSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, daemonSet)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
				daemonSet = GetDaemonSet(ctx, k8sClient, namespace, name)
				VerifyUnmodifiedDaemonSet(daemonSet)
				VerifyWebhookIgnoreOnceLabelIsPresent(&daemonSet.ObjectMeta)
			})

			It("should revert an instrumented deployment", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(DeploymentNamePrefix)
				By("Create an instrumented deployment")
				deployment := CreateInstrumentedDeployment(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, deployment)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
				deployment = GetDeployment(ctx, k8sClient, namespace, name)
				VerifyUnmodifiedDeployment(deployment)
				VerifyWebhookIgnoreOnceLabelIsPresent(&deployment.ObjectMeta)
			})

			It("should record a failure event when attempting to revert an existing instrumenting job (which has been instrumented by the webhook)", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(JobNamePrefix)
				By("Create an instrumented job")
				job := CreateInstrumentedJob(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

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
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(JobNamePrefix)
				By("Create a job with label dash0.com/instrumented=false")
				job := CreateJobForWhichAnInstrumentationAttemptHasFailed(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to attempt to revert the instrumented job")

				VerifyNoUninstrumentationNecessaryEvent(ctx, clientset, namespace, name, "Dash0 instrumentation was not present on this workload, no modification by the controller has been necessary.")
				VerifyUnmodifiedJob(GetJob(ctx, k8sClient, namespace, name))
			})

			It("should not revert an instrumented ownerless pod", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(PodNamePrefix)
				By("Create an instrumented pod")
				pod := CreateInstrumentedPod(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyModifiedPod(GetPod(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should leave existing uninstrumented pod owned by a replica set alone", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(PodNamePrefix)
				By("Create an instrumented pod owned by a deployment")
				pod := CreatePodOwnedByReplicaSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should revert an instrumented ownerless replica set", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(ReplicaSetNamePrefix)
				By("Create an instrumented replica set")
				replicaSet := CreateInstrumentedReplicaSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
				replicaSet = GetReplicaSet(ctx, k8sClient, namespace, name)
				VerifyUnmodifiedReplicaSet(replicaSet)
				VerifyWebhookIgnoreOnceLabelIsPresent(&replicaSet.ObjectMeta)
			})

			It("should leave existing uninstrumented replica sets owned by deployment alone", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(ReplicaSetNamePrefix)
				By("Create an instrumented replica set owned by a deployment")
				replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
			})

			It("should revert an instrumented stateful set", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(StatefulSetNamePrefix)
				By("Create an instrumented stateful set")
				statefulSet := CreateInstrumentedStatefulSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, statefulSet)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to revert the instrumented workload")

				VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, "controller")
				statefulSet = GetStatefulSet(ctx, k8sClient, namespace, name)
				VerifyUnmodifiedStatefulSet(statefulSet)
				VerifyWebhookIgnoreOnceLabelIsPresent(&statefulSet.ObjectMeta)
			})
		})

		Describe("when attempting to revert the instrumentation on cleanup but the resource has an opt-out label", func() {
			It("should not attempt to revert a cron job that has the opt-out label", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(CronJobNamePrefix)
				By("Create a cron job")
				cronJob := CreateCronJobWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, cronJob)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				cronJob = GetCronJob(ctx, k8sClient, namespace, name)
				VerifyCronJobWithOptOutLabel(cronJob)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&cronJob.ObjectMeta)
			})

			It("should not attempt to revert a daemon set that has the opt-out label", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(DaemonSetNamePrefix)
				By("Create a daemon set")
				daemonSet := CreateDaemonSetWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, daemonSet)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				daemonSet = GetDaemonSet(ctx, k8sClient, namespace, name)
				VerifyDaemonSetWithOptOutLabel(daemonSet)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&daemonSet.ObjectMeta)
			})

			It("should not attempt to revert a deployment that has the opt-out label", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(DeploymentNamePrefix)
				By("Create a deployment")
				deployment := CreateDeploymentWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, deployment)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				deployment = GetDeployment(ctx, k8sClient, namespace, name)
				VerifyDeploymentWithOptOutLabel(deployment)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&deployment.ObjectMeta)
			})

			It("should not attempt to revert a job that has the opt-out label", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(JobNamePrefix)
				By("Create a job")
				job := CreateJobWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "Trigger a reconcile request to attempt to revert the instrumented job")

				VerifyNoEvents(ctx, clientset, namespace)
				job = GetJob(ctx, k8sClient, namespace, name)
				VerifyJobWithOptOutLabel(job)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&job.ObjectMeta)
			})

			It("should not attempt to revert an ownerless replica set that has the opt-out label", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(ReplicaSetNamePrefix)
				By("Create a replica set")
				replicaSet := CreateReplicaSetWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				replicaSet = GetReplicaSet(ctx, k8sClient, namespace, name)
				VerifyReplicaSetWithOptOutLabel(replicaSet)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&replicaSet.ObjectMeta)
			})

			It("should not attempt to revert a stateful set that has the opt-out label", func() {
				// We trigger one reconcile request before creating any workload and before deleting the Dash0 custom
				// resource, just to get the `isFirstReconcile` logic out of the way and to add the finalizer.
				// Alternatively, we could just add the finalizer here directly, but this approach is closer to what usually
				// happens in production.
				triggerReconcileRequest(ctx, reconciler, "Trigger first reconcile request")

				name := UniqueName(StatefulSetNamePrefix)
				By("Create a stateful set")
				statefulSet := CreateStatefulSetWithOptOutLabel(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, statefulSet)

				By("Queue the deletion of the Dash0 custom resource")
				dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, dash0CustomResource)).To(Succeed())

				triggerReconcileRequest(ctx, reconciler, "")

				VerifyNoEvents(ctx, clientset, namespace)
				statefulSet = GetStatefulSet(ctx, k8sClient, namespace, name)
				VerifyStatefulSetWithOptOutLabel(statefulSet)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&statefulSet.ObjectMeta)
			})
		})
	})

	Describe("when the Dash0 resource does not exist", func() {
		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(ctx, reconciler, createdObjects)
		})
	})

	Describe("when the Dash0 resource exists but has InstrumentWorkloads=false set", Ordered, func() {
		BeforeAll(func() {
			dash0CustomResource := EnsureDash0CustomResourceExists(ctx, k8sClient)
			dash0CustomResource.Spec.InstrumentWorkloads = &False
			Expect(k8sClient.Update(ctx, dash0CustomResource)).To(Succeed())
		})

		AfterAll(func() {
			RemoveDash0CustomResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(ctx, reconciler, createdObjects)
		})
	})

	Describe("when the Dash0 resource exists but has InstrumentExistingWorkloads=false set", Ordered, func() {
		BeforeAll(func() {
			dash0CustomResource := EnsureDash0CustomResourceExists(ctx, k8sClient)
			dash0CustomResource.Spec.InstrumentExistingWorkloads = &False
			Expect(k8sClient.Update(ctx, dash0CustomResource)).To(Succeed())
		})

		AfterAll(func() {
			RemoveDash0CustomResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(ctx, reconciler, createdObjects)
		})
	})

	Describe("when the Dash0 resource exists and has InstrumentNewWorkloads=false set", Ordered, func() {
		BeforeAll(func() {
			dash0CustomResource := EnsureDash0CustomResourceExists(ctx, k8sClient)
			dash0CustomResource.Spec.InstrumentNewWorkloads = &False
			Expect(k8sClient.Update(ctx, dash0CustomResource)).To(Succeed())
		})

		AfterAll(func() {
			RemoveDash0CustomResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyDeploymentIsBeingInstrumented(ctx, reconciler, createdObjects)
		})
	})
})

func verifyDeploymentIsBeingInstrumented(ctx context.Context, reconciler *Dash0Reconciler, createdObjects []client.Object) []client.Object {
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
	triggerReconcileRequestForName(ctx, reconciler, stepMessage, Dash0CustomResourceQualifiedName)
}

func triggerReconcileRequestForName(
	ctx context.Context,
	reconciler *Dash0Reconciler,
	stepMessage string,
	dash0CustomResourceName types.NamespacedName,
) {
	if stepMessage == "" {
		stepMessage = "Trigger reconcile request"
	}
	By(stepMessage)
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: dash0CustomResourceName,
	})
	Expect(err).NotTo(HaveOccurred())
}

func verifyStatusConditionAndSuccessfulInstrumentationEvent(ctx context.Context, namespace string, name string) {
	verifyDash0CustomResourceIsAvailable(ctx)
	VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
}

func verifyDash0CustomResourceIsAvailable(ctx context.Context) *metav1.Condition {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		dash0CustomResource := LoadDash0CustomResourceOrFail(ctx, k8sClient, g)
		availableCondition = meta.FindStatusCondition(dash0CustomResource.Status.Conditions, string(util.ConditionTypeAvailable))
		g.Expect(availableCondition).NotTo(BeNil())
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
		degraded := meta.FindStatusCondition(dash0CustomResource.Status.Conditions, string(util.ConditionTypeDegraded))
		g.Expect(degraded).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
	return availableCondition
}

func loadCondition(ctx context.Context, dash0CustomResourceName types.NamespacedName, conditionType util.ConditionType) *metav1.Condition {
	dash0CustomResource := LoadDash0CustomResourceByNameOrFail(ctx, k8sClient, Default, dash0CustomResourceName)
	return meta.FindStatusCondition(dash0CustomResource.Status.Conditions, string(conditionType))
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
