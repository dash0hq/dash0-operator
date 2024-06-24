// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// Maintenance note: There is some overlap of test cases between this file and workloads/modify_test.go. This is
// intentional. However, this test should be used to verify external effects (recording events etc.) that cannot be
// covered in modify_test.go, while more fine-grained test cases and variations should rather be added to
// workloads/modify_test.go.

var _ = Describe("The Dash0 webhook", func() {
	var createdObjects []client.Object

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
		DeleteAllEvents(ctx, clientset, TestNamespaceName)
	})

	Describe("when the Dash0 custom resource exists and is available", Ordered, func() {
		BeforeAll(func() {
			EnsureDash0CustomResourceExistsAndIsAvailable(ctx, k8sClient)
		})

		AfterAll(func() {
			RemoveDash0CustomResource(ctx, k8sClient)
		})

		Describe("when mutating new deployments", Ordered, func() {
			It("should instrument a new basic deployment", func() {
				createdObjects = verifyDeploymentIsBeingInstrumented(createdObjects)
			})

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
							VolumeMounts:                             2,
							Dash0VolumeMountIdx:                      1,
							EnvVars:                                  3,
							NodeOptionsEnvVarIdx:                     1,
							Dash0CollectorBaseUrlEnvVarIdx:           2,
							Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-operator-opentelemetry-collector.dash0-operator-system.svc.cluster.local:4318",
						},
						{
							VolumeMounts:                             3,
							Dash0VolumeMountIdx:                      2,
							EnvVars:                                  4,
							NodeOptionsEnvVarIdx:                     2,
							Dash0CollectorBaseUrlEnvVarIdx:           3,
							Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-operator-opentelemetry-collector.dash0-operator-system.svc.cluster.local:4318",
						},
					},
				})
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})

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
							VolumeMounts:                             2,
							Dash0VolumeMountIdx:                      1,
							EnvVars:                                  3,
							NodeOptionsEnvVarIdx:                     1,
							NodeOptionsUsesValueFrom:                 true,
							Dash0CollectorBaseUrlEnvVarIdx:           2,
							Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-operator-opentelemetry-collector.dash0-operator-system.svc.cluster.local:4318",
						},
						{
							VolumeMounts:                             3,
							Dash0VolumeMountIdx:                      1,
							EnvVars:                                  3,
							NodeOptionsEnvVarIdx:                     1,
							NodeOptionsValue:                         "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --require something-else --experimental-modules",
							Dash0CollectorBaseUrlEnvVarIdx:           0,
							Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-operator-opentelemetry-collector.dash0-operator-system.svc.cluster.local:4318",
						},
					},
				})
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})

			It("should instrument a new basic cron job", func() {
				name := UniqueName(CronJobNamePrefix)
				workload := CreateBasicCronJob(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetCronJob(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedCronJob(workload, BasicInstrumentedPodSpecExpectations)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})

			It("should instrument a new basic daemon set", func() {
				name := UniqueName(DaemonSetNamePrefix)
				workload := CreateBasicDaemonSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetDaemonSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedDaemonSet(workload, BasicInstrumentedPodSpecExpectations)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})

			It("should instrument a new basic job", func() {
				name := UniqueName(JobNamePrefix)
				workload := CreateBasicJob(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetJob(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedJob(workload, BasicInstrumentedPodSpecExpectations)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})

			It("should instrument a new basic ownerless pod", func() {
				name := UniqueName(PodNamePrefix)
				workload := CreateBasicPod(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetPod(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedPod(workload, BasicInstrumentedPodSpecExpectations)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})

			It("should not instrument a new pod owned by a replica set", func() {
				name := UniqueName(PodNamePrefix)
				workload := CreatePodOwnedByReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetPod(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedPod(workload)
				VerifyNoInstrumentationNecessaryEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})

			It("should instrument a new basic replica set", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := CreateBasicReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})

			It("should not instrument a new replica set owned by a deployment", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := CreateReplicaSetOwnedByDeployment(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedReplicaSet(workload)
				VerifyNoInstrumentationNecessaryEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})

			It("should instrument a new basic stateful set", func() {
				name := UniqueName(StatefulSetNamePrefix)
				workload := CreateBasicStatefulSet(ctx, k8sClient, TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				workload = GetStatefulSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyModifiedStatefulSet(workload, BasicInstrumentedPodSpecExpectations)
				VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, name, "webhook")
			})
		})

		Describe("when workload has opted out of instrumentation", func() {
			It("should not instrument a cron job that has opted out of instrumentation", func() {
				name := UniqueName(CronJobNamePrefix)
				workload := CronJobWithOptOutLabel(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetCronJob(ctx, k8sClient, TestNamespaceName, name)
				VerifyCronJobWithOptOutLabel(workload)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a daemonset that has opted out of instrumentation", func() {
				name := UniqueName(DaemonSetNamePrefix)
				workload := DaemonSetWithOptOutLabel(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetDaemonSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyDaemonSetWithOptOutLabel(workload)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a deployment that has opted out of instrumentation", func() {
				name := UniqueName(DeploymentNamePrefix)
				workload := DeploymentWithOptOutLabel(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
				VerifyDeploymentWithOptOutLabel(workload)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a job that has opted out of instrumentation", func() {
				name := UniqueName(JobNamePrefix)
				workload := JobWithOptOutLabel(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetJob(ctx, k8sClient, TestNamespaceName, name)
				VerifyJobWithOptOutLabel(workload)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument an ownerless pod that has opted out of instrumentation", func() {
				name := UniqueName(PodNamePrefix)
				workload := PodWithOptOutLabel(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetPod(ctx, k8sClient, TestNamespaceName, name)
				VerifyPodWithOptOutLabel(workload)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument an ownerless replica set that has opted out of instrumentation", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := ReplicaSetWithOptOutLabel(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyReplicaSetWithOptOutLabel(workload)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a stateful set that has opted out of instrumentation", func() {
				name := UniqueName(StatefulSetNamePrefix)
				workload := StatefulSetWithOptOutLabel(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetStatefulSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyStatefulSetWithOptOutLabel(workload)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})
		})

		Describe("when seeing the ignore once label", func() {
			It("should not instrument a cron job that has the label, but remove the label", func() {
				name := UniqueName(CronJobNamePrefix)
				workload := BasicCronJob(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddLabel(&workload.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetCronJob(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedCronJob(workload)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a daemonset that has the label, but remove the label", func() {
				name := UniqueName(DaemonSetNamePrefix)
				workload := BasicDaemonSet(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddLabel(&workload.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetDaemonSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedDaemonSet(workload)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a deployment that has the label, but remove the label", func() {
				name := UniqueName(DeploymentNamePrefix)
				workload := BasicDeployment(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddLabel(&workload.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedDeployment(workload)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a job that has the label, but remove the label", func() {
				name := UniqueName(JobNamePrefix)
				workload := BasicJob(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddLabel(&workload.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetJob(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedJob(workload)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument an ownerless pod that has the label, but remove the label", func() {
				name := UniqueName(PodNamePrefix)
				workload := BasicPod(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddLabel(&workload.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetPod(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedPod(workload)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument an ownerless replica set that has the label, but remove the label", func() {
				name := UniqueName(ReplicaSetNamePrefix)
				workload := BasicReplicaSet(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddLabel(&workload.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetReplicaSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedReplicaSet(workload)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})

			It("should not instrument a stateful set that has the label, but remove the label", func() {
				name := UniqueName(StatefulSetNamePrefix)
				workload := BasicStatefulSet(TestNamespaceName, name)
				createdObjects = append(createdObjects, workload)
				AddLabel(&workload.ObjectMeta, "dash0.com/webhook-ignore-once", "true")
				CreateWorkload(ctx, k8sClient, workload)
				workload = GetStatefulSet(ctx, k8sClient, TestNamespaceName, name)
				VerifyUnmodifiedStatefulSet(workload)
				VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
				VerifyNoEvents(ctx, clientset, TestNamespaceName)
			})
		})
	})

	Describe("when the Dash0 resource does not exist", func() {
		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(createdObjects)
		})
	})

	Describe("when the Dash0 resource exists but is not available", Ordered, func() {
		BeforeAll(func() {
			EnsureDash0CustomResourceExistsAndIsDegraded(ctx, k8sClient)
		})

		AfterAll(func() {
			RemoveDash0CustomResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(createdObjects)
		})
	})

	Describe("when the Dash0 resource exists and is available but has InstrumentWorkloads=false set", Ordered, func() {
		BeforeAll(func() {
			dash0CustomResource := EnsureDash0CustomResourceExistsAndIsAvailable(ctx, k8sClient)
			dash0CustomResource.Spec.InstrumentWorkloads = &False
			Expect(k8sClient.Update(ctx, dash0CustomResource)).To(Succeed())
		})

		AfterAll(func() {
			RemoveDash0CustomResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(createdObjects)
		})
	})

	Describe("when the Dash0 resource exists and is available but has InstrumentNewWorkloads=false set", Ordered, func() {
		BeforeAll(func() {
			dash0CustomResource := EnsureDash0CustomResourceExistsAndIsAvailable(ctx, k8sClient)
			dash0CustomResource.Spec.InstrumentNewWorkloads = &False
			Expect(k8sClient.Update(ctx, dash0CustomResource)).To(Succeed())
		})

		AfterAll(func() {
			RemoveDash0CustomResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyThatDeploymentIsNotBeingInstrumented(createdObjects)
		})
	})

	Describe("when the Dash0 resource exists and is available and has InstrumentExistingWorkloads=false set", Ordered, func() {
		BeforeAll(func() {
			dash0CustomResource := EnsureDash0CustomResourceExistsAndIsAvailable(ctx, k8sClient)
			dash0CustomResource.Spec.InstrumentExistingWorkloads = &False
			Expect(k8sClient.Update(ctx, dash0CustomResource)).To(Succeed())
		})

		AfterAll(func() {
			RemoveDash0CustomResource(ctx, k8sClient)
		})

		It("should not instrument workloads", func() {
			createdObjects = verifyDeploymentIsBeingInstrumented(createdObjects)
		})
	})
})

func verifyDeploymentIsBeingInstrumented(createdObjects []client.Object) []client.Object {
	name := UniqueName(DeploymentNamePrefix)
	workload := CreateBasicDeployment(ctx, k8sClient, TestNamespaceName, name)
	createdObjects = append(createdObjects, workload)
	workload = GetDeployment(ctx, k8sClient, TestNamespaceName, name)
	VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations)
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
