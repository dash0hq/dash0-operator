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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	testActor = util.WorkloadModifierActor("actor")
)

// Maintenance note: There is some overlap of test cases between this file and dash0_webhook_test.go. This is
// intentional. However, this test should be used for more fine-grained test cases, while dash0_webhook_test.go should
// be used to verify external effects (recording events etc.) that cannot be covered in this test.

var (
	clusterInstrumentationConfig = util.NewClusterInstrumentationConfig(
		TestImages,
		OTelCollectorNodeLocalBaseUrlTest,
		util.ExtraConfigDefaults,
		nil,
		false,
	)
)

var _ = Describe("Dash0 Workload Modification", func() {

	ctx := context.Background()
	logger := log.FromContext(ctx)
	workloadModifier := NewResourceModifier(
		clusterInstrumentationConfig,
		DefaultNamespaceInstrumentationConfig,
		testActor,
		&logger,
	)

	type envVarModificationTest struct {
		envVars      []corev1.EnvVar
		expectations map[string]*EnvVarExpectation
	}

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

		It("should use the collector service URL instead of the node-local endpoint if requested", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			modificationResult :=
				NewResourceModifier(
					util.NewClusterInstrumentationConfig(
						TestImages,
						OTelCollectorServiceBaseUrlTest,
						util.ExtraConfigDefaults,
						nil,
						false,
					),
					DefaultNamespaceInstrumentationConfig,
					testActor,
					&logger,
				).ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			envVars := workload.Spec.Template.Spec.Containers[0].Env
			dash0CollectorBaseUrlName := FindEnvVarByName(envVars, envVarDash0CollectorBaseUrlName)
			Expect(dash0CollectorBaseUrlName.Value).To(Equal(OTelCollectorServiceBaseUrlTest))
			otelExporterOtlpEndpointName := FindEnvVarByName(envVars, envVarOtelExporterOtlpEndpointName)
			Expect(otelExporterOtlpEndpointName.Value).To(Equal(OTelCollectorServiceBaseUrlTest))
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
						ContainerName:       "test-container-0",
						VolumeMounts:        1,
						Dash0VolumeMountIdx: -1,
						EnvVars: map[string]*EnvVarExpectation{
							"TEST0": {
								Value: "value",
							},
							"LD_PRELOAD": {
								Value: "/some/preload.so /another/preload.so",
							},
						},
					},
					{
						ContainerName:       "test-container-1",
						VolumeMounts:        2,
						Dash0VolumeMountIdx: -1,
						EnvVars: map[string]*EnvVarExpectation{
							"TEST0": {
								Value: "value",
							},
							"TEST1": {
								ValueFrom: "metadata.namespace",
							},
						},
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
		workloadModifier := NewResourceModifier(
			clusterInstrumentationConfig,
			DefaultNamespaceInstrumentationConfig,
			testActor,
			&logger,
		)

		type addLdPreloadTest struct {
			value       string
			valueFrom   string
			expectation EnvVarExpectation
		}

		DescribeTable("should add the Dash0 injector to LD_PRELOAD",
			func(testConfig addLdPreloadTest) {
				container := &corev1.Container{Env: []corev1.EnvVar{}}
				if testConfig.value != "" {
					container.Env = append(
						container.Env,
						corev1.EnvVar{Name: "LD_PRELOAD", Value: testConfig.value},
					)
				} else if testConfig.valueFrom != "" {
					container.Env = append(
						container.Env,
						corev1.EnvVar{
							Name: "LD_PRELOAD",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: testConfig.valueFrom,
								},
							},
						},
					)
				}

				workloadModifier.handleLdPreloadEnvVar(container, logger)

				VerifyEnvVar(testConfig.expectation, container.Env, envVarLdPreloadName, "")
			},
			Entry("should add LD_PRELOAD with only the Dash0 injector if it does not exist", addLdPreloadTest{
				expectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should add LD_PRELOAD with only the Dash0 injector the env var exists but is empty", addLdPreloadTest{
				value: "",
				expectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should add LD_PRELOAD with only the Dash0 injector the env var exists but is only whitespace", addLdPreloadTest{
				value: "   ",
				expectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should do nothing if LD_PRELOAD already has the Dash0 injector as its only element", addLdPreloadTest{
				value: envVarLdPreloadValue,
				expectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should do nothing if LD_PRELOAD already has the Dash0 injector as its first element", addLdPreloadTest{
				value: envVarLdPreloadValue + " one.so two.so",
				expectation: EnvVarExpectation{
					Value: envVarLdPreloadValue + " one.so two.so",
				},
			}),
			Entry("should do nothing if LD_PRELOAD already has the Dash0 injector in the middle", addLdPreloadTest{
				value: "one.so " + envVarLdPreloadValue + " two.so",
				expectation: EnvVarExpectation{
					Value: "one.so " + envVarLdPreloadValue + " two.so",
				},
			}),
			Entry("should do nothing if LD_PRELOAD already has the Dash0 injector at the end", addLdPreloadTest{
				value: "one.so two.so " + envVarLdPreloadValue,
				expectation: EnvVarExpectation{
					Value: "one.so two.so " + envVarLdPreloadValue,
				},
			}),
			Entry("should prepend the Dash0 injector to LD_PRELOAD if there are other libraries", addLdPreloadTest{
				value: "one.so",
				expectation: EnvVarExpectation{
					Value: envVarLdPreloadValue + " one.so",
				},
			}),
			Entry("should do nothing if LD_PRELOAD exists with ValueFrom", addLdPreloadTest{
				valueFrom: "whatever",
				expectation: EnvVarExpectation{
					ValueFrom: "whatever",
				},
			}),
		)

		DescribeTable("should remove the Dash0 injector from LD_PRELOAD",
			func(testConfig envVarModificationTest) {
				container := &corev1.Container{Env: testConfig.envVars}
				workloadModifier.removeLdPreload(container)
				Expect(container.Env).To(HaveLen(len(testConfig.expectations)))
				VerifyEnvVarsFromMap(
					testConfig.expectations,
					container.Env,
				)
			},
			Entry("should do nothing if there are no env vars", envVarModificationTest{
				envVars:      nil,
				expectations: map[string]*EnvVarExpectation{},
			}),
			Entry("should do nothing if there is no LD_PRELOAD", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should do nothing if LD_PRELOAD does not list the Dash0 injector", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so  two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					envVarLdPreloadName: {Value: "one.so  two.so"},
					"OTHER":             {Value: "value"},
				},
			}),
			Entry("should do nothing if LD_PRELOAD uses ValueFrom", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{
						Name: envVarLdPreloadName,
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "whatever",
							},
						},
					},
					{Name: "OTHER", Value: "value"},
				},
				expectations: map[string]*EnvVarExpectation{
					envVarLdPreloadName: {ValueFrom: "whatever"},
					"OTHER":             {Value: "value"},
				},
			}),
			Entry("should remove LD_PRELOAD if the Dash0 injector is the only library", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: envVarLdPreloadValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should remove LD_PRELOAD if the Dash0 injector is the only library and has surrounding whitespace", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "  " + envVarLdPreloadValue + "   "},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should remove the Dash0 injector from LD_PRELOAD at the start (space separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: envVarLdPreloadValue + " one.so two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so"},
				},
			}),
			Entry("should remove the Dash0 injector from LD_PRELOAD in the middle (space separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so " + envVarLdPreloadValue + " two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so"},
				},
			}),
			Entry("should remove the Dash0 injector from LD_PRELOAD with extraneous whitespace (space separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "  one.so    " + envVarLdPreloadValue + "   two.so  "},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so"},
				},
			}),
			Entry("should remove the Dash0 injector from LD_PRELOAD at the end (space separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so two.so " + envVarLdPreloadValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so"},
				},
			}),
			Entry("should remove the Dash0 injector from LD_PRELOAD at the start (colon separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: envVarLdPreloadValue + ":one.so:two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so:two.so"},
				},
			}),
			Entry("should remove the Dash0 injector from LD_PRELOAD in the middle (colon separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so:" + envVarLdPreloadValue + ":two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so:two.so"},
				},
			}),
			Entry("should remove the Dash0 injector from LD_PRELOAD with extraneous whitespace (colon separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "  one.so  :  " + envVarLdPreloadValue + " :  two.so  "},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so:two.so"},
				},
			}),
			Entry("should remove the Dash0 injector from LD_PRELOAD at the end (colon separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so:two.so:" + envVarLdPreloadValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so:two.so"},
				},
			}),
		)

		DescribeTable("should clean up legacy env vars",
			func(testConfig envVarModificationTest) {
				container := &corev1.Container{Env: testConfig.envVars}
				workloadModifier.removeLegacyEnvironmentVariables(container)
				Expect(container.Env).To(HaveLen(len(testConfig.expectations)))
				VerifyEnvVarsFromMap(
					testConfig.expectations,
					container.Env,
				)
			},
			Entry("should do nothing if there are no env vars", envVarModificationTest{
				envVars:      nil,
				expectations: map[string]*EnvVarExpectation{},
			}),
			Entry("should do nothing if there is no NODE_OPTIONS", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should do nothing if NODE_OPTIONS does not list the legacy Dash0 Node.js OTel distribution", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: legacyEnvVarNodeOptionsName, Value: " --abort-on-uncaught-exception  --enable-fips "},
				},
				expectations: map[string]*EnvVarExpectation{
					legacyEnvVarNodeOptionsName: {Value: " --abort-on-uncaught-exception  --enable-fips "},
					"OTHER":                     {Value: "value"},
				},
			}),
			Entry("should do nothing if NODE_OPTIONS uses ValueFrom", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{
						Name: legacyEnvVarNodeOptionsName,
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "whatever",
							},
						},
					},
					{Name: "OTHER", Value: "value"},
				},
				expectations: map[string]*EnvVarExpectation{
					legacyEnvVarNodeOptionsName: {ValueFrom: "whatever"},
					"OTHER":                     {Value: "value"},
				},
			}),
			Entry("should remove NODE_OPTIONS entirely if the Dash0 --require is the only option", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: legacyEnvVarNodeOptionsName, Value: legacyEnvVarNodeOptionsValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should remove NODE_OPTIONS entirely if the Dash0 --require is the only option and has surrounding whitespace", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: legacyEnvVarNodeOptionsName, Value: "  " + legacyEnvVarNodeOptionsValue + "   "},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should remove the Dash0 --require from NODE_OPTIONS at the start", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: legacyEnvVarNodeOptionsName, Value: legacyEnvVarNodeOptionsValue + " --abort-on-uncaught-exception --enable-fips"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":                     {Value: "value"},
					legacyEnvVarNodeOptionsName: {Value: "--abort-on-uncaught-exception --enable-fips"},
				},
			}),
			Entry("should remove the Dash0 --require from NODE_OPTIONS in the middle", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: legacyEnvVarNodeOptionsName, Value: "--abort-on-uncaught-exception " + legacyEnvVarNodeOptionsValue + " --enable-fips"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":                     {Value: "value"},
					legacyEnvVarNodeOptionsName: {Value: "--abort-on-uncaught-exception --enable-fips"},
				},
			}),
			Entry("should remove the Dash0 --require from NODE_OPTIONS with extraneous whitespace", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: legacyEnvVarNodeOptionsName, Value: "  --abort-on-uncaught-exception    " + legacyEnvVarNodeOptionsValue + "   --enable-fips  "},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":                     {Value: "value"},
					legacyEnvVarNodeOptionsName: {Value: "  --abort-on-uncaught-exception      --enable-fips  "},
				},
			}),
			Entry("should remove the Dash0 --require from NODE_OPTIONS at the end", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: legacyEnvVarNodeOptionsName, Value: "--abort-on-uncaught-exception --enable-fips " + legacyEnvVarNodeOptionsValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":                     {Value: "value"},
					legacyEnvVarNodeOptionsName: {Value: "--abort-on-uncaught-exception --enable-fips"},
				},
			}),
			Entry("should remove DASH0_SERVICE_INSTANCE_ID", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: "DASH0_SERVICE_INSTANCE_ID", Value: "foobar"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
		)

		type objectMetaResourceAttributesTest struct {
			workloadLabels                  map[string]string
			workloadAnnotations             map[string]string
			podLabels                       map[string]string
			podAnnotations                  map[string]string
			containerEnvVars                []corev1.EnvVar
			expectedEnvVars                 map[string]*EnvVarExpectation
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

				container := &corev1.Container{
					Env: testConfig.containerEnvVars,
				}
				workloadModifier.addEnvironmentVariables(
					container,
					&workloadMeta,
					&podMeta,
					logger,
				)

				envVars := container.Env
				VerifyEnvVarsFromMap(testConfig.expectedEnvVars, envVars)

				expectedResourceAttributes := testConfig.expectedResourceAttributeEnvVar
				actualResourceAttributes := FindEnvVarByName(envVars, envVarDash0ResourceAttributesName)
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
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: nil,
				},
			}),
			Entry("should not add env vars if metadata is empty", objectMetaResourceAttributesTest{
				workloadLabels:      map[string]string{},
				workloadAnnotations: map[string]string{},
				podLabels:           map[string]string{},
				podAnnotations:      map[string]string{},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: nil,
				},
			}),
			Entry("should not add env vars if metadata is unrelated", objectMetaResourceAttributesTest{
				workloadLabels:      map[string]string{"a": "b"},
				workloadAnnotations: map[string]string{"a": "b"},
				podLabels:           map[string]string{"a": "b"},
				podAnnotations:      map[string]string{"a": "b"},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: nil,
				},
			}),
			Entry("should derive env vars from workload labels", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "workload-name",
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        {Value: "workload-name"},
					envVarDash0ServiceNamespace:   {Value: "workload-part-of"},
					envVarDash0ServiceVersionName: {Value: "workload-version"},
				},
			}),
			Entry("should not derive service name from workload labels if OTEL_SERVICE_NAME is set", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "workload-name",
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				containerEnvVars: []corev1.EnvVar{
					{Name: "OTEL_SERVICE_NAME", Value: "service-name"},
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   {Value: "workload-part-of"},
					envVarDash0ServiceVersionName: {Value: "workload-version"},
				},
			}),
			Entry("should not derive service name from workload labels if service.name is set in OTEL_RESOURCE_ATTRIBUTES", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "workload-name",
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				containerEnvVars: []corev1.EnvVar{
					{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "something=else,service.name=service-name,foo=bar"},
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   {Value: "workload-part-of"},
					envVarDash0ServiceVersionName: {Value: "workload-version"},
				},
			}),
			Entry("should not derive service namespace from workload labels if service.namespace is set in OTEL_RESOURCE_ATTRIBUTES", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "workload-name",
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				containerEnvVars: []corev1.EnvVar{
					{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "service.namespace=service-name,something=else,foo=bar"},
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        {Value: "workload-name"},
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: {Value: "workload-version"},
				},
			}),
			Entry("should not derive service version from workload labels if service.version is set in OTEL_RESOURCE_ATTRIBUTES", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "workload-name",
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				containerEnvVars: []corev1.EnvVar{
					{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "service.version=1.2.3"},
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        {Value: "workload-name"},
					envVarDash0ServiceNamespace:   {Value: "workload-part-of"},
					envVarDash0ServiceVersionName: nil,
				},
			}),
			Entry("should ignore workload labels if name is not set", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: nil,
				},
			}),
			Entry("should derive env vars from pod labels", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "pod-name",
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarDash0ServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarDash0ServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
				},
			}),
			Entry("should not derive service name from pod labels vif OTEL_SERVICE_NAME is set", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "pod-name",
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				containerEnvVars: []corev1.EnvVar{
					{Name: "OTEL_SERVICE_NAME", Value: "service-name"},
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarDash0ServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
				},
			}),
			Entry("should not derive service name from pod labels if service.name is set in OTEL_RESOURCE_ATTRIBUTES", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "pod-name",
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				containerEnvVars: []corev1.EnvVar{
					{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: ",something=else , foo = bar  , service.name = service-name,"},
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarDash0ServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
				},
			}),
			Entry("should not derive service namespace from pod labels if service.namespace is set in OTEL_RESOURCE_ATTRIBUTES", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "pod-name",
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				containerEnvVars: []corev1.EnvVar{
					{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "   service.namespace   =  service-namespace  "},
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
				},
			}),
			Entry("should not derive service version from pod labels if service.version is set in OTEL_RESOURCE_ATTRIBUTES", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "pod-name",
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				containerEnvVars: []corev1.EnvVar{
					{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "service.version=1.2.3"},
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarDash0ServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarDash0ServiceVersionName: nil,
				},
			}),
			Entry("should ignore pod labels if name is not set", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: nil,
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
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarDash0ServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarDash0ServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
				},
			}),
			Entry("should derive resource attributes from workload annotations", objectMetaResourceAttributesTest{
				workloadAnnotations: map[string]string{
					"resource.opentelemetry.io/workload.ra.1": "workload-value-1",
					"resource.opentelemetry.io/workload.ra.2": "workload-value-2",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: nil,
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
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: nil,
				},
				expectedResourceAttributeEnvVar: []string{"pod.ra.1=pod-value-1", "pod.ra.2=pod-value-2"},
			}),
			Entry("pod annotations override workload annotations", objectMetaResourceAttributesTest{
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
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarDash0ServiceName:        nil,
					envVarDash0ServiceNamespace:   nil,
					envVarDash0ServiceVersionName: nil,
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

		type otelPropagatorsTest struct {
			existingEnvVars                       []corev1.EnvVar
			namespaceInstrumentationConfig        util.NamespaceInstrumentationConfig
			expectedPreInstrumentationCheckResult bool
			expectedEnvVars                       map[string]*EnvVarExpectation
		}

		DescribeTable("should add or update OTEL_PROPAGATORS",
			func(testConfig otelPropagatorsTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				// addEnvironmentVariables/handleOTelPropagatorsEnvVar and
				// otelPropagatorsEnvVarWillBeUpdatedForAtLeastOneContainer are called at different points in time,
				// but they deliberately share the exact same logic. We opportunistically test both methods here.
				preInstrumentationCheckResult := otelPropagatorsEnvVarWillBeUpdatedForAtLeastOneContainer([]corev1.Container{
					*container,
				}, testConfig.namespaceInstrumentationConfig)
				Expect(preInstrumentationCheckResult).To(Equal(testConfig.expectedPreInstrumentationCheckResult))

				NewResourceModifier(
					clusterInstrumentationConfig,
					testConfig.namespaceInstrumentationConfig,
					testActor,
					&logger,
				).addEnvironmentVariables(
					container,
					&metav1.ObjectMeta{},
					&metav1.ObjectMeta{},
					logger,
				)

				envVars := container.Env
				VerifyEnvVarsFromMap(testConfig.expectedEnvVars, envVars)
			},
			Entry("should not add OTEL_PROPAGATORS if not configured", otelPropagatorsTest{
				namespaceInstrumentationConfig:        DefaultNamespaceInstrumentationConfig,
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
			Entry("should leave existing OTEL_PROPAGATORS in place if not configured and the existing value has not been set by the operator (no previous setting in monitoring resource status)", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "tracecontext,baggage",
				}},
				namespaceInstrumentationConfig:        DefaultNamespaceInstrumentationConfig,
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {Value: "tracecontext,baggage"},
				},
			}),
			Entry("should leave existing OTEL_PROPAGATORS in place if not configured and the existing value has not been set by the operator (previous setting from monitoring resource status does not match current value)", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "tracecontext,baggage",
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					PreviousTraceContextPropagators: ptr.To("something,else"),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {Value: "tracecontext,baggage"},
				},
			}),
			Entry("should remove existing OTEL_PROPAGATORS if not configured and the existing value has been set by the operator", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "tracecontext,baggage",
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					PreviousTraceContextPropagators: ptr.To("tracecontext,baggage"),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
			Entry("should not add OTEL_PROPAGATORS if configured as empty string", otelPropagatorsTest{
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To(""),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
			Entry("should not add OTEL_PROPAGATORS if configured string is only whitespace", otelPropagatorsTest{
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To("   "),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
			Entry("should add OTEL_PROPAGATORS if configured and the env var does not exist on the container", otelPropagatorsTest{
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To("tracecontext,xray"),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {Value: "tracecontext,xray"},
				},
			}),
			Entry("should not update existing OTEL_PROPAGATORS, even if configured, if the current setting uses ValueFrom", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name: util.OtelPropagatorsEnvVarName,
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "field.path",
						},
					},
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To("tracecontext,xray"),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {ValueFrom: "field.path"},
				},
			}),
			Entry("should not update existing OTEL_PROPAGATORS, even if configured, if the current setting has not been set by the operator (no previous setting in monitoring resource status)", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "jaeger,b3multi",
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To("tracecontext,xray"),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {Value: "jaeger,b3multi"},
				},
			}),
			Entry("should not update existing OTEL_PROPAGATORS, even if configured, if the current setting has not been set by the operator (non-matching previous setting in monitoring resource status)", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "jaeger,b3multi",
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators:         ptr.To("tracecontext,xray"),
					PreviousTraceContextPropagators: ptr.To("something,else"),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {Value: "jaeger,b3multi"},
				},
			}),
			Entry("should update existing OTEL_PROPAGATORS if configured, if the current setting has been set by the operator", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "jaeger,b3multi",
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators:         ptr.To("tracecontext,xray"),
					PreviousTraceContextPropagators: ptr.To("jaeger,b3multi"),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {Value: "tracecontext,xray"},
				},
			}),
			Entry("should not update existing OTEL_PROPAGATORS if the value already matches", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "tracecontext,xray",
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators:         ptr.To("tracecontext,xray"),
					PreviousTraceContextPropagators: ptr.To("tracecontext,xray"),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {Value: "tracecontext,xray"},
				},
			}),
			Entry("should not override existing OTEL_PROPAGATORS with ValueFrom, even if configured", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name: util.OtelPropagatorsEnvVarName,
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							// unrealistic ValueFrom ref, but does not matter for the test
							FieldPath: "tracecontext,xray",
						},
					},
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To("tracecontext,xray"),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {ValueFrom: "tracecontext,xray"},
				},
			}),
		)

		DescribeTable("should remove OTEL_PROPAGATORS when uninstrumentig workloads",
			func(testConfig otelPropagatorsTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				NewResourceModifier(
					clusterInstrumentationConfig,
					testConfig.namespaceInstrumentationConfig,
					testActor,
					&logger,
				).removeEnvironmentVariables(container)

				envVars := container.Env
				VerifyEnvVarsFromMap(testConfig.expectedEnvVars, envVars)
			},
			Entry("should not remove OTEL_PROPAGATORS if not configured", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "tracecontext,xray",
				}},
				namespaceInstrumentationConfig: DefaultNamespaceInstrumentationConfig,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {Value: "tracecontext,xray"},
				},
			}),
			Entry("should not remove OTEL_PROPAGATORS if configured but configured value does not match env var", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "jaeger,b3multi",
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To("tracecontext,xray"),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {Value: "jaeger,b3multi"},
				},
			}),
			Entry("should not remove OTEL_PROPAGATORS if configured but existing env var uses ValueFrom", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name: util.OtelPropagatorsEnvVarName,
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							// unrealistic ValueFrom ref, but does not matter for the test
							FieldPath: "tracecontext,xray",
						},
					},
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To("tracecontext,xray"),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: {ValueFrom: "tracecontext,xray"},
				},
			}),
			Entry("should remove OTEL_PROPAGATORS if configured and configured value matches env var", otelPropagatorsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  util.OtelPropagatorsEnvVarName,
					Value: "tracecontext,xray",
				}},
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To("tracecontext,xray"),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
			Entry("should do nothing if env var is not set", otelPropagatorsTest{
				namespaceInstrumentationConfig: util.NamespaceInstrumentationConfig{
					TraceContextPropagators: ptr.To("tracecontext,xray"),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
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
