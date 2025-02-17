// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"context"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	testActor = "actor"
)

// Maintenance note: There is some overlap of test cases between this file and dash0_webhook_test.go. This is
// intentional. However, this test should be used for more fine-grained test cases, while dash0_webhook_test.go should
// be used to verify external effects (recording events etc.) that cannot be covered in this test.

var (
	instrumentationMetadata = util.InstrumentationMetadata{
		Images:               TestImages,
		OTelCollectorBaseUrl: OTelCollectorBaseUrlTest,
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
			modificationResult := workloadModifier.ModifyCronJob(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedCronJob(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should instrument a basic daemon set", func() {
			workload := BasicDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			modificationResult := workloadModifier.ModifyDaemonSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedDaemonSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should add Dash0 to a basic deployment", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifier.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should instrument a deployment that has multiple containers, and already has volumes and init containers", func() {
			workload := DeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifier.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedDeployment(workload, PodSpecExpectations{
				Volumes:               3,
				Dash0VolumeIdx:        2,
				InitContainers:        3,
				Dash0InitContainerIdx: 2,
				Containers: []ContainerExpectations{
					{
						VolumeMounts:                                2,
						Dash0VolumeMountIdx:                         1,
						EnvVars:                                     13,
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
						Dash0ServiceNameEnvVarIdx:                   9,
						Dash0ServiceNamespaceEnvVarIdx:              10,
						Dash0ServiceVersionEnvVarIdx:                11,
						Dash0ResourceAttributesEnvVarIdx:            12,
					},
					{
						VolumeMounts:                                3,
						Dash0VolumeMountIdx:                         2,
						EnvVars:                                     14,
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
						Dash0ServiceNameEnvVarIdx:                   10,
						Dash0ServiceNamespaceEnvVarIdx:              11,
						Dash0ServiceVersionEnvVarIdx:                12,
						Dash0ResourceAttributesEnvVarIdx:            13,
					},
				},
			},
				IgnoreManagedFields,
			)
		})

		It("should update existing Dash0 artifacts in a deployment", func() {
			workload := DeploymentWithExistingDash0Artifacts(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifier.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
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
						Dash0ServiceNameEnvVarIdx:                   -1,
						Dash0ServiceNamespaceEnvVarIdx:              -1,
						Dash0ServiceVersionEnvVarIdx:                -1,
						Dash0ResourceAttributesEnvVarIdx:            -1,
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
						Dash0ServiceNameEnvVarIdx:                   -1,
						Dash0ServiceNamespaceEnvVarIdx:              -1,
						Dash0ServiceVersionEnvVarIdx:                -1,
						Dash0ResourceAttributesEnvVarIdx:            -1,
					},
				},
			},
				IgnoreManagedFields,
			)
		})

		It("should instrument a basic job", func() {
			workload := BasicJob(TestNamespaceName, JobNamePrefix)
			modificationResult := workloadModifier.ModifyJob(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedJob(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should instrument a basic ownerless pod", func() {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			modificationResult := workloadModifier.ModifyPod(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedPod(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should instrument a basic pod with an unrecognized owner", func() {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			workload.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "core.strimzi.io/v1beta2",
				Kind:       "StrimziPodSet",
			}}
			modificationResult := workloadModifier.ModifyPod(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedPod(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		DescribeTable("should not instrument a basic pod owned by another higher level workload", func(owner metav1.TypeMeta) {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			workload.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: owner.APIVersion,
				Kind:       owner.Kind,
			}}
			modificationResult := workloadModifier.ModifyPod(workload)

			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"The workload is part of a higher order workload that will be instrumented by the webhook, no modification by the actor is necessary."))
			VerifyUnmodifiedPod(workload)
		}, Entry("owned by a DaemonSet", util.K8sTypeMetaDaemonSet),
			Entry("owned by a ReplicaSet", util.K8sTypeMetaReplicaSet),
			Entry("owned by a StatefulSet", util.K8sTypeMetaStatefulSet),
			Entry("owned by a CronJob", util.K8sTypeMetaCronJob),
			Entry("owned by a Job", util.K8sTypeMetaJob),
		)

		It("should instrument a basic ownerless replica set", func() {
			workload := BasicReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			modificationResult := workloadModifier.ModifyReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should instrument a basic replica set owned by an unrecognized type", func() {
			workload := BasicReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			workload.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "something.com/v2alpha47",
				Kind:       "SomeKind",
			}}
			modificationResult := workloadModifier.ModifyReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should not instrument a basic replica set that is owned by a deployment", func() {
			workload := ReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetNamePrefix)
			modificationResult := workloadModifier.ModifyReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"The workload is part of a higher order workload that will be instrumented by the webhook, no modification by the actor is necessary."))
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should instrument a basic stateful set", func() {
			workload := BasicStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			modificationResult := workloadModifier.ModifyStatefulSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedStatefulSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})
	})

	Describe("when instrumenting workloads multiple times (instrumentation needs to be idempotent)", func() {
		It("cron job instrumentation needs to be idempotent", func() {
			workload := BasicCronJob(TestNamespaceName, CronJobNamePrefix)
			modificationResult := workloadModifier.ModifyCronJob(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifier.ModifyCronJob(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedCronJob(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("daemon set instrumentation needs to be idempotent", func() {
			workload := BasicDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			modificationResult := workloadModifier.ModifyDaemonSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifier.ModifyDaemonSet(workload)
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedDaemonSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("deployment instrumentation needs to be idempotent", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifier.ModifyDeployment(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifier.ModifyDeployment(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("job instrumentation needs to be idempotent", func() {
			workload := BasicJob(TestNamespaceName, JobNamePrefix)
			modificationResult := workloadModifier.ModifyJob(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifier.ModifyJob(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedJob(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("ownerless pod instrumentation needs to be idempotent", func() {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			modificationResult := workloadModifier.ModifyPod(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifier.ModifyPod(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedPod(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("ownerless replica set instrumentation needs to be idempotent", func() {
			workload := BasicReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			modificationResult := workloadModifier.ModifyReplicaSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifier.ModifyReplicaSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("stateful set instrumentation needs to be idempotent", func() {
			workload := BasicStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			modificationResult := workloadModifier.ModifyStatefulSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifier.ModifyStatefulSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedStatefulSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})
	})

	Describe("when reverting workloads", func() {
		It("should remove Dash0 from an instrumented cron job", func() {
			workload := InstrumentedCronJob(TestNamespaceName, CronJobNamePrefix)
			modificationResult := workloadModifier.RevertCronJob(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedCronJob(workload)
		})

		It("should remove Dash0 from an instrumented daemon set", func() {
			workload := InstrumentedDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			modificationResult := workloadModifier.RevertDaemonSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedDaemonSet(workload)
		})

		It("should remove Dash0 from an instrumented deployment", func() {
			workload := InstrumentedDeployment(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifier.RevertDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedDeployment(workload)
		})

		It("should remove Dash0 from a instrumented deployment that has multiple containers, and already has volumes and init containers previous to being instrumented", func() {
			workload := InstrumentedDeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifier.RevertDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyRevertedDeployment(workload, PodSpecExpectations{
				Volumes:               2,
				Dash0VolumeIdx:        -1,
				InitContainers:        2,
				Dash0InitContainerIdx: -1,
				Containers: []ContainerExpectations{
					{
						VolumeMounts:                      1,
						Dash0VolumeMountIdx:               -1,
						EnvVars:                           1,
						LdPreloadEnvVarIdx:                -1,
						Dash0NodeIpIdx:                    -1,
						Dash0CollectorBaseUrlEnvVarIdx:    -1,
						OtelExporterOtlpEndpointEnvVarIdx: -1,
						Dash0NamespaceNameEnvVarIdx:       -1,
						Dash0PodNameEnvVarIdx:             -1,
						Dash0PodUidEnvVarIdx:              -1,
						Dash0ContainerNameEnvVarIdx:       -1,
						Dash0ServiceNameEnvVarIdx:         -1,
						Dash0ServiceNamespaceEnvVarIdx:    -1,
						Dash0ServiceVersionEnvVarIdx:      -1,
						Dash0ResourceAttributesEnvVarIdx:  -1,
					},
					{
						VolumeMounts:                      2,
						Dash0VolumeMountIdx:               -1,
						EnvVars:                           2,
						LdPreloadEnvVarIdx:                -1,
						Dash0NodeIpIdx:                    -1,
						Dash0CollectorBaseUrlEnvVarIdx:    -1,
						OtelExporterOtlpEndpointEnvVarIdx: -1,
						Dash0NamespaceNameEnvVarIdx:       -1,
						Dash0PodNameEnvVarIdx:             -1,
						Dash0PodUidEnvVarIdx:              -1,
						Dash0ContainerNameEnvVarIdx:       -1,
						Dash0ServiceNameEnvVarIdx:         -1,
						Dash0ServiceNamespaceEnvVarIdx:    -1,
						Dash0ServiceVersionEnvVarIdx:      -1,
						Dash0ResourceAttributesEnvVarIdx:  -1,
					},
				},
			})
		})

		It("should remove Dash0 from an instrumented ownerless replica set", func() {
			workload := InstrumentedReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			modificationResult := workloadModifier.RevertReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should remove Dash0 from a replica set owned by an unrecognized type", func() {
			workload := InstrumentedReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			workload.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "something.com/v2alpha47",
				Kind:       "SomeKind",
			}}
			modificationResult := workloadModifier.RevertReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should not remove Dash0 from a replica set that is owned by a deployment", func() {
			workload := InstrumentedReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetNamePrefix)
			modificationResult := workloadModifier.RevertReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"The workload is part of a higher order workload that will be instrumented by the webhook, no modification by the actor is necessary."))
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should remove Dash0 from an instrumented stateful set", func() {
			workload := InstrumentedStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			modificationResult := workloadModifier.RevertStatefulSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedStatefulSet(workload)
		})
	})
})
