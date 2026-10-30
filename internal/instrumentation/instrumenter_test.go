// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	olderOperatorControllerImageLabel = "some-registry_com_1234_dash0hq_operator-controller_0.9.8"
	olderInitContainerImageLabel      = "some-registry_com_1234_dash0hq_instrumentation_2.3.4"
	testActor                         = string(util.ActorController)
)

var (
	namespace = TestNamespaceName
)

var _ = Describe("The instrumenter", Ordered, func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)
	var createdObjectsInstrumenterTest []client.Object

	var instrumenter *Instrumenter
	var dash0MonitoringResource *dash0v1beta1.Dash0Monitoring

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		dash0MonitoringResource = EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

		createdObjectsInstrumenterTest = make([]client.Object, 0)

		instrumenter = NewInstrumenter(
			k8sClient,
			clientset,
			recorder,
			util.NewClusterInstrumentationConfig(
				TestImages,
				PossibleCollectorUrlsTest,
				OTelCollectorNodeLocalBaseUrlTest,
				util.ExtraConfigDefaults,
				dash0v1alpha1.InstrumentationDeliveryInitContainer,
				nil,
				false,
				false,
				false,
			),
		)
	})

	AfterEach(func() {
		createdObjectsInstrumenterTest = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsInstrumenterTest)
		DeleteAllEvents(ctx, clientset, namespace)

		DeleteMonitoringResource(ctx, k8sClient)
		dash0MonitoringResource = nil
	})

	Describe("when the monitoring controller reconciles", func() {
		DescribeTable("when instrumenting existing workloads", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload.Get())

			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

			VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, testActor)
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
		}), Entry("should instrument an existing replicaset owned by an unrecognized type", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn: WrapReplicaSetFnAsTestableWorkload(
				func(ctx context.Context,
					k8sClient client.Client,
					namespace string,
					name string) *appsv1.ReplicaSet {
					rs := CreateBasicReplicaSet(ctx, k8sClient, namespace, name)
					rs.OwnerReferences = []metav1.OwnerReference{{
						Name:       "owner-name",
						APIVersion: "api/v1beta2",
						Kind:       "Kind",
						UID:        "35b829cb-78dc-4544-b7a9-5a8e51b7f322",
					}}
					UpdateWorkload(ctx, k8sClient, rs)
					return rs
				}),
			GetFn: WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
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

		DescribeTable("when pod-level instrumentation is enabled", func(config WorkloadTestConfig) {
			instrumenter.ClusterInstrumentationConfig.InstrumentPods = true
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload.Get())

			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

			VerifyNoEvents(ctx, clientset, namespace)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
		}, Entry("should not modify an existing cron job", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should not modify an existing daemon set", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should not modify an existing deployment", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should not modify an existing job", WorkloadTestConfig{
			WorkloadNamePrefix: JobNamePrefix,
			CreateFn:           WrapJobFnAsTestableWorkload(CreateBasicJob),
			GetFn:              WrapJobFnAsTestableWorkload(GetJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedJob(workload.Get().(*batchv1.Job))
			},
		}), Entry("should not modify an existing ownerless replicaset", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should not modify an existing stateful set", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
		}),
		)

		DescribeTable("when pod-level instrumentation is enabled and pods exist", func(config podEvictionTestConfig) {
			instrumenter.ClusterInstrumentationConfig.InstrumentPods = true
			name := UniqueName(PodNamePrefix)
			pod := CreateWorkload(ctx, k8sClient, config.podFn(name)).(*corev1.Pod)
			if config.phase != "" {
				pod.Status.Phase = config.phase
				Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
			}
			if !config.expectEviction {
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)
			}

			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

			if config.expectEviction {
				verifyPodHasBeenEvicted(ctx, name)
			} else {
				verifyPodHasNotBeenEvicted(ctx, name)
			}
			VerifyNoEvents(ctx, clientset, namespace)
		}, Entry("should evict an uninstrumented pod controlled by a replica set", podEvictionTestConfig{
			podFn:          podControlledByReplicaSet,
			expectEviction: true,
		}), Entry("should evict an uninstrumented pod controlled by a stateful set", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				return PodControlledBy(namespace, name, util.K8sTypeMetaStatefulSet)
			},
			expectEviction: true,
		}), Entry("should evict an uninstrumented pod controlled by a daemon set", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				return PodControlledBy(namespace, name, util.K8sTypeMetaDaemonSet)
			},
			expectEviction: true,
		}), Entry("should evict a pod with outdated instrumentation", podEvictionTestConfig{
			podFn:          outdatedInstrumentedPodControlledByReplicaSet,
			expectEviction: true,
		}), Entry("should not evict a pod with up-to-date instrumentation", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				pod := InstrumentedPod(namespace, name)
				pod.OwnerReferences = podControlledByReplicaSet(name).OwnerReferences
				return pod
			},
		}), Entry("should not evict an ownerless pod", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				return BasicPod(namespace, name)
			},
		}), Entry("should not evict a pod with an owner that is not its controller", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				return PodOwnedByReplicaSet(namespace, name)
			},
		}), Entry("should not evict a pod controlled by a job", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				return PodControlledBy(namespace, name, util.K8sTypeMetaJob)
			},
		}), Entry("should not evict a pod controlled by an unknown kind of owner", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				return PodControlledBy(namespace, name, metav1.TypeMeta{
					APIVersion: "core.strimzi.io/v1beta2",
					Kind:       "StrimziPodSet",
				})
			},
		}), Entry("should not evict a pod with the opt-out label", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				pod := podControlledByReplicaSet(name)
				AddOptOutLabel(&pod.ObjectMeta)
				return pod
			},
		}), Entry("should not evict an instrumented pod with the opt-out label", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				pod := outdatedInstrumentedPodControlledByReplicaSet(name)
				AddOptOutLabel(&pod.ObjectMeta)
				return pod
			},
		}), Entry("should not evict a pod that cannot be instrumented", podEvictionTestConfig{
			podFn: func(name string) *corev1.Pod {
				pod := podControlledByReplicaSet(name)
				pod.Spec.NodeSelector = map[string]string{util.KubernetesIoOs: "windows"}
				return pod
			},
		}), Entry("should not evict a pod that has succeeded", podEvictionTestConfig{
			podFn: podControlledByReplicaSet,
			phase: corev1.PodSucceeded,
		}), Entry("should not evict a pod that has failed", podEvictionTestConfig{
			podFn: podControlledByReplicaSet,
			phase: corev1.PodFailed,
		}),
		)

		Describe("when pod-level instrumentation is enabled (special cases for evicting pods)", func() {
			BeforeEach(func() {
				instrumenter.ClusterInstrumentationConfig.InstrumentPods = true
			})

			It("should not evict a pod that is being deleted", func() {
				name := UniqueName(PodNamePrefix)
				pod := podControlledByReplicaSet(name)
				pod.Finalizers = []string{"dash0.com/test-finalizer"}
				CreateWorkload(ctx, k8sClient, pod)
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
				DeferCleanup(func() {
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())
					pod.Finalizers = nil
					Expect(k8sClient.Update(ctx, pod)).To(Succeed())
				})

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())
				Expect(pod.DeletionTimestamp).ToNot(BeNil())
				Expect(pod.Finalizers).To(ConsistOf("dash0.com/test-finalizer"))
			})

			It("should not evict a pod if a pod disruption budget does not allow it", func() {
				name := UniqueName(PodNamePrefix)
				pod := podControlledByReplicaSet(name)
				AddLabel(&pod.ObjectMeta, "app", name)
				CreateWorkload(ctx, k8sClient, pod)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)
				pod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
				pdb := &policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
					Spec: policyv1.PodDisruptionBudgetSpec{
						MinAvailable: new(intstr.FromInt32(1)),
						Selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
					},
				}
				CreateWorkload(ctx, k8sClient, pdb)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pdb)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				verifyPodHasNotBeenEvicted(ctx, name)
			})

			It("should not evict pods if instrumentWorkloads.mode is created-and-updated", func() {
				name := UniqueName(PodNamePrefix)
				pod := CreateWorkload(ctx, k8sClient, podControlledByReplicaSet(name))
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)
				dash0MonitoringResource.Spec.InstrumentWorkloads.Mode = dash0common.InstrumentWorkloadsModeCreatedAndUpdated

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				verifyPodHasNotBeenEvicted(ctx, name)
			})

			It("should not evict pods in the operator namespace", func() {
				name := UniqueName(PodNamePrefix)
				pod := CreateWorkload(ctx, k8sClient, podControlledByReplicaSet(name))
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)
				instrumenter.ClusterInstrumentationConfig.OperatorNamespace = namespace

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				verifyPodHasNotBeenEvicted(ctx, name)
			})

			It("should not evict pods if the monitoring resource does not become available", func() {
				originalTimeout := waitForMonitoringResourceInCacheTimeout
				waitForMonitoringResourceInCacheTimeout = 300 * time.Millisecond
				DeferCleanup(func() {
					waitForMonitoringResourceInCacheTimeout = originalTimeout
				})
				dash0MonitoringResource.EnsureResourceIsMarkedAsDegraded("TestReason", "This resource is degraded.")
				Expect(k8sClient.Status().Update(ctx, dash0MonitoringResource)).To(Succeed())
				name := UniqueName(PodNamePrefix)
				pod := CreateWorkload(ctx, k8sClient, podControlledByReplicaSet(name))
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				verifyPodHasNotBeenEvicted(ctx, name)
			})
		})

		Describe("when instrumenting existing workloads (special cases)", func() {
			It("should record a failure event when attempting to instrument an existing job and add labels", func() {
				name := UniqueName(JobNamePrefix)
				By("Inititalize a job")
				job := CreateBasicJob(ctx, k8sClient, namespace, name)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, job)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

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
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				// We do not instrument existing pods via the monitoring controller, since they cannot be restarted.
				// We only instrument new pods via the webhook.
				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing pod owned by a replicaset", func() {
				name := UniqueName(PodNamePrefix)
				By("Inititalize a pod")
				pod := CreatePodOwnedByReplicaSet(ctx, k8sClient, namespace, name)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing replicaset owned by a deployment", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				By("Inititalize a replicaset")
				replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, replicaSet)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				VerifyInstrumentationViaHigherOrderWorkloadEvent(ctx, clientset, namespace, name, testActor)
				VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
			})
		})

		DescribeTable("when existing workloads have the opt-out label", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, namespace, name)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload.Get())

			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

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
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, job)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyJobWithOptOutLabel(GetJob(ctx, k8sClient, namespace, name))
			})
		})

		DescribeTable("when the opt-out label is added to an already instrumented workload", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload.Get())
			AddOptOutLabel(workload.GetObjectMeta())
			UpdateWorkload(ctx, k8sClient, workload.Get())
			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
			VerifySuccessfulUninstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
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
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload)
				AddOptOutLabel(&workload.ObjectMeta)
				UpdateWorkload(ctx, k8sClient, workload)
				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)
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
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload)
				AddOptOutLabel(&workload.ObjectMeta)
				UpdateWorkload(ctx, k8sClient, workload)
				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)
				VerifyJobWithOptOutLabel(GetJob(ctx, k8sClient, TestNamespaceName, name))
				VerifySuccessfulUninstrumentationEvent(
					ctx,
					clientset,
					TestNamespaceName,
					name,
					testActor,
				)
			})
		})

		It("when existing workloads opt in to auto-instrumentation via a custom label selector", func() {
			// Create two workloads, one that matches the custom instrumentation label selector and one that does not.
			nameForWorkloadWithoutCustomOptIn := UniqueName(DeploymentNamePrefix)
			workloadWithoutCustomOptIn := CreateBasicDeployment(ctx, k8sClient, namespace, nameForWorkloadWithoutCustomOptIn)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workloadWithoutCustomOptIn)

			nameForWorkloadWithCustomOptIn := UniqueName(DeploymentNamePrefix)
			deploymentWithCustomOptIn := BasicDeployment(namespace, nameForWorkloadWithCustomOptIn)
			deploymentWithCustomOptIn.ObjectMeta.Labels = map[string]string{"dash0-auto-instrument": "yes"}
			workloadWithCustomOptIn :=
				CreateWorkload(ctx, k8sClient, deploymentWithCustomOptIn).(*appsv1.Deployment)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workloadWithCustomOptIn)

			dash0MonitoringResourceWithCustomLabelSelector :=
				&dash0v1beta1.Dash0Monitoring{
					ObjectMeta: MonitoringResourceDefaultObjectMeta,
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
							LabelSelector: "dash0-auto-instrument=yes",
						},
					},
				}

			checkSettingsAndInstrumentExistingWorkloads(
				ctx,
				instrumenter,
				dash0MonitoringResourceWithCustomLabelSelector,
				logger,
			)

			VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, nameForWorkloadWithCustomOptIn, testActor)
			workloadWithCustomOptIn = GetDeployment(ctx, k8sClient, namespace, nameForWorkloadWithCustomOptIn)
			VerifyModifiedDeployment(workloadWithCustomOptIn, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			workloadWithoutCustomOptIn = GetDeployment(ctx, k8sClient, namespace, nameForWorkloadWithoutCustomOptIn)
			VerifyUnmodifiedDeployment(workloadWithoutCustomOptIn)
		})

		It("when a label matching the custom auto-instrumentation label selector is changed", func() {
			name := UniqueName(DeploymentNamePrefix)
			deployment := BasicDeployment(namespace, name)
			deployment.ObjectMeta.Labels = map[string]string{"dash0-auto-instrument": "yes"}
			workload := CreateWorkload(ctx, k8sClient, deployment).(*appsv1.Deployment)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload)

			dash0MonitoringResourceWithCustomLabelSelector :=
				&dash0v1beta1.Dash0Monitoring{
					ObjectMeta: MonitoringResourceDefaultObjectMeta,
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
							LabelSelector: "dash0-auto-instrument=yes",
						},
					},
				}

			checkSettingsAndInstrumentExistingWorkloads(
				ctx,
				instrumenter,
				dash0MonitoringResourceWithCustomLabelSelector,
				logger,
			)

			VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, testActor)
			workload = GetDeployment(ctx, k8sClient, namespace, name)
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)

			DeleteAllEvents(ctx, clientset, namespace)

			workload.Labels["dash0-auto-instrument"] = "no"
			UpdateWorkload(ctx, k8sClient, workload)

			checkSettingsAndInstrumentExistingWorkloads(
				ctx,
				instrumenter,
				dash0MonitoringResourceWithCustomLabelSelector,
				logger,
			)

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, testActor)
			workload = GetDeployment(ctx, k8sClient, namespace, name)
			VerifyUnmodifiedDeployment(workload)
		})

		It("when a label matching the custom auto-instrumentation label selector is removed", func() {
			name := UniqueName(DeploymentNamePrefix)
			deployment := BasicDeployment(namespace, name)
			deployment.ObjectMeta.Labels = map[string]string{"dash0-auto-instrument": "yes"}
			workload := CreateWorkload(ctx, k8sClient, deployment).(*appsv1.Deployment)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload)

			dash0MonitoringResourceWithCustomLabelSelector :=
				&dash0v1beta1.Dash0Monitoring{
					ObjectMeta: MonitoringResourceDefaultObjectMeta,
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
							LabelSelector: "dash0-auto-instrument=yes",
						},
					},
				}

			checkSettingsAndInstrumentExistingWorkloads(
				ctx,
				instrumenter,
				dash0MonitoringResourceWithCustomLabelSelector,
				logger,
			)

			VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, testActor)
			workload = GetDeployment(ctx, k8sClient, namespace, name)
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)

			DeleteAllEvents(ctx, clientset, namespace)

			delete(workload.Labels, "dash0-auto-instrument")
			UpdateWorkload(ctx, k8sClient, workload)

			checkSettingsAndInstrumentExistingWorkloads(
				ctx,
				instrumenter,
				dash0MonitoringResourceWithCustomLabelSelector,
				logger,
			)

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, testActor)
			workload = GetDeployment(ctx, k8sClient, namespace, name)
			VerifyUnmodifiedDeployment(workload)
		})

		It("when the custom auto-instrumentation label selector is changed in the monitoring resource", func() {
			// This case is actually mainly tested in internal/controller/monitoring_controller_test.go,
			// "the auto-instrumentation label selector changes on an existing Dash0 monitoring resource", because it
			// involves MonitoringReconciler#manageInstrumentWorkloadsChanges, which is not covered here in
			// instrumenter_test.go.
			name := UniqueName(DeploymentNamePrefix)
			deployment := BasicDeployment(namespace, name)
			deployment.ObjectMeta.Labels = map[string]string{"dash0-auto-instrument": "yes"}
			workload := CreateWorkload(ctx, k8sClient, deployment).(*appsv1.Deployment)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload)

			dash0MonitoringResourceWithCustomLabelSelector :=
				&dash0v1beta1.Dash0Monitoring{
					ObjectMeta: MonitoringResourceDefaultObjectMeta,
					Spec: dash0v1beta1.Dash0MonitoringSpec{
						InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
							LabelSelector: "dash0-auto-instrument=yes",
						},
					},
				}

			checkSettingsAndInstrumentExistingWorkloads(
				ctx,
				instrumenter,
				dash0MonitoringResourceWithCustomLabelSelector,
				logger,
			)

			VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, testActor)
			workload = GetDeployment(ctx, k8sClient, namespace, name)
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)

			DeleteAllEvents(ctx, clientset, namespace)

			dash0MonitoringResourceWithCustomLabelSelector.Spec.InstrumentWorkloads.LabelSelector =
				"some-other-label=true"

			checkSettingsAndInstrumentExistingWorkloads(
				ctx,
				instrumenter,
				dash0MonitoringResourceWithCustomLabelSelector,
				logger,
			)

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, testActor)
			workload = GetDeployment(ctx, k8sClient, namespace, name)
			VerifyUnmodifiedDeployment(workload)
		})

		DescribeTable("when a workload is already instrumented by the same version", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload.Get())
			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)
			config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
			VerifyNoEvents(ctx, clientset, TestNamespaceName)
		}, Entry("should not touch a successfully instrumented cron job", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateInstrumentedCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
		}), Entry("should not touch a successfully instrumented daemon set", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateInstrumentedDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
		}), Entry("should not touch a successfully instrumented deployment", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateInstrumentedDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
		}), Entry("should not touch a successfully instrumented job", WorkloadTestConfig{
			WorkloadNamePrefix: JobNamePrefix,
			CreateFn:           WrapJobFnAsTestableWorkload(CreateInstrumentedJob),
			GetFn:              WrapJobFnAsTestableWorkload(GetJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedJob(workload.Get().(*batchv1.Job), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
		}), Entry("should not touch a successfully instrumented replica set", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateInstrumentedReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
		}), Entry("should not touch a successfully instrumented stateful set", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateInstrumentedStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			},
		}),
		)

		Describe("when a workload that needs no other update has a leftover webhook-ignore-once label", func() {
			It("should remove the label from a deployment that is already instrumented by the same version", func() {
				name := UniqueName(DeploymentNamePrefix)
				deployment := InstrumentedDeployment(namespace, name)
				AddLabel(&deployment.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, deployment)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, deployment)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				deployment = GetDeployment(ctx, k8sClient, namespace, name)
				VerifyModifiedDeployment(deployment, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
				VerifyWebhookIgnoreOnceLabelIsAbsent(&deployment.ObjectMeta)
				VerifyNoEvents(ctx, clientset, namespace)
			})

			It("should remove the label from a deployment with the opt-out label", func() {
				name := UniqueName(DeploymentNamePrefix)
				deployment := DeploymentWithOptOutLabel(namespace, name)
				AddLabel(&deployment.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, deployment)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, deployment)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				deployment = GetDeployment(ctx, k8sClient, namespace, name)
				VerifyDeploymentWithOptOutLabel(deployment)
				VerifyWebhookIgnoreOnceLabelIsAbsent(&deployment.ObjectMeta)
				VerifyNoEvents(ctx, clientset, namespace)
			})

			It("should remove the label from a deployment that cannot be instrumented", func() {
				name := UniqueName(DeploymentNamePrefix)
				deployment := BasicDeployment(namespace, name)
				deployment.Spec.Template.Spec.OS = &corev1.PodOS{Name: corev1.Windows}
				AddLabel(&deployment.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, deployment)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, deployment)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, logger)

				deployment = GetDeployment(ctx, k8sClient, namespace, name)
				VerifyUnmodifiedDeployment(deployment)
				VerifyWebhookIgnoreOnceLabelIsAbsent(&deployment.ObjectMeta)
			})
		})

		DescribeTable("when uninstrumenting workloads when the Dash0 monitoring resource is deleted", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload.Get())

			uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, logger)

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, testActor)
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
		}), Entry("should revert an instrumented replica set owned by an unrecognized type", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn: WrapReplicaSetFnAsTestableWorkload(
				func(ctx context.Context,
					k8sClient client.Client,
					namespace string,
					name string) *appsv1.ReplicaSet {
					rs := CreateInstrumentedReplicaSet(ctx, k8sClient, namespace, name)
					rs.OwnerReferences = []metav1.OwnerReference{{
						Name:       "owner-name",
						APIVersion: "api/v1beta2",
						Kind:       "Kind",
						UID:        "35b829cb-78dc-4544-b7a9-5a8e51b7f322",
					}}
					UpdateWorkload(ctx, k8sClient, rs)
					return rs
				}),
			GetFn: WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
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

		Describe("when uninstrumenting workloads when the Dash0 monitoring resource is deleted (special cases)", func() {
			It("should record a failure event when attempting to revert an existing instrumenting job (which has been instrumented by the webhook)", func() {
				name := UniqueName(JobNamePrefix)
				By("Create an instrumented job")
				job := CreateInstrumentedJob(ctx, k8sClient, namespace, name)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, job)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, logger)

				VerifyFailedUninstrumentationEvent(
					ctx,
					clientset,
					namespace,
					name,
					fmt.Sprintf("The controller's attempt to remove the Dash0 instrumentation from this workload has not "+
						"been successful. Error message: Dash0 cannot remove the instrumentation from the existing job "+
						"test-namespace/%s, since this type of workload is immutable.", name),
				)
				VerifyModifiedJob(GetJob(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			})

			It("should remove instrumentation labels from an existing job for which an instrumentation attempt has failed", func() {
				name := UniqueName(JobNamePrefix)
				By("Create a job with label dash0.com/instrumented=false")
				job := CreateJobForWhichAnInstrumentationAttemptHasFailed(ctx, k8sClient, namespace, name)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, job)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, logger)

				VerifySuccessfulUninstrumentationEvent(ctx, clientset, namespace, name, testActor)
				VerifyUnmodifiedJob(GetJob(ctx, k8sClient, namespace, name))
			})

			It("should not revert an instrumented ownerless pod", func() {
				name := UniqueName(PodNamePrefix)
				By("Create an instrumented pod")
				pod := CreateInstrumentedPod(ctx, k8sClient, namespace, name)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyModifiedPod(GetPod(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			})

			It("should not revert an instrumented pod owned by an unrecognized type", func() {
				name := UniqueName(PodNamePrefix)
				By("Create an instrumented pod")
				pod := CreateInstrumentedPod(ctx, k8sClient, namespace, name)
				pod.OwnerReferences = []metav1.OwnerReference{{
					Name:       "strimzi-podset-name",
					APIVersion: "core.strimzi.io/v1beta2",
					Kind:       "StrimziPodSet",
					UID:        "35b829cb-78dc-4544-b7a9-5a8e51b7f322",
				}}
				UpdateWorkload(ctx, k8sClient, pod)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyModifiedPod(GetPod(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			})

			It("should leave existing uninstrumented pod owned by a replica set alone", func() {
				name := UniqueName(PodNamePrefix)
				By("Create an instrumented pod owned by a deployment")
				pod := CreatePodOwnedByReplicaSet(ctx, k8sClient, namespace, name)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, pod)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should leave existing uninstrumented replica sets owned by deployment alone", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				By("Create an instrumented replica set owned by a deployment")
				replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
				createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, replicaSet)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
			})
		})

		DescribeTable("when attempting to revert the instrumentation on cleanup but the resource has an opt-out label", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload.Get())

			uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, logger)

			VerifyNoEvents(ctx, clientset, namespace)
			workload = config.GetFn(ctx, k8sClient, namespace, name)
			config.VerifyFn(workload)
			VerifyWebhookIgnoreOnceLabelIsAbsent(workload.GetObjectMeta())

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

		It("enables debug environment variables", func() {
			instrumenterWithDebug := NewInstrumenter(
				k8sClient,
				clientset,
				recorder,
				util.NewClusterInstrumentationConfig(
					TestImages,
					PossibleCollectorUrlsTest,
					OTelCollectorNodeLocalBaseUrlTest,
					util.ExtraConfigDefaults,
					dash0v1alpha1.InstrumentationDeliveryInitContainer,
					nil,
					true,
					false,
					false,
				),
			)

			name := UniqueName(DeploymentNamePrefix)
			workload := CreateBasicDeployment(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload)

			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenterWithDebug, dash0MonitoringResource, logger)

			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
			instrumentedDeployment := GetDeployment(ctx, k8sClient, TestNamespaceName, name)
			podSpec := instrumentedDeployment.Spec.Template.Spec
			initContainerEnv := podSpec.InitContainers[0].Env
			Expect(initContainerEnv).To(ContainElement(MatchEnvVar("DASH0_COPY_INSTRUMENTATION_DEBUG", "true")))
			workloadEnv := podSpec.Containers[0].Env
			Expect(workloadEnv).To(ContainElement(MatchEnvVar("OTEL_INJECTOR_LOG_LEVEL", "debug")))
		})
	})

	It("should evict uninstrumented and outdated pods at startup when pod-level instrumentation is enabled", func() {
		instrumenter.ClusterInstrumentationConfig.InstrumentPods = true
		uninstrumentedPodName := UniqueName(PodNamePrefix)
		CreateWorkload(ctx, k8sClient, podControlledByReplicaSet(uninstrumentedPodName))
		outdatedPodName := UniqueName(PodNamePrefix)
		CreateWorkload(ctx, k8sClient, outdatedInstrumentedPodControlledByReplicaSet(outdatedPodName))
		ownerlessPodName := UniqueName(PodNamePrefix)
		ownerlessPod := CreateBasicPod(ctx, k8sClient, namespace, ownerlessPodName)
		createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, ownerlessPod)

		instrumenter.InstrumentAtStartup(ctx, logger)

		verifyPodHasBeenEvicted(ctx, uninstrumentedPodName)
		verifyPodHasBeenEvicted(ctx, outdatedPodName)
		verifyPodHasNotBeenEvicted(ctx, ownerlessPodName)
	})

	DescribeTable("should instrument existing uninstrumented workloads at startup", func(config WorkloadTestConfig) {
		name := UniqueName(config.WorkloadNamePrefix)
		workload := config.CreateFn(ctx, k8sClient, namespace, name)
		createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload.Get())

		instrumenter.InstrumentAtStartup(ctx, logger)

		VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, testActor)
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
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, job)

			instrumenter.InstrumentAtStartup(ctx, logger)

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
		createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload.Get())
		workload.GetObjectMeta().Labels["dash0.com/operator-image"] = olderOperatorControllerImageLabel
		workload.GetObjectMeta().Labels["dash0.com/instrumentation-image"] = olderInitContainerImageLabel
		UpdateWorkload(ctx, k8sClient, workload.Get())

		instrumenter.InstrumentAtStartup(ctx, logger)

		config.VerifyFn(config.GetFn(ctx, k8sClient, TestNamespaceName, name))
		VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, testActor)
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
			createdObjectsInstrumenterTest = append(createdObjectsInstrumenterTest, workload)
			workload.ObjectMeta.Labels["dash0.com/operator-image"] = "some-registry.com_1234_dash0hq_operator-controller_0.9.8"
			workload.ObjectMeta.Labels["dash0.com/instrumentation-image"] = "some-registry.com_1234_dash0hq_instrumentation_2.3.4"
			UpdateWorkload(ctx, k8sClient, workload)
			instrumenter.InstrumentAtStartup(ctx, logger)

			// we do not attempt to update the instrumentation for jobs, since they are immutable
			workload = GetJob(ctx, k8sClient, TestNamespaceName, name)
			jobLabels := workload.ObjectMeta.Labels
			Expect(jobLabels["dash0.com/instrumented"]).To(Equal("true"))
			Expect(jobLabels["dash0.com/operator-image"]).To(Equal("some-registry.com_1234_dash0hq_operator-controller_0.9.8"))
			Expect(jobLabels["dash0.com/instrumentation-image"]).To(Equal("some-registry.com_1234_dash0hq_instrumentation_2.3.4"))
			VerifyNoEvents(ctx, clientset, namespace)
		})
	})
})

