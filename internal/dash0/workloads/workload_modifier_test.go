// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"context"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/internal/dash0/util"

	. "github.com/dash0hq/dash0-operator/test/util"
)

// Maintenance note: There is some overlap of test cases between this file and dash0_webhook_test.go. This is
// intentional. However, this test should be used for more fine-grained test cases, while dash0_webhook_test.go should
// be used to verify external effects (recording events etc.) that cannot be covered in this test.

var (
	instrumentationMetadata = util.InstrumentationMetadata{
		Images: util.Images{
			OperatorImage:                "some-registry.com:1234/dash0hq/operator-controller:1.2.3",
			InitContainerImage:           "some-registry.com:1234/dash0hq/instrumentation:4.5.6",
			InitContainerImagePullPolicy: corev1.PullAlways,
		},
		OtelCollectorBaseUrl: "http://dash0-operator-opentelemetry-collector.dash0-system.svc.cluster.local:4318",
		InstrumentedBy:       "modify_test",
	}
)

var _ = Describe("Dash0 Workload Modification", func() {

	ctx := context.Background()
	logger := log.FromContext(ctx)
	workloadModifier := NewResourceModifier(instrumentationMetadata, &logger)

	Describe("when instrumenting workloads", func() {
		It("should instrument a basic cron job", func() {
			workload := BasicCronJob(TestNamespaceName, CronJobNamePrefix)
			hasBeenModified := workloadModifier.ModifyCronJob(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyModifiedCronJob(workload, BasicInstrumentedPodSpecExpectations())
		})

		It("should instrument a basic daemon set", func() {
			workload := BasicDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			hasBeenModified := workloadModifier.ModifyDaemonSet(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyModifiedDaemonSet(workload, BasicInstrumentedPodSpecExpectations())
		})

		It("should add Dash0 to a basic deployment", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			hasBeenModified := workloadModifier.ModifyDeployment(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations())
		})

		It("should instrument a deployment that has multiple containers, and already has volumes and init containers", func() {
			workload := DeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentNamePrefix)
			hasBeenModified := workloadModifier.ModifyDeployment(workload)

			Expect(hasBeenModified).To(BeTrue())
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
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-operator-opentelemetry-collector.dash0-system.svc.cluster.local:4318",
					},
					{
						VolumeMounts:                             3,
						Dash0VolumeMountIdx:                      2,
						EnvVars:                                  4,
						NodeOptionsEnvVarIdx:                     2,
						Dash0CollectorBaseUrlEnvVarIdx:           3,
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-operator-opentelemetry-collector.dash0-system.svc.cluster.local:4318",
					},
				},
			})
		})

		It("should update existing Dash0 artifacts in a deployment", func() {
			workload := DeploymentWithExistingDash0Artifacts(TestNamespaceName, DeploymentNamePrefix)
			hasBeenModified := workloadModifier.ModifyDeployment(workload)

			Expect(hasBeenModified).To(BeTrue())
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
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-operator-opentelemetry-collector.dash0-system.svc.cluster.local:4318",
					},
					{
						VolumeMounts:                             3,
						Dash0VolumeMountIdx:                      1,
						EnvVars:                                  3,
						NodeOptionsEnvVarIdx:                     1,
						NodeOptionsValue:                         "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --require something-else --experimental-modules",
						Dash0CollectorBaseUrlEnvVarIdx:           0,
						Dash0CollectorBaseUrlEnvVarExpectedValue: "http://dash0-operator-opentelemetry-collector.dash0-system.svc.cluster.local:4318",
					},
				},
			})
		})

		It("should instrument a basic job", func() {
			workload := BasicJob(TestNamespaceName, JobNamePrefix)
			hasBeenModified := workloadModifier.ModifyJob(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyModifiedJob(workload, BasicInstrumentedPodSpecExpectations())
		})

		It("should instrument a basic ownerless pod", func() {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			hasBeenModified := workloadModifier.ModifyPod(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyModifiedPod(workload, BasicInstrumentedPodSpecExpectations())
		})

		It("should not instrument a basic pod owned by another higher level workload", func() {
			workload := PodOwnedByReplicaSet(TestNamespaceName, PodNamePrefix)
			hasBeenModified := workloadModifier.ModifyPod(workload)

			Expect(hasBeenModified).To(BeFalse())
			VerifyUnmodifiedPod(workload)
		})

		It("should instrument a basic ownerless replica set", func() {
			workload := BasicReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			hasBeenModified := workloadModifier.ModifyReplicaSet(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations())
		})

		It("should not instrument a basic replica set that is owned by a deployment", func() {
			workload := ReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetNamePrefix)
			hasBeenModified := workloadModifier.ModifyReplicaSet(workload)

			Expect(hasBeenModified).To(BeFalse())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should instrument a basic stateful set", func() {
			workload := BasicStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			hasBeenModified := workloadModifier.ModifyStatefulSet(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyModifiedStatefulSet(workload, BasicInstrumentedPodSpecExpectations())
		})
	})

	Describe("when instrumenting workloads multiple times (instrumentation needs to be idempotent)", func() {
		It("cron job instrumentation needs to be idempotent", func() {
			workload := BasicCronJob(TestNamespaceName, CronJobNamePrefix)
			hasBeenModified := workloadModifier.ModifyCronJob(workload)
			Expect(hasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			hasBeenModified = workloadModifier.ModifyCronJob(workload)
			Expect(hasBeenModified).To(BeFalse())
			VerifyModifiedCronJob(workload, BasicInstrumentedPodSpecExpectations())
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("daemon set instrumentation needs to be idempotent", func() {
			workload := BasicDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			hasBeenModified := workloadModifier.ModifyDaemonSet(workload)
			Expect(hasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			hasBeenModified = workloadModifier.ModifyDaemonSet(workload)
			Expect(hasBeenModified).To(BeFalse())
			VerifyModifiedDaemonSet(workload, BasicInstrumentedPodSpecExpectations())
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("deployment instrumentation needs to be idempotent", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			hasBeenModified := workloadModifier.ModifyDeployment(workload)
			Expect(hasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			hasBeenModified = workloadModifier.ModifyDeployment(workload)
			Expect(hasBeenModified).To(BeFalse())
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations())
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("job instrumentation needs to be idempotent", func() {
			workload := BasicJob(TestNamespaceName, JobNamePrefix)
			hasBeenModified := workloadModifier.ModifyJob(workload)
			Expect(hasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			hasBeenModified = workloadModifier.ModifyJob(workload)
			Expect(hasBeenModified).To(BeFalse())
			VerifyModifiedJob(workload, BasicInstrumentedPodSpecExpectations())
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("ownerless pod instrumentation needs to be idempotent", func() {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			hasBeenModified := workloadModifier.ModifyPod(workload)
			Expect(hasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			hasBeenModified = workloadModifier.ModifyPod(workload)
			Expect(hasBeenModified).To(BeFalse())
			VerifyModifiedPod(workload, BasicInstrumentedPodSpecExpectations())
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("ownerless replica set instrumentation needs to be idempotent", func() {
			workload := BasicReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			hasBeenModified := workloadModifier.ModifyReplicaSet(workload)
			Expect(hasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			hasBeenModified = workloadModifier.ModifyReplicaSet(workload)
			Expect(hasBeenModified).To(BeFalse())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations())
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("stateful set instrumentation needs to be idempotent", func() {
			workload := BasicStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			hasBeenModified := workloadModifier.ModifyStatefulSet(workload)
			Expect(hasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			hasBeenModified = workloadModifier.ModifyStatefulSet(workload)
			Expect(hasBeenModified).To(BeFalse())
			VerifyModifiedStatefulSet(workload, BasicInstrumentedPodSpecExpectations())
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})
	})

	Describe("when updating instrumentation from 0.5.1 to 0.6.0", func() {
		It("should remove the old --require from NODE_OPTIONS (when it is the only content of NODE_OPTIONS)", func() {
			workload := InstrumentedDeployment(TestNamespaceName, DeploymentNamePrefix)
			Expect(workload.Spec.Template.Spec.Containers[0].Env[0].Name).To(Equal("NODE_OPTIONS"))
			workload.Spec.Template.Spec.Containers[0].Env[0].Value = "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
			hasBeenModified := workloadModifier.ModifyDeployment(workload)
			Expect(hasBeenModified).To(BeTrue())
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations())
		})

		It("should remove the old --require from NODE_OPTIONS (when it is at the start of NODE_OPTIONS)", func() {
			workload := InstrumentedDeployment(TestNamespaceName, DeploymentNamePrefix)
			Expect(workload.Spec.Template.Spec.Containers[0].Env[0].Name).To(Equal("NODE_OPTIONS"))
			workload.Spec.Template.Spec.Containers[0].Env[0].Value = "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --require /something/else"
			hasBeenModified := workloadModifier.ModifyDeployment(workload)
			Expect(hasBeenModified).To(BeTrue())
			expectations := BasicInstrumentedPodSpecExpectations()
			expectations.Containers[0].NodeOptionsValue = "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --require /something/else"
			VerifyModifiedDeployment(workload, expectations)
		})

		It("should remove the old --require from NODE_OPTIONS (when it is at the end of NODE_OPTIONS)", func() {
			workload := InstrumentedDeployment(TestNamespaceName, DeploymentNamePrefix)
			Expect(workload.Spec.Template.Spec.Containers[0].Env[0].Name).To(Equal("NODE_OPTIONS"))
			workload.Spec.Template.Spec.Containers[0].Env[0].Value = "--require /something/else --require /opt/dash0/instrumentation/node.js/node_modules/@dash0hq/opentelemetry"
			hasBeenModified := workloadModifier.ModifyDeployment(workload)
			Expect(hasBeenModified).To(BeTrue())
			expectations := BasicInstrumentedPodSpecExpectations()
			expectations.Containers[0].NodeOptionsValue = "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --require /something/else"
			VerifyModifiedDeployment(workload, expectations)
		})

		It("should remove the old --require from NODE_OPTIONS (when it is in the middle of NODE_OPTIONS)", func() {
			workload := InstrumentedDeployment(TestNamespaceName, DeploymentNamePrefix)
			Expect(workload.Spec.Template.Spec.Containers[0].Env[0].Name).To(Equal("NODE_OPTIONS"))
			workload.Spec.Template.Spec.Containers[0].Env[0].Value = "--require /something/else --require /opt/dash0/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --require /another/thing"
			hasBeenModified := workloadModifier.ModifyDeployment(workload)
			Expect(hasBeenModified).To(BeTrue())
			expectations := BasicInstrumentedPodSpecExpectations()
			expectations.Containers[0].NodeOptionsValue = "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --require /something/else --require /another/thing"
			VerifyModifiedDeployment(workload, expectations)
		})
	})

	Describe("when reverting workloads", func() {
		It("should remove Dash0 from an instrumented cron job", func() {
			workload := InstrumentedCronJob(TestNamespaceName, CronJobNamePrefix)
			hasBeenModified := workloadModifier.RevertCronJob(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyUnmodifiedCronJob(workload)
		})

		It("should remove Dash0 from an instrumented daemon set", func() {
			workload := InstrumentedDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			hasBeenModified := workloadModifier.RevertDaemonSet(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyUnmodifiedDaemonSet(workload)
		})

		It("should remove Dash0 from an instrumented deployment", func() {
			workload := InstrumentedDeployment(TestNamespaceName, DeploymentNamePrefix)
			hasBeenModified := workloadModifier.RevertDeployment(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyUnmodifiedDeployment(workload)
		})

		It("should remove Dash0 from a instrumented deployment that has multiple containers, and already has volumes and init containers previous to being instrumented", func() {
			workload := InstrumentedDeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentNamePrefix)
			hasBeenModified := workloadModifier.RevertDeployment(workload)

			Expect(hasBeenModified).To(BeTrue())
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

		It("should remove Dash0 from an instrumented ownerless replica set", func() {
			workload := InstrumentedReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			hasBeenModified := workloadModifier.RevertReplicaSet(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should not remove Dash0 from a replica set that is owned by a deployment", func() {
			workload := InstrumentedReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetNamePrefix)
			hasBeenModified := workloadModifier.RevertReplicaSet(workload)

			Expect(hasBeenModified).To(BeFalse())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations())
		})

		It("should remove Dash0 from an instrumented stateful set", func() {
			workload := InstrumentedStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			hasBeenModified := workloadModifier.RevertStatefulSet(workload)

			Expect(hasBeenModified).To(BeTrue())
			VerifyUnmodifiedStatefulSet(workload)
		})
	})
})
