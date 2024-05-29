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
		Versions: util.Versions{OperatorVersion: "1.2.3",
			InitContainerImageVersion: "4.5.6",
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
			deployment := BasicDeployment(TestNamespaceName, DeploymentName)
			result := workloadModifier.ModifyDeployment(deployment)

			Expect(result).To(BeTrue())
			VerifyModifiedDeployment(deployment, BasicInstrumentedPodSpecExpectations)
		})

		It("should add Dash0 to a deployment that has multiple containers, and already has volumes and init containers", func() {
			deployment := DeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentName)
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
			deployment := DeploymentWithExistingDash0Artifacts(TestNamespaceName, DeploymentName)
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

		It("should add Dash0 to a basic cron job", func() {
			workload := BasicCronJob(TestNamespaceName, CronJobName)
			result := workloadModifier.ModifyCronJob(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedCronJob(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should add Dash0 to a basic daemon set", func() {
			workload := BasicDaemonSet(TestNamespaceName, DaemonSetName)
			result := workloadModifier.ModifyDaemonSet(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedDaemonSet(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should add Dash0 to a basic job", func() {
			workload := BasicJob(TestNamespaceName, JobName1)
			result := workloadModifier.ModifyJob(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedJob(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should add Dash0 to a basic replica set", func() {
			workload := BasicReplicaSet(TestNamespaceName, ReplicaSetName)
			result := workloadModifier.ModifyReplicaSet(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should not add Dash0 to a basic replica set that is owned by a deployment", func() {
			workload := ReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetName)
			result := workloadModifier.ModifyReplicaSet(workload)

			Expect(result).To(BeFalse())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should add Dash0 to a basic stateful set", func() {
			workload := BasicStatefulSet(TestNamespaceName, StatefulSetName)
			result := workloadModifier.ModifyStatefulSet(workload)

			Expect(result).To(BeTrue())
			VerifyModifiedStatefulSet(workload, BasicInstrumentedPodSpecExpectations)
		})
	})

	Context("when reverting workloads", func() {
		It("should remove Dash0 from an instrumented deployment", func() {
			deployment := InstrumentedDeployment(TestNamespaceName, DeploymentName)
			result := workloadModifier.RevertDeployment(deployment)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedDeployment(deployment)
		})

		It("should only remove labels from deployment that has dash0.instrumented=false", func() {
			deployment := DeploymentWithInstrumentedFalseLabel(TestNamespaceName, DeploymentName)
			result := workloadModifier.RevertDeployment(deployment)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedDeployment(deployment)
		})

		It("should remove Dash0 from a instrumented deployment that has multiple containers, and already has volumes and init containers previous to being instrumented", func() {
			deployment := InstrumentedDeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentName)
			result := workloadModifier.RevertDeployment(deployment)

			Expect(result).To(BeTrue())
			VerifyRevertedDeployment(deployment, PodSpecExpectations{
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
			workload := InstrumentedCronJob(TestNamespaceName, CronJobName)
			result := workloadModifier.RevertCronJob(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedCronJob(workload)
		})

		It("should remove Dash0 from an instrumented daemon set", func() {
			workload := InstrumentedDaemonSet(TestNamespaceName, DaemonSetName)
			result := workloadModifier.RevertDaemonSet(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedDaemonSet(workload)
		})

		It("should remove Dash0 from an instrumented job", func() {
			workload := InstrumentedJob(TestNamespaceName, JobName1)
			result := workloadModifier.RevertJob(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedJob(workload)
		})

		It("should remove Dash0 from an instrumented replica set", func() {
			workload := InstrumentedReplicaSet(TestNamespaceName, ReplicaSetName)
			result := workloadModifier.RevertReplicaSet(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should not remove Dash0 from a replica set that is owned by a deployment", func() {
			workload := InstrumentedReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetName)
			result := workloadModifier.RevertReplicaSet(workload)

			Expect(result).To(BeFalse())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations)
		})

		It("should remove Dash0 from an instrumented stateful set", func() {
			workload := InstrumentedStatefulSet(TestNamespaceName, StatefulSetName)
			result := workloadModifier.RevertStatefulSet(workload)

			Expect(result).To(BeTrue())
			VerifyUnmodifiedStatefulSet(workload)
		})
	})
})
