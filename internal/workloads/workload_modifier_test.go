// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// Maintenance note: There is some overlap of test cases between this file and dash0_webhook_test.go. This is
// intentional. However, this test should be used for more fine-grained test cases, while dash0_webhook_test.go should
// be used to verify external effects (recording events etc.) that cannot be covered in this test.

var (
	instrumentationMetadata = util.InstrumentationMetadata{
		Images: util.Images{OperatorImage: "some-registry.com:1234/dash0-operator-controller:1.2.3",
			InitContainerImage: "some-registry.com:1234/dash0-instrumentation:4.5.6",
		},
		InstrumentedBy: "modify_test",
	}
)

var _ = Describe("Dash0 Workload Modification", func() {

	ctx := context.Background()
	logger := log.FromContext(ctx)
	workloadModifier := NewResourceModifier(instrumentationMetadata, &logger)

	Context("when instrumenting workloads", func() {
		It("should add Dash0 to a basic deployment", func() {
			deployment := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.ModifyDeployment(deployment)

			Expect(result).To(BeTrue())
			VerifyModifiedDeployment(deployment, BasicInstrumentedPodSpecExpectations)
		})

		It("should instrument a deployment that has multiple containers, and already has volumes and init containers", func() {
			deployment := DeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.ModifyDeployment(deployment)

			Expect(result).To(BeTrue())
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
		})

		It("should update existing Dash0 artifacts in a deployment", func() {
			deployment := DeploymentWithExistingDash0Artifacts(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.ModifyDeployment(deployment)

			Expect(result).To(BeTrue())
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

		It("should not touch a deployment that has opted out of instrumentation", func() {
			workload := DeploymentWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.ModifyDeployment(workload)

			Expect(result).To(BeFalse())
			VerifyDeploymentWithOptOutLabel(workload)
		})

		It("should instrument a basic cron job", func() {
			workload := BasicCronJob(TestNamespaceName, CronJobNamePrefix)
			result := workloadModifier.ModifyCronJob(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedCronJob(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should not touch a cron job that has opted out of instrumentation", func() {
			workload := CronJobWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.ModifyCronJob(workload)

			Expect(result).To(BeFalse())
			VerifyCronJobWithOptOutLabel(workload)
		})

		It("should instrument a basic daemon set", func() {
			workload := BasicDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			result := workloadModifier.ModifyDaemonSet(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedDaemonSet(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should not touch a daemon set that has opted out of instrumentation", func() {
			workload := DaemonSetWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.ModifyDaemonSet(workload)

			Expect(result).To(BeFalse())
			VerifyDaemonSetWithOptOutLabel(workload)
		})

		It("should instrument a basic job", func() {
			workload := BasicJob(TestNamespaceName, JobNamePrefix)
			result := workloadModifier.ModifyJob(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedJob(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should not touch a job that has opted out of instrumentation", func() {
			workload := JobWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.ModifyJob(workload)

			Expect(result).To(BeFalse())
			VerifyJobWithOptOutLabel(workload)
		})

		It("should instrument a basic replica set", func() {
			workload := BasicReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			result := workloadModifier.ModifyReplicaSet(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should not instrument a basic replica set that is owned by a deployment", func() {
			workload := ReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetNamePrefix)
			result := workloadModifier.ModifyReplicaSet(workload)

			Expect(result).To(BeFalse())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should not touch a replica set that has opted out of instrumentation", func() {
			workload := ReplicaSetWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.ModifyReplicaSet(workload)

			Expect(result).To(BeFalse())
			VerifyReplicaSetWithOptOutLabel(workload)
		})

		It("should instrument a basic stateful set", func() {
			workload := BasicStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			result := workloadModifier.ModifyStatefulSet(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedStatefulSet(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should not touch a stateful set that has opted out of instrumentation", func() {
			workload := StatefulSetWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.ModifyStatefulSet(workload)

			Expect(result).To(BeFalse())
			VerifyStatefulSetWithOptOutLabel(workload)
		})
	})

	Context("when reverting workloads", func() {
		It("should remove Dash0 from an instrumented deployment", func() {
			workload := InstrumentedDeployment(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.RevertDeployment(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedDeployment(workload)
		})

		It("should not touch deployments that have opted out of instrumentation", func() {
			workload := DeploymentWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.RevertDeployment(workload)

			Expect(result).To(BeFalse())
			VerifyDeploymentWithOptOutLabel(workload)
		})

		It("should remove Dash0 from a instrumented deployment that has multiple containers, and already has volumes and init containers previous to being instrumented", func() {
			workload := InstrumentedDeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.RevertDeployment(workload)

			Expect(result).To(BeTrue())
			VerifyRevertedDeployment(workload, PodSpecExpectations{
				Volumes:               2,
				Dash0VolumeIdx:        -1,
				InitContainers:        2,
				Dash0InitContainerIdx: -1,
				Containers: []ContainerExpectations{
					{
						VolumeMounts:                   1,
						Dash0VolumeMountIdx:            -1,
						EnvVars:                        1,
						NodeOptionsEnvVarIdx:           -1,
						Dash0CollectorBaseUrlEnvVarIdx: -1,
					},
					{
						VolumeMounts:                   2,
						Dash0VolumeMountIdx:            -1,
						EnvVars:                        2,
						NodeOptionsEnvVarIdx:           -1,
						Dash0CollectorBaseUrlEnvVarIdx: -1,
					},
				},
			})
		})

		It("should remove Dash0 from an instrumented cron job", func() {
			workload := InstrumentedCronJob(TestNamespaceName, CronJobNamePrefix)
			result := workloadModifier.RevertCronJob(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedCronJob(workload)
		})

		It("should not touch cron jobs that have opted out of instrumentation", func() {
			workload := CronJobWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.RevertCronJob(workload)

			Expect(result).To(BeFalse())
			VerifyCronJobWithOptOutLabel(workload)
		})

		It("should remove Dash0 from an instrumented daemon set", func() {
			workload := InstrumentedDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			result := workloadModifier.RevertDaemonSet(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedDaemonSet(workload)
		})

		It("should not touch daemon sets that have opted out of instrumentation", func() {
			workload := DaemonSetWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.RevertDaemonSet(workload)

			Expect(result).To(BeFalse())
			VerifyDaemonSetWithOptOutLabel(workload)
		})

		It("should remove Dash0 from an instrumented job", func() {
			workload := InstrumentedJob(TestNamespaceName, JobNamePrefix)
			result := workloadModifier.RevertJob(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedJob(workload)
		})

		It("should not touch jobs that have opted out of instrumentation", func() {
			workload := JobWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.RevertJob(workload)

			Expect(result).To(BeFalse())
			VerifyJobWithOptOutLabel(workload)
		})

		It("should remove Dash0 from an instrumented replica set", func() {
			workload := InstrumentedReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			result := workloadModifier.RevertReplicaSet(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should not touch replica sets that have opted out of instrumentation", func() {
			workload := ReplicaSetWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.RevertReplicaSet(workload)

			Expect(result).To(BeFalse())
			VerifyReplicaSetWithOptOutLabel(workload)
		})

		It("should not remove Dash0 from a replica set that is owned by a deployment", func() {
			workload := InstrumentedReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetNamePrefix)
			result := workloadModifier.RevertReplicaSet(workload)

			Expect(result).To(BeFalse())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should remove Dash0 from an instrumented stateful set", func() {
			workload := InstrumentedStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			result := workloadModifier.RevertStatefulSet(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedStatefulSet(workload)
		})

		It("should not touch a stateful sets that have opted out of instrumentation", func() {
			workload := StatefulSetWithOptOutLabel(TestNamespaceName, DeploymentNamePrefix)
			result := workloadModifier.RevertStatefulSet(workload)

			Expect(result).To(BeFalse())
			VerifyStatefulSetWithOptOutLabel(workload)
		})
	})
})
