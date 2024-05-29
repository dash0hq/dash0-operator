// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"fmt"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// Maintenance note: There is some overlap of test cases between this file and k8sresources/modify_test.go. This is
// intentional. However, this test should be used to verify external effects (recording events etc.) that cannot be
// covered modify_test.go, while more fine-grained test cases and variations should rather be added to
// k8sresources/modify_test.go.

var _ = Describe("Dash0 Webhook", func() {
	AfterEach(func() {
		_ = k8sClient.Delete(ctx, BasicCronJob(TestNamespaceName, CronJobName))
		_ = k8sClient.Delete(ctx, BasicDaemonSet(TestNamespaceName, DaemonSetName))
		_ = k8sClient.Delete(ctx, BasicDeployment(TestNamespaceName, DeploymentName))
		_ = k8sClient.Delete(ctx, BasicJob(TestNamespaceName, JobName1))
		err := k8sClient.Delete(ctx, BasicReplicaSet(TestNamespaceName, ReplicaSetName))
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "cannot delete replicaset: %v\n", err)
		}
		_ = k8sClient.Delete(ctx, BasicStatefulSet(TestNamespaceName, StatefulSetName))

	})

	Context("when mutating new deployments", func() {
		It("should inject Dash0 into a new basic deployment", func() {
			CreateBasicDeployment(ctx, k8sClient, TestNamespaceName, DeploymentName)
			deployment := GetDeployment(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyModifiedDeployment(deployment, BasicInstrumentedPodSpecExpectations)
		})

		It("should inject Dash0 into a new deployment that has multiple containers, and already has volumes and init containers", func() {
			deployment := DeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deployment = GetDeployment(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyModifiedDeployment(deployment, PodSpecExpectations{
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
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
					},
					{
						VolumeMounts:                             3,
						Dash0VolumeMountIdx:                      2,
						EnvVars:                                  4,
						NodeOptionsEnvVarIdx:                     2,
						Dash0CollectorBaseUrlEnvVarIdx:           3,
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
					},
				},
			})
			VerifySuccessfulInstrumentationEvent(ctx, clientset, TestNamespaceName, DeploymentName, "webhook")
		})

		It("should update existing Dash0 artifacts in a new deployment", func() {
			deployment := DeploymentWithExistingDash0Artifacts(TestNamespaceName, DeploymentName)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deployment = GetDeployment(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyModifiedDeployment(deployment, PodSpecExpectations{
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
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
					},
					{
						VolumeMounts:                             3,
						Dash0VolumeMountIdx:                      1,
						EnvVars:                                  3,
						NodeOptionsEnvVarIdx:                     1,
						NodeOptionsValue:                         "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js --require something-else --experimental-modules",
						Dash0CollectorBaseUrlEnvVarIdx:           0,
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
					},
				},
			})
		})

		It("should inject Dash0 into a new basic cron job", func() {
			CreateBasicCronJob(ctx, k8sClient, TestNamespaceName, CronJobName)
			cronJob := GetCronJob(ctx, k8sClient, TestNamespaceName, CronJobName)
			VerifyModifiedCronJob(cronJob, BasicInstrumentedPodSpecExpectations)
		})

		It("should inject Dash0 into a new basic daemon set", func() {
			CreateBasicDaemonSet(ctx, k8sClient, TestNamespaceName, DaemonSetName)
			daemonSet := GetDaemonSet(ctx, k8sClient, TestNamespaceName, DaemonSetName)
			VerifyModifiedDaemonSet(daemonSet, BasicInstrumentedPodSpecExpectations)
		})

		It("should inject Dash0 into a new basic job", func() {
			CreateBasicJob(ctx, k8sClient, TestNamespaceName, JobName1)
			job := GetJob(ctx, k8sClient, TestNamespaceName, JobName1)
			VerifyModifiedJob(job, BasicInstrumentedPodSpecExpectations)
		})

		It("should inject Dash0 into a new basic replica set", func() {
			CreateBasicReplicaSet(ctx, k8sClient, TestNamespaceName, ReplicaSetName)
			replicaSet := GetReplicaSet(ctx, k8sClient, TestNamespaceName, ReplicaSetName)
			VerifyModifiedReplicaSet(replicaSet, BasicInstrumentedPodSpecExpectations)
		})

		It("should not inject Dash0 into a new replica set owned by a deployment", func() {
			CreateReplicaSetOwnedByDeployment(ctx, k8sClient, TestNamespaceName, ReplicaSetName)
			replicaSet := GetReplicaSet(ctx, k8sClient, TestNamespaceName, ReplicaSetName)
			VerifyUnmodifiedReplicaSet(replicaSet)
		})

		It("should inject Dash0 into a new basic stateful set", func() {
			CreateBasicStatefulSet(ctx, k8sClient, TestNamespaceName, StatefulSetName)
			statefulSet := GetStatefulSet(ctx, k8sClient, TestNamespaceName, StatefulSetName)
			VerifyModifiedStatefulSet(statefulSet, BasicInstrumentedPodSpecExpectations)
		})
	})

	Context("when seeing the ignore once label", func() {
		It("should not instrument a cron job that has the label, but remove the label", func() {
			workload := BasicCronJob(TestNamespaceName, DeploymentName)
			AddLabel(&workload.ObjectMeta, util.WebhookIgnoreOnceLabelKey, "true")
			CreateWorkload(ctx, k8sClient, workload)
			workload = GetCronJob(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyUnmodifiedCronJob(workload)
			VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
		})

		It("should not instrument a daemonset that has the label, but remove the label", func() {
			workload := BasicDaemonSet(TestNamespaceName, DeploymentName)
			AddLabel(&workload.ObjectMeta, util.WebhookIgnoreOnceLabelKey, "true")
			CreateWorkload(ctx, k8sClient, workload)
			workload = GetDaemonSet(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyUnmodifiedDaemonSet(workload)
			VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
		})

		It("should not instrument a deployment that has the label, but remove the label", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentName)
			AddLabel(&workload.ObjectMeta, util.WebhookIgnoreOnceLabelKey, "true")
			CreateWorkload(ctx, k8sClient, workload)
			workload = GetDeployment(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyUnmodifiedDeployment(workload)
			VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
		})

		It("should not instrument a job that has the label, but remove the label", func() {
			workload := BasicJob(TestNamespaceName, DeploymentName)
			AddLabel(&workload.ObjectMeta, util.WebhookIgnoreOnceLabelKey, "true")
			CreateWorkload(ctx, k8sClient, workload)
			workload = GetJob(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyUnmodifiedJob(workload)
			VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
		})

		It("should not instrument an orphan replica set that has the label, but remove the label", func() {
			workload := BasicReplicaSet(TestNamespaceName, DeploymentName)
			AddLabel(&workload.ObjectMeta, util.WebhookIgnoreOnceLabelKey, "true")
			CreateWorkload(ctx, k8sClient, workload)
			workload = GetReplicaSet(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyUnmodifiedReplicaSet(workload)
			VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
		})

		It("should not instrument a stateful set that has the label, but remove the label", func() {
			workload := BasicStatefulSet(TestNamespaceName, DeploymentName)
			AddLabel(&workload.ObjectMeta, util.WebhookIgnoreOnceLabelKey, "true")
			CreateWorkload(ctx, k8sClient, workload)
			workload = GetStatefulSet(ctx, k8sClient, TestNamespaceName, DeploymentName)
			VerifyUnmodifiedStatefulSet(workload)
			VerifyWebhookIgnoreOnceLabelIsAbesent(&workload.ObjectMeta)
		})
	})

})
