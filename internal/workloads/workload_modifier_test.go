// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type envVarExpectation struct {
	value     string
	valueFrom string
}

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
						Dash0ServiceNameEnvVarValueFrom:             true,
						Dash0ServiceNamespaceEnvVarIdx:              10,
						Dash0ServiceNamespaceEnvVarValueFrom:        true,
						Dash0ServiceVersionEnvVarIdx:                11,
						Dash0ServiceVersionEnvVarValueFrom:          true,
						Dash0ResourceAttributesEnvVarIdx:            12,
						Dash0ResourceAttributesEnvVarKeyValuePairs: []string{
							"workload.only.1=workload-value-1",
							"workload.only.2=workload-value-2",
							"pod.and.workload=pod-value",
							"pod.only.1=pod-value-1",
							"pod.only.2=pod-value-2",
						},
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
						Dash0ServiceNameEnvVarValueFrom:             true,
						Dash0ServiceNamespaceEnvVarIdx:              11,
						Dash0ServiceNamespaceEnvVarValueFrom:        true,
						Dash0ServiceVersionEnvVarIdx:                12,
						Dash0ServiceVersionEnvVarValueFrom:          true,
						Dash0ResourceAttributesEnvVarIdx:            13,
						Dash0ResourceAttributesEnvVarKeyValuePairs: []string{
							"workload.only.1=workload-value-1",
							"workload.only.2=workload-value-2",
							"pod.and.workload=pod-value",
							"pod.only.1=pod-value-1",
							"pod.only.2=pod-value-2",
						},
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

	Describe("individual modification functions", func() {
		ctx := context.Background()
		logger := log.FromContext(ctx)
		workloadModifier := NewResourceModifier(instrumentationMetadata, &logger)

		type objectMetaResourceAttributesTest struct {
			workloadLabels                  map[string]string
			workloadAnnotations             map[string]string
			podLabels                       map[string]string
			podAnnotations                  map[string]string
			expectedEnvVars                 map[string]*envVarExpectation
			expectedResourceAttributeEnvVar []string
		}

		DescribeTable("should derive resource attributes from labels and annotations",
			func(testConfig objectMetaResourceAttributesTest) {
				workloadMeta := metav1.ObjectMeta{}
				if testConfig.workloadLabels != nil {
					workloadMeta.Labels = testConfig.workloadLabels
				}
				if testConfig.workloadAnnotations != nil {
					workloadMeta.Annotations = testConfig.workloadAnnotations
				}
				podMeta := metav1.ObjectMeta{}
				if testConfig.podLabels != nil {
					podMeta.Labels = testConfig.podLabels
				}
				if testConfig.podAnnotations != nil {
					podMeta.Annotations = testConfig.podAnnotations
				}

				container := &corev1.Container{}
				workloadModifier.addEnvironmentVariables(
					container,
					&workloadMeta,
					&podMeta,
					logger,
				)

				envVars := container.Env
				verifyEnvVar(testConfig.expectedEnvVars, envVars, envVarDash0ServiceName)
				verifyEnvVar(testConfig.expectedEnvVars, envVars, envVarDash0ServiceNamespace)
				verifyEnvVar(testConfig.expectedEnvVars, envVars, envVarDash0ServiceVersion)

				expectedResourceAttributes := testConfig.expectedResourceAttributeEnvVar
				actualResourceAttributes := FindEnvVarByName(envVars, envVarDash0ResourceAttributes)
				if expectedResourceAttributes != nil {
					Expect(actualResourceAttributes).ToNot(BeNil())
					actualKeyValuePairs := strings.Split(actualResourceAttributes.Value, ",")
					Expect(actualKeyValuePairs).To(HaveLen(len(expectedResourceAttributes)))
					for _, expectedKeyValuePair := range expectedResourceAttributes {
						Expect(actualKeyValuePairs).To(ContainElement(expectedKeyValuePair))
					}
				} else {
					Expect(actualResourceAttributes).To(BeNil())
				}
			},
			Entry("should not add env vars if there is no metadata", objectMetaResourceAttributesTest{
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      nil,
					envVarDash0ServiceNamespace: nil,
					envVarDash0ServiceVersion:   nil,
				},
			}),
			Entry("should not add env vars if metadata is empty", objectMetaResourceAttributesTest{
				workloadLabels:      map[string]string{},
				workloadAnnotations: map[string]string{},
				podLabels:           map[string]string{},
				podAnnotations:      map[string]string{},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      nil,
					envVarDash0ServiceNamespace: nil,
					envVarDash0ServiceVersion:   nil,
				},
			}),
			Entry("should not add env vars if metadata is unrelated", objectMetaResourceAttributesTest{
				workloadLabels:      map[string]string{"a": "b"},
				workloadAnnotations: map[string]string{"a": "b"},
				podLabels:           map[string]string{"a": "b"},
				podAnnotations:      map[string]string{"a": "b"},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      nil,
					envVarDash0ServiceNamespace: nil,
					envVarDash0ServiceVersion:   nil,
				},
			}),
			Entry("should derive env vars from workload labels", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "workload-name",
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      {value: "workload-name"},
					envVarDash0ServiceNamespace: {value: "workload-part-of"},
					envVarDash0ServiceVersion:   {value: "workload-version"},
				},
			}),
			Entry("should ignore workload labels if name is not set", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      nil,
					envVarDash0ServiceNamespace: nil,
					envVarDash0ServiceVersion:   nil,
				},
			}),
			Entry("should derive env vars from pod labels", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "pod-name",
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      {valueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarDash0ServiceNamespace: {valueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarDash0ServiceVersion:   {valueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
				},
			}),
			Entry("should ignore pod labels if name is not set", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      nil,
					envVarDash0ServiceNamespace: nil,
					envVarDash0ServiceVersion:   nil,
				},
			}),
			Entry("pod labels should override workload labels", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "workload-name",
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				podLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "pod-name",
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      {valueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarDash0ServiceNamespace: {valueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarDash0ServiceVersion:   {valueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
				},
			}),
			Entry("should derive resource attributes from workload annotations", objectMetaResourceAttributesTest{
				workloadAnnotations: map[string]string{
					"resource.opentelemetry.io/workload.ra.1": "workload-value-1",
					"resource.opentelemetry.io/workload.ra.2": "workload-value-2",
				},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      nil,
					envVarDash0ServiceNamespace: nil,
					envVarDash0ServiceVersion:   nil,
				},
				expectedResourceAttributeEnvVar: []string{
					"workload.ra.1=workload-value-1",
					"workload.ra.2=workload-value-2",
				},
			}),
			Entry("should derive resource attributes from pod annotations", objectMetaResourceAttributesTest{
				podAnnotations: map[string]string{
					"resource.opentelemetry.io/pod.ra.1": "pod-value-1",
					"resource.opentelemetry.io/pod.ra.2": "pod-value-2",
				},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      nil,
					envVarDash0ServiceNamespace: nil,
					envVarDash0ServiceVersion:   nil,
				},
				expectedResourceAttributeEnvVar: []string{"pod.ra.1=pod-value-1", "pod.ra.2=pod-value-2"},
			}),
			Entry("pod annotaions override workload annotations", objectMetaResourceAttributesTest{
				workloadAnnotations: map[string]string{
					"resource.opentelemetry.io/workload.ra.1":  "workload-value-1",
					"resource.opentelemetry.io/workload.ra.2":  "workload-value-2",
					"resource.opentelemetry.io/occurs-in-both": "workload-value",
				},
				podAnnotations: map[string]string{
					"resource.opentelemetry.io/pod.ra.1":       "pod-value-1",
					"resource.opentelemetry.io/pod.ra.2":       "pod-value-2",
					"resource.opentelemetry.io/occurs-in-both": "pod-value",
				},
				expectedEnvVars: map[string]*envVarExpectation{
					envVarDash0ServiceName:      nil,
					envVarDash0ServiceNamespace: nil,
					envVarDash0ServiceVersion:   nil,
				},
				expectedResourceAttributeEnvVar: []string{
					"workload.ra.1=workload-value-1",
					"workload.ra.2=workload-value-2",
					"pod.ra.1=pod-value-1",
					"pod.ra.2=pod-value-2",
					"occurs-in-both=pod-value",
				},
			}),
		)

		type safeToEvictLocalVolumesAnnotationTest struct {
			input    map[string]string
			expected map[string]string
		}

		DescribeTable("should add safe-to-evict local volumes annotation to pod annotations",
			func(testConfig safeToEvictLocalVolumesAnnotationTest) {
				podMeta := metav1.ObjectMeta{
					Annotations: testConfig.input,
				}
				workloadModifier.addSafeToEvictLocalVolumesAnnotation(&podMeta)
				Expect(podMeta.Annotations).To(HaveLen(len(testConfig.expected)))
				for key, expectedValue := range testConfig.expected {
					Expect(podMeta.Annotations[key]).To(Equal(expectedValue))
				}
			},
			Entry("should add annotation if annotations are nil", safeToEvictLocalVolumesAnnotationTest{
				input: nil,
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": dash0VolumeName,
				},
			}),
			Entry("should add annotation zero annotations are present", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": dash0VolumeName,
				},
			}),
			Entry("should leave other annotations alone", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"a": "b",
					"c": "d",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": dash0VolumeName,
					"a": "b",
					"c": "d",
				},
			}),
			Entry("should add volume name to empty annotation list", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": dash0VolumeName,
					"other": "annotation",
				},
			}),
			Entry("should add volume name to empty annotation list with whitespace", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "  ",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": dash0VolumeName,
					"other": "annotation",
				},
			}),
			Entry("should add Dash0 volume name to non-empty annotation list", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-one, volume-two",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-one,volume-two," + dash0VolumeName,
					"other": "annotation",
				},
			}),
			Entry("should add Dash0 volume name to non-empty annotation list with whitespace", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "  volume-one , , volume-two  ",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-one,volume-two," + dash0VolumeName,
					"other": "annotation",
				},
			}),
			Entry("should leave annotation unchanged if it already has the Dash0 volume name", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "  volume-one ,  dash0-instrumentation , volume-two  ",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "  volume-one ,  dash0-instrumentation , volume-two  ",
					"other": "annotation",
				},
			}),
		)

		DescribeTable("should remove safe-to-evict local volume annotation from pod annotations",
			func(testConfig safeToEvictLocalVolumesAnnotationTest) {
				podMeta := metav1.ObjectMeta{
					Annotations: testConfig.input,
				}
				workloadModifier.removeSafeToEvictLocalVolumesAnnotation(&podMeta)
				if testConfig.expected == nil {
					Expect(podMeta.Annotations).To(BeNil())
					return
				}
				Expect(podMeta.Annotations).To(HaveLen(len(testConfig.expected)))
				for key, expectedValue := range testConfig.expected {
					Expect(podMeta.Annotations[key]).To(Equal(expectedValue))
				}
			},
			Entry("should leave nil annotations unchanged", safeToEvictLocalVolumesAnnotationTest{
				input:    nil,
				expected: nil,
			}),
			Entry("should leave empty annotations unchanged", safeToEvictLocalVolumesAnnotationTest{
				input:    map[string]string{},
				expected: map[string]string{},
			}),
			Entry("should leave other annotations alone", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"a": "b",
					"c": "d",
				},
				expected: map[string]string{
					"a": "b",
					"c": "d",
				},
			}),
			Entry("should remove Dash0 volume name if it is the only element", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": dash0VolumeName,
					"other": "annotation",
				},
				expected: map[string]string{
					"other": "annotation",
				},
			}),
			Entry("should remove Dash0 volume name if it is the only element, but has surrounding whitespace", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "  dash0-instrumentation  ",
					"other": "annotation",
				},
				expected: map[string]string{
					"other": "annotation",
				},
			}),
			Entry("should do nothing if the annotation only lists other volumes", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-one, volume-two",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-one, volume-two",
					"other": "annotation",
				},
			}),
			Entry("should remove the Dash0 volume name from a list of volumes, at the start", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "dash0-instrumentation,volume-1,volume-2",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-1,volume-2",
					"other": "annotation",
				},
			}),
			Entry("should remove the Dash0 volume name from a list of volumes, in the middle", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-1,dash0-instrumentation,volume-2",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-1,volume-2",
					"other": "annotation",
				},
			}),
			Entry("should remove the Dash0 volume name from a list of volumes, at the end", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-1,volume-2,dash0-instrumentation",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-1,volume-2",
					"other": "annotation",
				},
			}),
			Entry("should remove the Dash0 volume name from a list of volumes with whitepsace", safeToEvictLocalVolumesAnnotationTest{
				input: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": " volume-1 , dash0-instrumentation , volume-2",
					"other": "annotation",
				},
				expected: map[string]string{
					"cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes": "volume-1,volume-2",
					"other": "annotation",
				},
			}),
		)
	})
})

func verifyEnvVar(expectedEnvVars map[string]*envVarExpectation, envVars []corev1.EnvVar, name string) {
	expected := expectedEnvVars[name]
	actual := FindEnvVarByName(envVars, name)
	if expected != nil {
		Expect(actual).ToNot(BeNil())
		if expected.value != "" {
			Expect(actual.Value).To(Equal(expected.value))
			Expect(actual.ValueFrom).To(BeNil())
		} else if expected.valueFrom != "" {
			Expect(actual.ValueFrom).ToNot(BeNil())
			Expect(actual.ValueFrom.FieldRef).ToNot(BeNil())
			Expect(actual.ValueFrom.FieldRef.FieldPath).To(Equal(expected.valueFrom))
			Expect(actual.Value).To(BeEmpty())
		} else {
			Fail("inconsistent env var expectation")
		}
	} else {
		Expect(actual).To(BeNil())
	}
}
