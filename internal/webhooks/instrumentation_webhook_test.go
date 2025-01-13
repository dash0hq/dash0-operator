// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// Maintenance note: There is some overlap of test cases between this file and workloads/modify_test.go. This is
// intentional. However, this test should be used to verify external effects (recording events etc.) that cannot be
// covered in modify_test.go, while more fine-grained test cases and variations should rather be added to
// workloads/modify_test.go.

var _ = Describe("The Dash0 instrumentation webhook", func() {
	var createdObjects []client.Object

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		DeleteAllEvents(ctx, clientset, TestNamespaceName)
	})

	Describe("when the Dash0 monitoring resource exists and is available", Ordered, func() {
		BeforeAll(func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		DescribeTable("when mutating new workloads", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())
			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
		}, Entry("should instrument a new basic cron job", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateBasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should instrument a new basic daemon set", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateBasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should instrument a new basic deployment", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateBasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should instrument a new basic job", WorkloadTestConfig{
			WorkloadNamePrefix: JobNamePrefix,
			CreateFn:           WrapJobFnAsTestableWorkload(CreateBasicJob),
			GetFn:              WrapJobFnAsTestableWorkload(GetJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedJob(workload.Get().(*batchv1.Job), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should instrument a new basic ownerless pod", WorkloadTestConfig{
			WorkloadNamePrefix: PodNamePrefix,
			CreateFn:           WrapPodFnAsTestableWorkload(CreateBasicPod),
			GetFn:              WrapPodFnAsTestableWorkload(GetPod),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedPod(workload.Get().(*corev1.Pod), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should instrument a new basic ownerless replica set", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateBasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should instrument a new basic stateful set", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateBasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}))

		Describe("when new workloads are deployed that are owned by higher order workloads ", Ordered, func() {
			It("should not instrument a new pod owned by a replica set", func() {
				name := UniqueName(PodNamePrefix)
				workload := CreatePodOwnedByReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetPod(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedPod(workload)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a new replica set owned by a deployment", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedReplicaSet(workload)
				VerifyNoInstrumentationNecessaryEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})
		})

		DescribeTable("when an uninstrumented workload has opted out of instrumentation", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())
			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifyNoEvents(ctx, clientset, TestNamespaceName)
		}, Entry("should not instrument a cron job that has opted out of instrumentation", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateCronJobWithOptOutLabel),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyCronJobWithOptOutLabel(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should not instrument a daemonset that has opted out of instrumentation", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateDaemonSetWithOptOutLabel),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyDaemonSetWithOptOutLabel(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should not instrument a deployment that has opted out of instrumentation", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateDeploymentWithOptOutLabel),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyDeploymentWithOptOutLabel(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should not instrument a job that has opted out of instrumentation", WorkloadTestConfig{
			WorkloadNamePrefix: JobNamePrefix,
			CreateFn:           WrapJobFnAsTestableWorkload(CreateJobWithOptOutLabel),
			GetFn:              WrapJobFnAsTestableWorkload(GetJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyJobWithOptOutLabel(workload.Get().(*batchv1.Job))
			},
		}), Entry("should not instrument an ownerless pod that has opted out of instrumentation", WorkloadTestConfig{
			WorkloadNamePrefix: PodNamePrefix,
			CreateFn:           WrapPodFnAsTestableWorkload(CreatePodWithOptOutLabel),
			GetFn:              WrapPodFnAsTestableWorkload(GetPod),
			VerifyFn: func(workload TestableWorkload) {
				VerifyPodWithOptOutLabel(workload.Get().(*corev1.Pod))
			},
		}), Entry("should not instrument an ownerless replica set that has opted out of instrumentation", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateReplicaSetWithOptOutLabel),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyReplicaSetWithOptOutLabel(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should not instrument a stateful set that has opted out of instrumentation", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateStatefulSetWithOptOutLabel),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyStatefulSetWithOptOutLabel(workload.Get().(*appsv1.StatefulSet))
			},
		}))

		Describe("when mutating new workloads with multiple containers and volumes", func() {
			It("should instrument a new deployment that has multiple containers, and already has volumes and init containers", func() {
				name := UniqueName(DeploymentNamePrefix)
				workload := DeploymentWithMoreBellsAndWhistles(TestNamespaceName, name)
				Expect(k8sClient.Create(ctx, workload)).Should(Succeed())
				createdObjects = append(createdObjects, workload)

				workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedDeployment(workload, PodSpecExpectations{
					Volumes:               3,
					Dash0VolumeIdx:        2,
					InitContainers:        3,
					Dash0InitContainerIdx: 2,
					Containers: []ContainerExpectations{
						{
							VolumeMounts:                                2,
							Dash0VolumeMountIdx:                         1,
							EnvVars:                                     9,
							LdPreloadEnvVarIdx:                          1,
							Dash0NodeIpIdx:                              2,
							Dash0CollectorBaseUrlEnvVarIdx:              3,
							Dash0CollectorBaseUrlEnvVarExpectedValue:    OTelCollectorBaseUrlTest,
							OtelExporterOtlpEndpointEnvVarIdx:           4,
							OtelExporterOtlpEndpointEnvVarExpectedValue: OTelCollectorBaseUrlTest,
							Dash0NamespaceNameEnvVarIdx:                 5,
							Dash0PodNameEnvVarIdx:                       6,
							Dash0PodUidEnvVarIdx:                        7,
							Dash0ContainerNameEnvVarIdx:                 8,
							Dash0ContainerNameEnvVarExpectedValue:       "test-container-0",
						},
						{
							VolumeMounts:                                3,
							Dash0VolumeMountIdx:                         2,
							EnvVars:                                     10,
							LdPreloadEnvVarIdx:                          2,
							Dash0NodeIpIdx:                              3,
							Dash0CollectorBaseUrlEnvVarIdx:              4,
							Dash0CollectorBaseUrlEnvVarExpectedValue:    OTelCollectorBaseUrlTest,
							OtelExporterOtlpEndpointEnvVarIdx:           5,
							OtelExporterOtlpEndpointEnvVarExpectedValue: OTelCollectorBaseUrlTest,
							Dash0NamespaceNameEnvVarIdx:                 6,
							Dash0PodNameEnvVarIdx:                       7,
							Dash0PodUidEnvVarIdx:                        8,
							Dash0ContainerNameEnvVarIdx:                 9,
							Dash0ContainerNameEnvVarExpectedValue:       "test-container-1",
						},
					},
				},
					VerifyNoManagedFields,
				)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})
		})

		Describe("when updating instrumentation modifications on new workloads", func() {
			It("should update existing Dash0 artifacts in a new deployment", func() {
				name := UniqueName(DeploymentNamePrefix)
				workload := DeploymentWithExistingDash0Artifacts(TestNamespaceName, name)
				Expect(k8sClient.Create(ctx, workload)).Should(Succeed())
				createdObjects = append(createdObjects, workload)

				workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedDeployment(workload, PodSpecExpectations{
					Volumes:               3,
					Dash0VolumeIdx:        1,
					InitContainers:        3,
					Dash0InitContainerIdx: 1,
					Containers: []ContainerExpectations{
						{
							VolumeMounts:                                2,
							Dash0VolumeMountIdx:                         1,
							EnvVars:                                     9,
							LdPreloadEnvVarIdx:                          1,
							LdPreloadUsesValueFrom:                      true,
							Dash0NodeIpIdx:                              2,
							Dash0CollectorBaseUrlEnvVarIdx:              3,
							Dash0CollectorBaseUrlEnvVarExpectedValue:    OTelCollectorBaseUrlTest,
							OtelExporterOtlpEndpointEnvVarIdx:           4,
							OtelExporterOtlpEndpointEnvVarExpectedValue: OTelCollectorBaseUrlTest,
							Dash0NamespaceNameEnvVarIdx:                 5,
							Dash0PodNameEnvVarIdx:                       6,
							Dash0PodUidEnvVarIdx:                        7,
							Dash0ContainerNameEnvVarIdx:                 8,
							Dash0ContainerNameEnvVarExpectedValue:       "test-container-0",
						},
						{
							VolumeMounts:                                3,
							Dash0VolumeMountIdx:                         1,
							EnvVars:                                     9,
							LdPreloadEnvVarIdx:                          2,
							LdPreloadValue:                              "/__dash0__/dash0_injector.so third_party_preload.so another_third_party_preload.so",
							Dash0NodeIpIdx:                              3,
							Dash0CollectorBaseUrlEnvVarIdx:              0,
							Dash0CollectorBaseUrlEnvVarExpectedValue:    OTelCollectorBaseUrlTest,
							OtelExporterOtlpEndpointEnvVarIdx:           1,
							OtelExporterOtlpEndpointEnvVarExpectedValue: OTelCollectorBaseUrlTest,
							Dash0NamespaceNameEnvVarIdx:                 5,
							Dash0PodNameEnvVarIdx:                       6,
							Dash0PodUidEnvVarIdx:                        7,
							Dash0ContainerNameEnvVarIdx:                 8,
							Dash0ContainerNameEnvVarExpectedValue:       "test-container-1",
						},
					},
				},
					VerifyNoManagedFields,
				)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})
		})

		DescribeTable("when the opt-out label is added to an already instrumented workload", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			DeleteAllEvents(ctx, clientset, TestNamespaceName)
			AddOptOutLabel(workload.GetObjectMeta())
			UpdateWorkload(ctx, k8sClient, workload.Get())

			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifySuccessfulUninstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
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

			// The test cases for jobs and pods are deliberately missing here, since we do not listen to UPDATE requests
			// for those two immutable workload types.

		}), Entry("should remove Dash0 from an instrumented ownerless replica set when dash0.com/enable=false is added", WorkloadTestConfig{
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
		}))

		DescribeTable("when the opt-out label is removed from a workload that had previously opted out", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			DeleteAllEvents(ctx, clientset, TestNamespaceName)
			RemoveOptOutLabel(workload.GetObjectMeta())
			UpdateWorkload(ctx, k8sClient, workload.Get())

			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
		}, Entry("should add Dash0 to an uninstrumented cron job when dash0.com/enable=false is removed", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateCronJobWithOptOutLabel),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should add Dash0 to an uninstrumented daemon set when dash0.com/enable=false is removed", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateDaemonSetWithOptOutLabel),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should add Dash0 to an uninstrumented deployment when dash0.com/enable=false is removed", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateDeploymentWithOptOutLabel),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},

			// The test cases for jobs and pods are deliberately missing here, since we do not listen to UPDATE requests
			// for those two immutable workload types.

		}), Entry("should add Dash0 to an uninstrumented replica set when dash0.com/enable=false is removed", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateReplicaSetWithOptOutLabel),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should add Dash0 to an uninstrumented stateful set when dash0.com/enable=false is removed", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateStatefulSetWithOptOutLabel),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}))

		DescribeTable("when dash0.com/enabled=true is set on a workload that had previously opted out", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())

			DeleteAllEvents(ctx, clientset, TestNamespaceName)
			UpdateLabel(workload.GetObjectMeta(), "dash0.com/enable", "true")
			UpdateWorkload(ctx, k8sClient, workload.Get())

			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
		}, Entry("should add Dash0 to an uninstrumented cron job when dash0.com/enable flips from false to true", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			CreateFn:           WrapCronJobFnAsTestableWorkload(CreateCronJobWithOptOutLabel),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedCronJob(workload.Get().(*batchv1.CronJob), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should add Dash0 to an uninstrumented daemon set when dash0.com/enable flips from false to true", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			CreateFn:           WrapDaemonSetFnAsTestableWorkload(CreateDaemonSetWithOptOutLabel),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDaemonSet(workload.Get().(*appsv1.DaemonSet), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should add Dash0 to an uninstrumented deployment when dash0.com/enable flips from false to true", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			CreateFn:           WrapDeploymentFnAsTestableWorkload(CreateDeploymentWithOptOutLabel),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedDeployment(workload.Get().(*appsv1.Deployment), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},

			// The test cases for jobs and pods are deliberately missing here, since we do not listen to UPDATE requests
			// for those two immutable workload types.

		}), Entry("should add Dash0 to an uninstrumented replica set when dash0.com/enable flips from false to true", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn:           WrapReplicaSetFnAsTestableWorkload(CreateReplicaSetWithOptOutLabel),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}), Entry("should add Dash0 to an uninstrumented stateful set when dash0.com/enable flips from false to true", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			CreateFn:           WrapStatefulSetFnAsTestableWorkload(CreateStatefulSetWithOptOutLabel),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyModifiedStatefulSet(workload.Get().(*appsv1.StatefulSet), BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
			},
		}))

		DescribeTable("when seeing the webhook-ignore-once label", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.ConfigureFn(TestNamespaceName, name)
			createdObjects = append(createdObjects, workload.Get())
			AddLabel(workload.GetObjectMeta(), "dash0.com/webhook-ignore-once", "true")
			CreateWorkload(ctx, k8sClient, workload.Get())

			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifyWebhookIgnoreOnceLabelIsAbsent(workload.GetObjectMeta())
			VerifyNoEvents(ctx, clientset, TestNamespaceName)
		}, Entry("should not instrument a cron job that has the label, but remove the label", WorkloadTestConfig{
			WorkloadNamePrefix: CronJobNamePrefix,
			ConfigureFn:        WrapConfigureCronJobFnAsTestableWorkload(BasicCronJob),
			GetFn:              WrapCronJobFnAsTestableWorkload(GetCronJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedCronJob(workload.Get().(*batchv1.CronJob))
			},
		}), Entry("should not instrument a daemonset that has the label, but remove the label", WorkloadTestConfig{
			WorkloadNamePrefix: DaemonSetNamePrefix,
			ConfigureFn:        WrapConfigureDaemonSetFnAsTestableWorkload(BasicDaemonSet),
			GetFn:              WrapDaemonSetFnAsTestableWorkload(GetDaemonSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDaemonSet(workload.Get().(*appsv1.DaemonSet))
			},
		}), Entry("should not instrument a deployment that has the label, but remove the label", WorkloadTestConfig{
			WorkloadNamePrefix: DeploymentNamePrefix,
			ConfigureFn:        WrapConfigureDeploymentFnAsTestableWorkload(BasicDeployment),
			GetFn:              WrapDeploymentFnAsTestableWorkload(GetDeployment),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedDeployment(workload.Get().(*appsv1.Deployment))
			},
		}), Entry("should not instrument a job that has the label, but remove the label", WorkloadTestConfig{
			WorkloadNamePrefix: JobNamePrefix,
			ConfigureFn:        WrapConfigureJobFnAsTestableWorkload(BasicJob),
			GetFn:              WrapJobFnAsTestableWorkload(GetJob),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedJob(workload.Get().(*batchv1.Job))
			},
		}), Entry("should not instrument an ownerless pod that has the label, but remove the label", WorkloadTestConfig{
			WorkloadNamePrefix: PodNamePrefix,
			ConfigureFn:        WrapConfigurePodFnAsTestableWorkload(BasicPod),
			GetFn:              WrapPodFnAsTestableWorkload(GetPod),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedPod(workload.Get().(*corev1.Pod))
			},
		}), Entry("should not instrument an ownerless replica set that has the label, but remove the label", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			ConfigureFn:        WrapConfigureReplicaSetFnAsTestableWorkload(BasicReplicaSet),
			GetFn:              WrapReplicaSetFnAsTestableWorkload(GetReplicaSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedReplicaSet(workload.Get().(*appsv1.ReplicaSet))
			},
		}), Entry("should not instrument a stateful set that has the label, but remove the label", WorkloadTestConfig{
			WorkloadNamePrefix: StatefulSetNamePrefix,
			ConfigureFn:        WrapConfigureStatefulSetFnAsTestableWorkload(BasicStatefulSet),
			GetFn:              WrapStatefulSetFnAsTestableWorkload(GetStatefulSet),
			VerifyFn: func(workload TestableWorkload) {
				VerifyUnmodifiedStatefulSet(workload.Get().(*appsv1.StatefulSet))
			},
		}))
	})

	Describe("when the Dash0 monitoring resource does not exist", func() {
		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(createdObjects)
		})
	})

	Describe("when the Dash0 monitoring resource exists but is not available", Ordered, func() {
		BeforeAll(func() {
			EnsureMonitoringResourceExistsAndIsDegraded(ctx, k8sClient)
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(createdObjects)
		})
	})

	Describe("when the Dash0 monitoring resource exists and is available and has InstrumentWorkloads=all set explicitly", Ordered, func() {
		BeforeAll(func() {
			dash0MonitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads = dash0v1alpha1.All
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsInstrumented(createdObjects)
		})
	})

	Describe("when the Dash0 monitoring resource exists and is available but has InstrumentWorkloads=none set", Ordered, func() {
		BeforeAll(func() {
			dash0MonitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads = dash0v1alpha1.None
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(createdObjects)
		})
	})

	Describe("when the Dash0 monitoring resource exists and is available and has InstrumentWorkloads=created-and-updated set", Ordered, func() {
		BeforeAll(func() {
			dash0MonitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads = dash0v1alpha1.CreatedAndUpdated
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsInstrumented(createdObjects)
		})
	})
})

func verifyThatDeploymentIsInstrumented(createdObjects []client.Object) []client.Object {
	name := UniqueName(DeploymentNamePrefix)
	workload := CreateBasicDeployment(ctx, k8sClient, TestNamespaceName, name)
	createdObjects = append(createdObjects, workload)
	workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
	VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
	VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
	return createdObjects
}

func verifyThatDeploymentIsNotBeingInstrumented(createdObjects []client.Object) []client.Object {
	name := UniqueName(DeploymentNamePrefix)
	workload := CreateBasicDeployment(ctx, k8sClient, TestNamespaceName, name)
	createdObjects = append(createdObjects, workload)
	workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
	VerifyUnmodifiedDeployment(workload)
	VerifyNoEvents(ctx, clientset, TestNamespaceName)
	return createdObjects
}