type podEvictionTestConfig struct {
	podFn          func(name string) *corev1.Pod
	phase          corev1.PodPhase
	expectEviction bool
}

func podControlledByReplicaSet(name string) *corev1.Pod {
	return PodControlledBy(namespace, name, util.K8sTypeMetaReplicaSet)
}

func outdatedInstrumentedPodControlledByReplicaSet(name string) *corev1.Pod {
	pod := InstrumentedPod(namespace, name)
	pod.OwnerReferences = podControlledByReplicaSet(name).OwnerReferences
	pod.Labels["dash0.com/operator-image"] = olderOperatorControllerImageLabel
	pod.Labels["dash0.com/instrumentation-image"] = olderInitContainerImageLabel
	pod.Spec.InitContainers[0].Image = "some-registry.com:1234/dash0hq/instrumentation:2.3.4"
	return pod
}

func verifyPodHasBeenEvicted(ctx context.Context, name string) {
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &corev1.Pod{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected pod %s to have been evicted, but got %v", name, err)
}

func verifyPodHasNotBeenEvicted(ctx context.Context, name string) {
	pod := &corev1.Pod{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pod)).To(Succeed())
	Expect(pod.DeletionTimestamp).To(BeNil())
}

func checkSettingsAndInstrumentExistingWorkloads(
	ctx context.Context,
	instrumenter *Instrumenter,
	dash0MonitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) {
	Expect(
		instrumenter.CheckSettingsAndInstrumentExistingWorkloads(
			ctx,
			dash0MonitoringResource,
			logger,
		)).To(Succeed())
}

func uninstrumentWorkloadsIfAvailable(
	ctx context.Context,
	instrumenter *Instrumenter,
	dash0MonitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) {
	Expect(
		instrumenter.UninstrumentWorkloadsIfAvailable(
			ctx,
			dash0MonitoringResource,
			logger,
		)).To(Succeed())
}
