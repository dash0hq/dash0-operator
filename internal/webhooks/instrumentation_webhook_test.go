// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// Maintenance note: There is some overlap of test cases between this file and workloads/modify_test.go. This is
// intentional. However, this test should be used to verify external effects (recording events etc.) that cannot be
// covered in modify_test.go, while more fine-grained test cases and variations should rather be added to
// workloads/modify_test.go.

const (
	testActor = string(util.ActorWebhook)
)

var _ = Describe("The Dash0 instrumentation webhook", func() {
	var createdObjectsInstrumentationWebhookTest []client.Object

	BeforeEach(func() {
		createdObjectsInstrumentationWebhookTest = make([]client.Object, 0)
	})

	AfterEach(func() {
		createdObjectsInstrumentationWebhookTest = DeleteAllCreatedObjects(ctx, k8sClient, createdObjectsInstrumentationWebhookTest)
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
			createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload.Get())
			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
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
		}), Entry("should instrument a new basic pod owned by an unrecognized type", WorkloadTestConfig{
			WorkloadNamePrefix: PodNamePrefix,
			CreateFn: WrapPodFnAsTestableWorkload(func(
				ctx context.Context,
				k8sClient client.Client,
				namespace string,
				name string,
			) *corev1.Pod {
				pod := CreateBasicPod(ctx, k8sClient, namespace, name)
				pod.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
					Name:       "strimzi-podset-name",
					APIVersion: "core.strimzi.io/v1beta2",
					Kind:       "StrimziPodSet",
					UID:        "35b829cb-78dc-4544-b7a9-5a8e51b7f322",
				}}
				UpdateWorkload(ctx, k8sClient, pod)
				return pod
			}),
			GetFn: WrapPodFnAsTestableWorkload(GetPod),
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
		}), Entry("should instrument a new basic replica set owned by an unrecognized type", WorkloadTestConfig{
			WorkloadNamePrefix: ReplicaSetNamePrefix,
			CreateFn: WrapReplicaSetFnAsTestableWorkload(func(ctx context.Context,
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
				createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload)
				workload = GetPod(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedPod(workload)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a new replica set owned by a deployment", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, TestNamespaceName, name)
				createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload)
				workload = GetReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedReplicaSet(workload)
				VerifyInstrumentationViaHigherOrderWorkloadEvent(ctx, clientset, TestNamespaceName, name, testActor)
			})
		})

		DescribeTable("when an uninstrumented workload has opted out of instrumentation", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload.Get())
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
				createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload)

				workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedDeployment(workload, PodSpecExpectations{
					Volumes:               3,
					Dash0VolumeIdx:        2,
					InitContainers:        3,
					Dash0InitContainerIdx: 2,
					Containers: []ContainerExpectations{
						{
							ContainerName:       "test-container-0",
							VolumeMounts:        2,
							Dash0VolumeMountIdx: 1,
							EnvVars: map[string]*EnvVarExpectation{
								"TEST0": {
									Value: "value",
								},
								"LD_PRELOAD": {
									Value: "/__dash0__/dash0_injector.so",
								},
								"DASH0_NODE_IP": {
									ValueFrom: "status.hostIP",
								},
								"DASH0_OTEL_COLLECTOR_BASE_URL": {
									Value: OTelCollectorNodeLocalBaseUrlTest,
								},
								"OTEL_EXPORTER_OTLP_ENDPOINT": {
									Value: OTelCollectorNodeLocalBaseUrlTest,
								},
								"OTEL_EXPORTER_OTLP_PROTOCOL": {
									Value: common.ProtocolHttpProtobuf,
								},
								"DASH0_NAMESPACE_NAME": {
									ValueFrom: "metadata.namespace",
								},
								"DASH0_POD_NAME": {
									ValueFrom: "metadata.name",
								},
								"DASH0_POD_UID": {
									ValueFrom: "metadata.uid",
								},
								"DASH0_CONTAINER_NAME": {
									Value: "test-container-0",
								},
								"DASH0_SERVICE_NAME": {
									ValueFrom: "metadata.labels['app.kubernetes.io/name']",
								},
								"DASH0_SERVICE_NAMESPACE": {
									ValueFrom: "metadata.labels['app.kubernetes.io/part-of']",
								},
								"DASH0_SERVICE_VERSION": {
									ValueFrom: "metadata.labels['app.kubernetes.io/version']",
								},
								"DASH0_RESOURCE_ATTRIBUTES": {
									UnorderedCommaSeparatedValues: []string{
										"workload.only.1=workload-value-1",
										"workload.only.2=workload-value-2",
										"pod.and.workload=pod-value",
										"pod.only.1=pod-value-1",
										"pod.only.2=pod-value-2",
									},
								},
							},
						},
						{
							ContainerName:       "test-container-1",
							VolumeMounts:        3,
							Dash0VolumeMountIdx: 2,
							EnvVars: map[string]*EnvVarExpectation{
								"TEST0": {
									Value: "value",
								},
								"TEST1": {
									ValueFrom: "metadata.namespace",
								},
								"LD_PRELOAD": {
									Value: "/__dash0__/dash0_injector.so",
								},
								"DASH0_NODE_IP": {
									ValueFrom: "status.hostIP",
								},
								"DASH0_OTEL_COLLECTOR_BASE_URL": {
									Value: OTelCollectorNodeLocalBaseUrlTest,
								},
								"OTEL_EXPORTER_OTLP_ENDPOINT": {
									Value: OTelCollectorNodeLocalBaseUrlTest,
								},
								"OTEL_EXPORTER_OTLP_PROTOCOL": {
									Value: common.ProtocolHttpProtobuf,
								},
								"DASH0_NAMESPACE_NAME": {
									ValueFrom: "metadata.namespace",
								},
								"DASH0_POD_NAME": {
									ValueFrom: "metadata.name",
								},
								"DASH0_POD_UID": {
									ValueFrom: "metadata.uid",
								},
								"DASH0_CONTAINER_NAME": {
									Value: "test-container-1",
								},
								"DASH0_SERVICE_NAME": {
									ValueFrom: "metadata.labels['app.kubernetes.io/name']",
								},
								"DASH0_SERVICE_NAMESPACE": {
									ValueFrom: "metadata.labels['app.kubernetes.io/part-of']",
								},
								"DASH0_SERVICE_VERSION": {
									ValueFrom: "metadata.labels['app.kubernetes.io/version']",
								},
								"DASH0_RESOURCE_ATTRIBUTES": {
									UnorderedCommaSeparatedValues: []string{
										"workload.only.1=workload-value-1",
										"workload.only.2=workload-value-2",
										"pod.and.workload=pod-value",
										"pod.only.1=pod-value-1",
										"pod.only.2=pod-value-2",
									},
								},
							},
						},
					},
				},
					VerifyNoManagedFields,
				)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
			})
		})

		Describe("when updating instrumentation modifications on new workloads", func() {
			It("should update existing Dash0 artifacts in a new deployment", func() {
				name := UniqueName(DeploymentNamePrefix)
				workload := DeploymentWithExistingDash0Artifacts(TestNamespaceName, name)
				Expect(k8sClient.Create(ctx, workload)).Should(Succeed())
				createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload)

				workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedDeployment(workload, PodSpecExpectations{
					Volumes:               3,
					Dash0VolumeIdx:        1,
					InitContainers:        3,
					Dash0InitContainerIdx: 1,
					Containers: []ContainerExpectations{
						{
							ContainerName:       "test-container-0",
							VolumeMounts:        2,
							Dash0VolumeMountIdx: 1,
							EnvVars: map[string]*EnvVarExpectation{
								"TEST0": {
									Value: "value",
								},
								"LD_PRELOAD": {
									// The operator does not support injecting into containers that already have LD_PRELOAD set via a
									// ValueFrom clause, thus this env var will not be modified.
									ValueFrom: "metadata.namespace",
								},
								"DASH0_NODE_IP": {
									ValueFrom: "status.hostIP",
								},
								"DASH0_OTEL_COLLECTOR_BASE_URL": {
									Value: OTelCollectorNodeLocalBaseUrlTest,
								},
								"OTEL_EXPORTER_OTLP_ENDPOINT": {
									Value: OTelCollectorNodeLocalBaseUrlTest,
								},
								"OTEL_EXPORTER_OTLP_PROTOCOL": {
									Value: common.ProtocolHttpProtobuf,
								},
								"DASH0_NAMESPACE_NAME": {
									ValueFrom: "metadata.namespace",
								},
								"DASH0_POD_NAME": {
									ValueFrom: "metadata.name",
								},
								"DASH0_POD_UID": {
									ValueFrom: "metadata.uid",
								},
								"DASH0_CONTAINER_NAME": {
									Value: "test-container-0",
								},
							},
						},
						{
							ContainerName:       "test-container-1",
							VolumeMounts:        3,
							Dash0VolumeMountIdx: 1,
							EnvVars: map[string]*EnvVarExpectation{
								"LD_PRELOAD": {
									Value: "/__dash0__/dash0_injector.so third_party_preload.so another_third_party_preload.so",
								},
								"DASH0_NODE_IP": {
									ValueFrom: "status.hostIP",
								},
								"DASH0_OTEL_COLLECTOR_BASE_URL": {
									Value: OTelCollectorNodeLocalBaseUrlTest,
								},
								"OTEL_EXPORTER_OTLP_ENDPOINT": {
									Value: OTelCollectorNodeLocalBaseUrlTest,
								},
								"OTEL_EXPORTER_OTLP_PROTOCOL": {
									Value: common.ProtocolHttpProtobuf,
								},
								"DASH0_NAMESPACE_NAME": {
									ValueFrom: "metadata.namespace",
								},
								"DASH0_POD_NAME": {
									ValueFrom: "metadata.name",
								},
								"DASH0_POD_UID": {
									ValueFrom: "metadata.uid",
								},
								"DASH0_CONTAINER_NAME": {
									Value: "test-container-1",
								},
								"TEST4": {
									Value: "value",
								},
							},
						},
					},
				},
					VerifyNoManagedFields,
				)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
			})
		})

		DescribeTable("when the opt-out label is added to an already instrumented workload", func(config WorkloadTestConfig) {
			name := UniqueName(config.WorkloadNamePrefix)
			workload := config.CreateFn(ctx, k8sClient, TestNamespaceName, name)
			createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload.Get())

			DeleteAllEvents(ctx, clientset, TestNamespaceName)
			AddOptOutLabel(workload.GetObjectMeta())
			UpdateWorkload(ctx, k8sClient, workload.Get())

			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
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
			createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload.Get())

			DeleteAllEvents(ctx, clientset, TestNamespaceName)
			RemoveOptOutLabel(workload.GetObjectMeta())
			UpdateWorkload(ctx, k8sClient, workload.Get())

			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
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
			createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload.Get())

			DeleteAllEvents(ctx, clientset, TestNamespaceName)
			UpdateLabel(workload.GetObjectMeta(), "dash0.com/enable", "true")
			UpdateWorkload(ctx, k8sClient, workload.Get())

			workload = config.GetFn(ctx, k8sClient, TestNamespaceName, name)
			config.VerifyFn(workload)
			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
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
			createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload.Get())
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

	Describe("with a custom instrument workloads label selector", Ordered, func() {
		BeforeAll(func() {
			EnsureMonitoringResourceWithSpecExistsAndIsAvailable(
				ctx,
				k8sClient,
				dash0v1beta1.Dash0MonitoringSpec{
					InstrumentWorkloads: dash0v1beta1.InstrumentWorkloads{
						LabelSelector: "dash0-auto-instrument=yes",
					},
					Export: &dash0common.Export{
						Dash0: &dash0common.Dash0Configuration{
							Endpoint: EndpointDash0Test,
							Authorization: dash0common.Authorization{
								Token: &AuthorizationTokenTest,
							},
						},
					},
				},
			)
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("when a new uninstrumented workload matches the custom auto-instrumentation label selector", func() {
			name := UniqueName(DeploymentNamePrefix)
			deployment := BasicDeployment(TestNamespaceName, name)
			deployment.Labels = map[string]string{"dash0-auto-instrument": "yes"}

			workload := CreateWorkload(ctx, k8sClient, deployment)
			createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload)

			deployment = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
			VerifyModifiedDeployment(deployment, BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
		})

		It("when a new uninstrumented workload does not match the custom auto-instrumentation label selector", func() {
			name := UniqueName(DeploymentNamePrefix)
			deployment := BasicDeployment(TestNamespaceName, name)

			workload := CreateWorkload(ctx, k8sClient, deployment)
			createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload)

			deployment = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
			VerifyNoEvents(ctx, clientset, TestNamespaceName)
			VerifyUnmodifiedDeployment(deployment)
		})

		It("when an instrumented workload no longer matches the custom auto-instrumentation label selector", func() {
			name := UniqueName(DeploymentNamePrefix)
			deployment := BasicDeployment(TestNamespaceName, name)
			deployment.Labels = map[string]string{"dash0-auto-instrument": "yes"}
			workload := CreateWorkload(ctx, k8sClient, deployment)
			createdObjectsInstrumentationWebhookTest = append(createdObjectsInstrumentationWebhookTest, workload)
			deployment = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
			VerifyModifiedDeployment(deployment, BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)

			DeleteAllEvents(ctx, clientset, TestNamespaceName)
			deployment.Labels["dash0-auto-instrument"] = "nope"
			UpdateWorkload(ctx, k8sClient, deployment)

			VerifySuccessfulUninstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
			deployment = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
			VerifyUnmodifiedDeployment(deployment)
		})
	})

	Describe("when the Dash0 monitoring resource does not exist", func() {
		It("should not instrument workloads", func() {
			createdObjectsInstrumentationWebhookTest = verifyThatDeploymentIsNotBeingInstrumented(createdObjectsInstrumentationWebhookTest)
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
			createdObjectsInstrumentationWebhookTest = verifyThatDeploymentIsNotBeingInstrumented(createdObjectsInstrumentationWebhookTest)
		})
	})

	Describe("when the Dash0 monitoring resource exists and is available and has instrumentWorkloads.mode=all set explicitly", Ordered, func() {
		BeforeAll(func() {
			dash0MonitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads.Mode = dash0common.InstrumentWorkloadsModeAll
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should instrument workloads", func() {
			createdObjectsInstrumentationWebhookTest = verifyThatDeploymentIsInstrumented(createdObjectsInstrumentationWebhookTest)
		})
	})

	Describe("when the Dash0 monitoring resource exists and is available but has instrumentWorkloads.mode=none set", Ordered, func() {
		BeforeAll(func() {
			dash0MonitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads.Mode = dash0common.InstrumentWorkloadsModeNone
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjectsInstrumentationWebhookTest = verifyThatDeploymentIsNotBeingInstrumented(createdObjectsInstrumentationWebhookTest)
		})
	})

	Describe("when the Dash0 monitoring resource exists and is available and has instrumentWorkloads.mode=created-and-updated set", Ordered, func() {
		BeforeAll(func() {
			dash0MonitoringResource := EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
			dash0MonitoringResource.Spec.InstrumentWorkloads.Mode = dash0common.InstrumentWorkloadsModeCreatedAndUpdated
			Expect(k8sClient.Update(ctx, dash0MonitoringResource)).To(Succeed())
		})

		AfterAll(func() {
			DeleteMonitoringResource(ctx, k8sClient)
		})

		It("should instrument workloads", func() {
			createdObjectsInstrumentationWebhookTest = verifyThatDeploymentIsInstrumented(createdObjectsInstrumentationWebhookTest)
		})
	})
})

func verifyThatDeploymentIsInstrumented(createdObjects []client.Object) []client.Object {
	name := UniqueName(DeploymentNamePrefix)
	workload := CreateBasicDeployment(ctx, k8sClient, TestNamespaceName, name)
	createdObjects = append(createdObjects, workload)
	workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
	VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), VerifyNoManagedFields)
	VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, testActor)
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
