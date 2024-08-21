// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	olderOperatorControllerImageLabel = "some-registry_com_1234_dash0hq_operator-controller_0.9.8"
	olderInitContainerImageLabel      = "some-registry_com_1234_dash0hq_instrumentation_2.3.4"
)

var (
	namespace = TestNamespaceName
)

var _ = Describe("The instrumenter", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	var createdObjects []client.Object

	var instrumenter *Instrumenter
	var dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		dash0MonitoringResource = EnsureDash0MonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

		createdObjects = make([]client.Object, 0)

		instrumenter = &Instrumenter{
			Client:               k8sClient,
			Clientset:            clientset,
			Recorder:             recorder,
			Images:               TestImages,
			OTelCollectorBaseUrl: OTelCollectorBaseUrlTest,
		}
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		DeleteAllEvents(ctx, clientset, namespace)

		RemoveDash0MonitoringResource(ctx, k8sClient)
		dash0MonitoringResource = nil
	})

	Describe("when the controller reconciles", func() {
		DescribeTable("when instrumenting existing workloads", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)

			VerifySuccessfulInstrumentationEvent(ctx, clientset, namespace, name, "controller")
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

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)

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

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)

				// We do not instrument existing pods via the controller, since they cannot be restarted.
				// We only instrument new pods via the webhook.
				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing pod owned by a replicaset", func() {
				name := UniqueName(PodNamePrefix)
				By("Inititalize a pod")
				pod := CreatePodOwnedByReplicaSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should not instrument an existing replicaset owned by a deployment", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				By("Inititalize a replicaset")
				replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				//expect(Instrumenter.CheckSettingsAndInstrumentExistingWorkloads(ctx, dash0MonitoringResource, &logger); err != nil {

				VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
			})
		})

		DescribeTable("when existing workloads have the opt-out label", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, namespace, name)
			createdObjects = append(createdObjects, workload.Get())

			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)

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

				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)

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
			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)
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
				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)
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
				checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)
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
			checkSettingsAndInstrumentExistingWorkloads(ctx, instrumenter, dash0MonitoringResource, &logger)
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

		DescribeTable("when uninstrumenting workloads when the Dash0 monitoring resource is deleted", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, &logger)

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

		Describe("when uninstrumenting workloads when the Dash0 monitoring resource is deleted (special cases)", func() {
			It("should record a failure event when attempting to revert an existing instrumenting job (which has been instrumented by the webhook)", func() {
				name := UniqueName(JobNamePrefix)
				By("Create an instrumented job")
				job := CreateInstrumentedJob(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, &logger)

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
				name := UniqueName(JobNamePrefix)
				By("Create a job with label dash0.com/instrumented=false")
				job := CreateJobForWhichAnInstrumentationAttemptHasFailed(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, job)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, &logger)

				VerifyNoUninstrumentationNecessaryEvent(ctx, clientset, namespace, name, "Dash0 instrumentation was not present on this workload, no modification by the controller has been necessary.")
				VerifyUnmodifiedJob(GetJob(ctx, k8sClient, namespace, name))
			})

			It("should not revert an instrumented ownerless pod", func() {
				name := UniqueName(PodNamePrefix)
				By("Create an instrumented pod")
				pod := CreateInstrumentedPod(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, &logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyModifiedPod(GetPod(ctx, k8sClient, namespace, name), BasicInstrumentedPodSpecExpectations())
			})

			It("should leave existing uninstrumented pod owned by a replica set alone", func() {
				name := UniqueName(PodNamePrefix)
				By("Create an instrumented pod owned by a deployment")
				pod := CreatePodOwnedByReplicaSet(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, pod)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, &logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedPod(GetPod(ctx, k8sClient, namespace, name))
			})

			It("should leave existing uninstrumented replica sets owned by deployment alone", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				By("Create an instrumented replica set owned by a deployment")
				replicaSet := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, namespace, name)
				createdObjects = append(createdObjects, replicaSet)

				uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, &logger)

				VerifyNoEvents(ctx, clientset, namespace)
				VerifyUnmodifiedReplicaSet(GetReplicaSet(ctx, k8sClient, namespace, name))
			})
		})

		DescribeTable("when attempting to revert the instrumentation on cleanup but the resource has an opt-out label", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			uninstrumentWorkloadsIfAvailable(ctx, instrumenter, dash0MonitoringResource, &logger)

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

	DescribeTable("should instrument existing uninstrumented workloads at startup", func(config WorkloadTestConfig) {
		name := UniqueName(config.WorkloadNamePrefix)
		workload := config.CreateFn(ctx, k8sClient, namespace, name)
		createdObjects = append(createdObjects, workload.Get())

		instrumenter.InstrumentAtStartup(ctx, k8sClient, &logger)

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

			instrumenter.InstrumentAtStartup(ctx, k8sClient, &logger)

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

		instrumenter.InstrumentAtStartup(ctx, k8sClient, &logger)

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
			instrumenter.InstrumentAtStartup(ctx, k8sClient, &logger)

			// we do not attempt to update the instrumentation for jobs, since they are immutable
			workload = GetJob(ctx, k8sClient, TestNamespaceName, name)
			jobLabels := workload.ObjectMeta.Labels
			Expect(jobLabels["dash0.com/instrumented"]).To(Equal("true"))
			Expect(jobLabels["dash0.com/operator-image"]).To(Equal("some-registry.com_1234_dash0hq_operator-controller_0.9.8"))
			Expect(jobLabels["dash0.com/init-container-image"]).To(Equal("some-registry.com_1234_dash0hq_instrumentation_2.3.4"))
			VerifyNoEvents(ctx, clientset, namespace)
		})
	})
})

func checkSettingsAndInstrumentExistingWorkloads(
	ctx context.Context,
	instrumenter *Instrumenter,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
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
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) {
	Expect(
		instrumenter.UninstrumentWorkloadsIfAvailable(
			ctx,
			dash0MonitoringResource,
			logger,
		)).To(Succeed())
}
