// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

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
	clusterInstrumentationConfigWithInitContainer = util.NewClusterInstrumentationConfig(
		TestImages,
		PossibleCollectorUrlsTest,
		OTelCollectorNodeLocalBaseUrlTest,
		util.ExtraConfigDefaults,
		cluster.ResolvedInstrumentationDeliveryInitContainer,
		nil,
		false,
		false,
	)

	clusterInstrumentationConfigWithImageVolumes = util.NewClusterInstrumentationConfig(
		TestImages,
		PossibleCollectorUrlsTest,
		OTelCollectorNodeLocalBaseUrlTest,
		util.ExtraConfigDefaults,
		cluster.ResolvedInstrumentationDeliveryImageVolume,
		nil,
		false,
		false,
	)

	clusterInstrumentationConfigWithServiceUrl = util.NewClusterInstrumentationConfig(
		TestImages,
		PossibleCollectorUrlsTest,
		OTelCollectorServiceBaseUrlTest,
		util.ExtraConfigDefaults,
		cluster.ResolvedInstrumentationDeliveryInitContainer,
		nil,
		false,
		false,
	)
)

var _ = Describe("Dash0 Workload Modification", func() {

	ctx := context.Background()
	logger := logd.FromContext(ctx)
	workloadModifierInitCnt := NewResourceModifier(
		clusterInstrumentationConfigWithInitContainer,
		DefaultNamespaceInstrumentationConfig,
		testActor,
		logger,
	)
	workloadModifierImgVol := NewResourceModifier(
		clusterInstrumentationConfigWithImageVolumes,
		DefaultNamespaceInstrumentationConfig,
		testActor,
		logger,
	)

	type envVarModificationTest struct {
		envVars      []corev1.EnvVar
		expectations map[string]*EnvVarExpectation
	}

	Context("when instrumenting workloads", func() {
		It("should instrument a basic cron job", func() {
			workload := BasicCronJob(TestNamespaceName, CronJobNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyCronJob(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedCronJob(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should instrument a basic daemon set", func() {
			workload := BasicDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyDaemonSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedDaemonSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should add Dash0 to a basic deployment (instrumentation init container)", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should add Dash0 to a basic deployment (instrumentation image volume)", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifierImgVol.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectationsWithImageVolume(), IgnoreManagedFields)
		})

		It("should use the collector service URL instead of the node-local endpoint if requested", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			modificationResult :=
				NewResourceModifier(
					util.NewClusterInstrumentationConfig(
						TestImages,
						PossibleCollectorUrlsTest,
						OTelCollectorServiceBaseUrlTest,
						util.ExtraConfigDefaults,
						cluster.ResolvedInstrumentationDeliveryInitContainer,
						nil,
						false,
						false,
					),
					DefaultNamespaceInstrumentationConfig,
					testActor,
					logger,
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
			modificationResult := workloadModifierInitCnt.ModifyDeployment(workload)

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
							"DASH0_NODE_IP": {
								ValueFrom: "status.hostIP",
							},
							"TEST0": {
								Value: "value",
							},
							"LD_PRELOAD": {
								Value: "/__otel_auto_instrumentation/injector/libotelinject.so",
							},
							"OTEL_INJECTOR_CONFIG_FILE": {
								Value: "/__otel_auto_instrumentation/injector/injector.conf",
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
							"OTEL_LOGS_EXPORTER": {
								Value: "none",
							},
							"OTEL_INJECTOR_K8S_NAMESPACE_NAME": {
								ValueFrom: "metadata.namespace",
							},
							"OTEL_INJECTOR_K8S_POD_NAME": {
								ValueFrom: "metadata.name",
							},
							"OTEL_INJECTOR_K8S_POD_UID": {
								ValueFrom: "metadata.uid",
							},
							"OTEL_INJECTOR_K8S_CONTAINER_NAME": {
								Value: "test-container-0",
							},
							"OTEL_INJECTOR_SERVICE_NAME": {
								ValueFrom: "metadata.labels['app.kubernetes.io/name']",
							},
							"OTEL_INJECTOR_SERVICE_NAMESPACE": {
								ValueFrom: "metadata.labels['app.kubernetes.io/part-of']",
							},
							"OTEL_INJECTOR_SERVICE_VERSION": {
								ValueFrom: "metadata.labels['app.kubernetes.io/version']",
							},
							"OTEL_INJECTOR_RESOURCE_ATTRIBUTES": {
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
							"DASH0_NODE_IP": {
								ValueFrom: "status.hostIP",
							},
							"TEST0": {
								Value: "value",
							},
							"TEST1": {
								ValueFrom: "metadata.namespace",
							},
							"LD_PRELOAD": {
								Value: "/__otel_auto_instrumentation/injector/libotelinject.so",
							},
							"OTEL_INJECTOR_CONFIG_FILE": {
								Value: "/__otel_auto_instrumentation/injector/injector.conf",
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
							"OTEL_LOGS_EXPORTER": {
								Value: "none",
							},
							"OTEL_INJECTOR_K8S_NAMESPACE_NAME": {
								ValueFrom: "metadata.namespace",
							},
							"OTEL_INJECTOR_K8S_POD_NAME": {
								ValueFrom: "metadata.name",
							},
							"OTEL_INJECTOR_K8S_POD_UID": {
								ValueFrom: "metadata.uid",
							},
							"OTEL_INJECTOR_K8S_CONTAINER_NAME": {
								Value: "test-container-1",
							},
							"OTEL_INJECTOR_SERVICE_NAME": {
								ValueFrom: "metadata.labels['app.kubernetes.io/name']",
							},
							"OTEL_INJECTOR_SERVICE_NAMESPACE": {
								ValueFrom: "metadata.labels['app.kubernetes.io/part-of']",
							},
							"OTEL_INJECTOR_SERVICE_VERSION": {
								ValueFrom: "metadata.labels['app.kubernetes.io/version']",
							},
							"OTEL_INJECTOR_RESOURCE_ATTRIBUTES": {
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
			modificationResult := workloadModifierInitCnt.ModifyDeployment(workload)

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
							"DASH0_NODE_IP": {
								ValueFrom: "status.hostIP",
							},
							"TEST0": {
								Value: "value",
							},
							"LD_PRELOAD": {
								// The operator does not support injecting into containers that already have LD_PRELOAD set via a
								// ValueFrom clause, thus this env var will not be modified.
								ValueFrom: "metadata.namespace",
							},
							"OTEL_INJECTOR_CONFIG_FILE": {
								Value: "/__otel_auto_instrumentation/injector/injector.conf",
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
							"OTEL_LOGS_EXPORTER": {
								Value: "none",
							},
							"OTEL_INJECTOR_K8S_NAMESPACE_NAME": {
								ValueFrom: "metadata.namespace",
							},
							"OTEL_INJECTOR_K8S_POD_NAME": {
								ValueFrom: "metadata.name",
							},
							"OTEL_INJECTOR_K8S_POD_UID": {
								ValueFrom: "metadata.uid",
							},
							"OTEL_INJECTOR_K8S_CONTAINER_NAME": {
								Value: "test-container-0",
							},
						},
					},
					{
						ContainerName:       "test-container-1",
						VolumeMounts:        3,
						Dash0VolumeMountIdx: 1,
						EnvVars: map[string]*EnvVarExpectation{
							"DASH0_NODE_IP": {
								ValueFrom: "status.hostIP",
							},
							"LD_PRELOAD": {
								Value: "/__otel_auto_instrumentation/injector/libotelinject.so third_party_preload.so another_third_party_preload.so",
							},
							"OTEL_INJECTOR_CONFIG_FILE": {
								Value: "/__otel_auto_instrumentation/injector/injector.conf",
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
							"OTEL_LOGS_EXPORTER": {
								Value: "none",
							},
							"OTEL_INJECTOR_K8S_NAMESPACE_NAME": {
								ValueFrom: "metadata.namespace",
							},
							"OTEL_INJECTOR_K8S_POD_NAME": {
								ValueFrom: "metadata.name",
							},
							"OTEL_INJECTOR_K8S_POD_UID": {
								ValueFrom: "metadata.uid",
							},
							"OTEL_INJECTOR_K8S_CONTAINER_NAME": {
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
			modificationResult := workloadModifierInitCnt.ModifyJob(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedJob(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should instrument a basic ownerless pod", func() {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyPod(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedPod(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should instrument a basic pod with an unrecognized owner", func() {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			workload.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "core.strimzi.io/v1beta2",
				Kind:       "StrimziPodSet",
			}}
			modificationResult := workloadModifierInitCnt.ModifyPod(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedPod(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		DescribeTable("should not instrument a basic pod owned by another higher level workload", func(owner metav1.TypeMeta) {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			workload.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: owner.APIVersion,
				Kind:       owner.Kind,
			}}
			modificationResult := workloadModifierInitCnt.ModifyPod(workload)

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
			modificationResult := workloadModifierInitCnt.ModifyReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should instrument a basic replica set owned by an unrecognized type", func() {
			workload := BasicReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			workload.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "something.com/v2alpha47",
				Kind:       "SomeKind",
			}}
			modificationResult := workloadModifierInitCnt.ModifyReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should not instrument a basic replica set that is owned by a deployment", func() {
			workload := ReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"The workload is part of a higher order workload that will be instrumented by the webhook, no modification by the actor is necessary."))
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should instrument a basic stateful set", func() {
			workload := BasicStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyStatefulSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyModifiedStatefulSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should not try to instrument Windows workloads", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			workload.Spec.Template.Spec.NodeSelector = map[string]string{
				util.KubernetesIoOs: "windows",
			}
			modificationResult := workloadModifierInitCnt.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"The actor has not modified this workload since it seems to be targeting a non-Linux operating " +
					"system, workload modifications are only supported for Linux workloads. " +
					"Details: pod.spec.nodeSelector: \"kubernetes.io/os=windows\""))
			VerifyUnmodifiedDeployment(workload)
		})

		It("should not instrument workloads with init container where a container's ephemeral-storage limit is below the threshold", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			workload.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceEphemeralStorage: resource.MustParse("100M"),
			}
			modificationResult := workloadModifierInitCnt.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"The actor has not modified this workload since container \"test-container-0\" has an ephemeral-storage " +
					"limit of 100M, which is below the required threshold of 500M. The Dash0 init-container instrumentation " +
					"requires at least 500M of ephemeral storage, raise the limit or remove it to enable instrumentation, or " +
					"use image volume instrumentation delivery."))
			VerifyUnmodifiedDeployment(workload)
		})

		It("should instrument workloads with image volume even if a container's ephemeral-storage limit is below the threshold", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			workload.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceEphemeralStorage: resource.MustParse("100M"),
			}
			modificationResult := workloadModifierImgVol.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
		})

		It("should instrument workloads where the ephemeral-storage limit equals the threshold", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			workload.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceEphemeralStorage: resource.MustParse("500M"),
			}
			modificationResult := workloadModifierInitCnt.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
		})

		It("should instrument workloads where the ephemeral-storage limit is only set on an init container", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			workload.Spec.Template.Spec.InitContainers = []corev1.Container{{
				Name:  "user-init-container",
				Image: "ubuntu",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceEphemeralStorage: resource.MustParse("10M"),
					},
				},
			}}
			modificationResult := workloadModifierInitCnt.ModifyDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
		})
	})

	Context("when instrumenting workloads multiple times (instrumentation needs to be idempotent)", func() {
		It("cron job instrumentation needs to be idempotent", func() {
			workload := BasicCronJob(TestNamespaceName, CronJobNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyCronJob(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifierInitCnt.ModifyCronJob(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedCronJob(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("daemon set instrumentation needs to be idempotent", func() {
			workload := BasicDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyDaemonSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifierInitCnt.ModifyDaemonSet(workload)
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedDaemonSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("deployment instrumentation needs to be idempotent", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyDeployment(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifierInitCnt.ModifyDeployment(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("job instrumentation needs to be idempotent", func() {
			workload := BasicJob(TestNamespaceName, JobNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyJob(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifierInitCnt.ModifyJob(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedJob(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("ownerless pod instrumentation needs to be idempotent", func() {
			workload := BasicPod(TestNamespaceName, PodNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyPod(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifierInitCnt.ModifyPod(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedPod(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("ownerless replica set instrumentation needs to be idempotent", func() {
			workload := BasicReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyReplicaSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifierInitCnt.ModifyReplicaSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})

		It("stateful set instrumentation needs to be idempotent", func() {
			workload := BasicStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			modificationResult := workloadModifierInitCnt.ModifyStatefulSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeTrue())
			instrumentedOnce := workload.DeepCopy()
			modificationResult = workloadModifierInitCnt.ModifyStatefulSet(workload)
			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"Dash0 instrumentation was already present on this workload, no modification by the actor is necessary."))
			VerifyModifiedStatefulSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
			Expect(reflect.DeepEqual(instrumentedOnce, workload)).To(BeTrue())
		})
	})

	Context("when the instrumentation delivery mode changes between reconciliations", func() {
		It("should clean up init container artifacts when re-instrumenting init-container -> image-volume", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)

			firstResult := workloadModifierInitCnt.ModifyDeployment(workload)
			Expect(firstResult.HasBeenModified).To(BeTrue())
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)

			secondResult := workloadModifierImgVol.ModifyDeployment(workload)
			Expect(secondResult.HasBeenModified).To(BeTrue())
			// The end state must match what would result from instrumenting via image-volume delivery from scratch:
			// no dash0-instrumentation init container, no safe-to-evict-local-volumes annotation, the volume
			// switched from emptyDir to image, and the container volume mount carries the image-volume SubPath.
			VerifyModifiedDeployment(
				workload, BasicInstrumentedPodSpecExpectationsWithImageVolume(), IgnoreManagedFields)
			Expect(workload.Spec.Template.Spec.InitContainers).To(BeEmpty())
			Expect(workload.Spec.Template.Annotations).
				NotTo(HaveKey(safeToEviceLocalVolumesAnnotationName))
		})

		It("should produce a correct image-volume->init-container layout when re-instrumenting", func() {
			workload := BasicDeployment(TestNamespaceName, DeploymentNamePrefix)

			firstResult := workloadModifierImgVol.ModifyDeployment(workload)
			Expect(firstResult.HasBeenModified).To(BeTrue())
			VerifyModifiedDeployment(
				workload, BasicInstrumentedPodSpecExpectationsWithImageVolume(), IgnoreManagedFields)

			secondResult := workloadModifierInitCnt.ModifyDeployment(workload)
			Expect(secondResult.HasBeenModified).To(BeTrue())
			// The end state must match what would result from instrumenting via init-container delivery from
			// scratch: the dash0-instrumentation init container is added, the safe-to-evict-local-volumes
			// annotation lists the Dash0 volume, the volume switched from image to emptyDir, and the container
			// volume mount no longer has the image-volume SubPath.
			VerifyModifiedDeployment(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})
	})

	Context("when reverting workloads", func() {
		It("should remove Dash0 from an instrumented cron job", func() {
			workload := InstrumentedCronJob(TestNamespaceName, CronJobNamePrefix)
			modificationResult := workloadModifierInitCnt.RevertCronJob(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedCronJob(workload)
		})

		It("should remove Dash0 from an instrumented daemon set", func() {
			workload := InstrumentedDaemonSet(TestNamespaceName, DaemonSetNamePrefix)
			modificationResult := workloadModifierInitCnt.RevertDaemonSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedDaemonSet(workload)
		})

		It("should remove Dash0 from an instrumented deployment", func() {
			workload := InstrumentedDeployment(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifierInitCnt.RevertDeployment(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedDeployment(workload)
		})

		It("should remove Dash0 from an instrumented deployment that has multiple containers, and already has volumes and init containers previous to being instrumented", func() {
			workload := InstrumentedDeploymentWithMoreBellsAndWhistles(TestNamespaceName, DeploymentNamePrefix)
			modificationResult := workloadModifierInitCnt.RevertDeployment(workload)

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
			modificationResult := workloadModifierInitCnt.RevertReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should remove Dash0 from a replica set owned by an unrecognized type", func() {
			workload := InstrumentedReplicaSet(TestNamespaceName, ReplicaSetNamePrefix)
			workload.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "something.com/v2alpha47",
				Kind:       "SomeKind",
			}}
			modificationResult := workloadModifierInitCnt.RevertReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedReplicaSet(workload)
		})

		It("should not remove Dash0 from a replica set that is owned by a deployment", func() {
			workload := InstrumentedReplicaSetOwnedByDeployment(TestNamespaceName, ReplicaSetNamePrefix)
			modificationResult := workloadModifierInitCnt.RevertReplicaSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeFalse())
			Expect(modificationResult.RenderReasonMessage(testActor)).To(Equal(
				"The workload is part of a higher order workload that will be instrumented by the webhook, no modification by the actor is necessary."))
			VerifyModifiedReplicaSet(workload, BasicInstrumentedPodSpecExpectations(), IgnoreManagedFields)
		})

		It("should remove Dash0 from an instrumented stateful set", func() {
			workload := InstrumentedStatefulSet(TestNamespaceName, StatefulSetNamePrefix)
			modificationResult := workloadModifierInitCnt.RevertStatefulSet(workload)

			Expect(modificationResult.HasBeenModified).To(BeTrue())
			VerifyUnmodifiedStatefulSet(workload)
		})
	})

	Context("individual modification functions", func() {
		ctx := context.Background()
		logger := logd.FromContext(ctx)
		workloadModifier := NewResourceModifier(
			clusterInstrumentationConfigWithInitContainer,
			DefaultNamespaceInstrumentationConfig,
			testActor,
			logger,
		)

		type instrumentationDeliveryTest struct {
			instrumentationDelivery cluster.ResolvedInstrumentationDelivery
		}

		DescribeTable("should attach the instrumentation either via init container or image volume",
			func(testConfig instrumentationDeliveryTest) {
				config := clusterInstrumentationConfigWithInitContainer
				if testConfig.instrumentationDelivery == cluster.ResolvedInstrumentationDeliveryImageVolume {
					config = clusterInstrumentationConfigWithImageVolumes
				}
				modifier := NewResourceModifier(
					config,
					DefaultNamespaceInstrumentationConfig,
					testActor,
					logger,
				)

				podSpec := &corev1.PodSpec{
					Containers: []corev1.Container{{Name: "test-container-0"}},
				}
				podMeta := &metav1.ObjectMeta{}

				modifier.modifyPodSpec(podSpec, &metav1.ObjectMeta{}, podMeta)

				Expect(podSpec.Volumes).To(HaveLen(1))
				volume := podSpec.Volumes[0]
				Expect(volume.Name).To(Equal(dash0VolumeName))

				mounts := podSpec.Containers[0].VolumeMounts
				Expect(mounts).To(HaveLen(1))
				Expect(mounts[0].Name).To(Equal(dash0VolumeName))
				Expect(mounts[0].MountPath).To(Equal(otelAutoInstrumentationBaseDirectory))

				if testConfig.instrumentationDelivery == cluster.ResolvedInstrumentationDeliveryImageVolume {
					Expect(volume.EmptyDir).To(BeNil())
					Expect(volume.Image).NotTo(BeNil())
					Expect(volume.Image.Reference).To(Equal(InitContainerImageTest))
					Expect(podSpec.InitContainers).To(BeEmpty())
					Expect(mounts[0].SubPath).To(Equal(imageVolumeSubPath))
					Expect(podMeta.Annotations).To(BeEmpty())
				} else {
					Expect(volume.EmptyDir).NotTo(BeNil())
					Expect(volume.Image).To(BeNil())
					Expect(podSpec.InitContainers).To(HaveLen(1))
					Expect(podSpec.InitContainers[0].Name).To(Equal(initContainerName))
					Expect(podSpec.InitContainers[0].Image).To(Equal(InitContainerImageTest))
					Expect(podSpec.InitContainers[0].VolumeMounts).To(HaveLen(1))
					Expect(podSpec.InitContainers[0].VolumeMounts[0].Name).To(Equal(dash0VolumeName))
					Expect(podSpec.InitContainers[0].VolumeMounts[0].MountPath).To(
						Equal(otelAutoInstrumentationBaseDirectory))
					Expect(mounts[0].SubPath).To(BeEmpty())
					Expect(podMeta.Annotations[safeToEviceLocalVolumesAnnotationName]).To(Equal(dash0VolumeName))
				}
			},
			Entry("via init container", instrumentationDeliveryTest{
				instrumentationDelivery: cluster.ResolvedInstrumentationDeliveryInitContainer,
			}),
			Entry("via image volume", instrumentationDeliveryTest{
				instrumentationDelivery: cluster.ResolvedInstrumentationDeliveryImageVolume,
			}),
		)

		type detectNonLinuxPodTest struct {
			podSpec         corev1.PodSpec
			expectedMessage *string
		}

		DescribeTable("should detect non-Linux pods heuristically based on pod OS, node selectors or node affinities",
			func(testConfig detectNonLinuxPodTest) {
				result := workloadModifier.checkEligibleForModification(&testConfig.podSpec)
				if testConfig.expectedMessage == nil {
					Expect(result).To(BeNil())
				} else {
					Expect(*result).ToNot(BeNil())
					Expect(result.HasBeenModified).To(BeFalse())
					message := result.RenderReasonMessage(testActor)
					Expect(message).To(Equal(*testConfig.expectedMessage))
				}
			},
			Entry("should deem pod spec without any OS specification eligible", detectNonLinuxPodTest{
				podSpec:         corev1.PodSpec{},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with Linux pod OS eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					OS: &corev1.PodOS{
						Name: corev1.Linux,
					},
				},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with Windows pod OS non-eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					OS: &corev1.PodOS{
						Name: corev1.Windows,
					},
				},
				expectedMessage: new(
					"The actor has not modified this workload since it seems to be targeting a non-Linux operating " +
						"system, workload modifications are only supported for Linux workloads. Details: " +
						"pod.spec.os.name: \"windows\"",
				),
			}),
			Entry("should deem pod spec with random pod OS non-eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					OS: &corev1.PodOS{
						Name: "whatever",
					},
				},
				expectedMessage: new(
					"The actor has not modified this workload since it seems to be targeting a non-Linux operating " +
						"system, workload modifications are only supported for Linux workloads. Details: " +
						"pod.spec.os.name: \"whatever\""),
			}),
			Entry("should deem pod spec with unrelated node selectors eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					NodeSelector: map[string]string{
						"some-label":    "some-value",
						"another-label": "another-value",
					},
				},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with Linux node selector eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					NodeSelector: map[string]string{
						util.KubernetesIoOs: "linux",
					},
				},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with Windows node selector non-eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					NodeSelector: map[string]string{
						util.KubernetesIoOs: "windows",
					},
				},
				expectedMessage: new(
					"The actor has not modified this workload since it seems to be targeting a non-Linux operating " +
						"system, workload modifications are only supported for Linux workloads. " +
						"Details: pod.spec.nodeSelector: \"kubernetes.io/os=windows\"",
				),
			}),
			Entry("should deem pod spec with empty Affinity struct eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					Affinity: &corev1.Affinity{},
				},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with empty NodeAffinity struct eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{},
					},
				},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with only PreferredDuringSchedulingIgnoredDuringExecution node affinities eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
								{Preference: corev1.NodeSelectorTerm{}},
							},
						},
					},
				},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with unrelated RequiredDuringSchedulingIgnoredDuringExecution node affinities eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{Key: "some-label", Operator: corev1.NodeSelectorOpIn, Values: []string{"label-value"}},
										},
										MatchFields: []corev1.NodeSelectorRequirement{
											{Key: "field", Operator: corev1.NodeSelectorOpIn, Values: []string{"field-value"}},
										},
									},
								},
							},
						},
					},
				},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with Linux node affinities via operator 'In' eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{Key: "some-label1", Operator: corev1.NodeSelectorOpIn, Values: []string{"label-value1"}},
											{Key: util.KubernetesIoOs, Operator: corev1.NodeSelectorOpIn, Values: []string{"windows", "linux"}},
											{Key: "some-label2", Operator: corev1.NodeSelectorOpIn, Values: []string{"label-value2"}},
										},
									},
								},
							},
						},
					},
				},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with non-Linux node affinities via operator 'In' non-eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{Key: "some-label1", Operator: corev1.NodeSelectorOpIn, Values: []string{"label-value1"}},
											{Key: util.KubernetesIoOs, Operator: corev1.NodeSelectorOpIn, Values: []string{"some-operating-system", "windows"}},
											{Key: "some-label2", Operator: corev1.NodeSelectorOpIn, Values: []string{"label-value2"}},
										},
									},
								},
							},
						},
					},
				},
				expectedMessage: new(
					"The actor has not modified this workload since it seems to be targeting a non-Linux operating " +
						"system, workload modifications are only supported for Linux workloads. " +
						"Details: pod.spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution." +
						"nodeSelectorTerms.matchExpression: key: \"kubernetes.io/os\", operator: \"In\", " +
						"values: \"[some-operating-system windows]\"",
				),
			}),
			Entry("should deem pod spec with Linux node affinities via operator 'NotIn' eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{Key: "some-label1", Operator: corev1.NodeSelectorOpIn, Values: []string{"label-value1"}},
											{Key: util.KubernetesIoOs, Operator: corev1.NodeSelectorOpNotIn, Values: []string{"windows", "some-operating-system"}},
											{Key: "some-label2", Operator: corev1.NodeSelectorOpIn, Values: []string{"label-value2"}},
										},
									},
								},
							},
						},
					},
				},
				expectedMessage: nil,
			}),
			Entry("should deem pod spec with non-Linux node affinities via operator 'NotIn' non-eligible", detectNonLinuxPodTest{
				podSpec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{Key: "some-label1", Operator: corev1.NodeSelectorOpIn, Values: []string{"label-value1"}},
											{Key: util.KubernetesIoOs, Operator: corev1.NodeSelectorOpNotIn, Values: []string{"linux", "some-operating-system"}},
											{Key: "some-label2", Operator: corev1.NodeSelectorOpIn, Values: []string{"label-value2"}},
										},
									},
								},
							},
						},
					},
				},
				expectedMessage: new(
					"The actor has not modified this workload since it seems to be targeting a non-Linux operating " +
						"system, workload modifications are only supported for Linux workloads. " +
						"Details: pod.spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution." +
						"nodeSelectorTerms.matchExpression: key: \"kubernetes.io/os\", operator: \"NotIn\", " +
						"values: \"[linux some-operating-system]\"",
				),
			}),
		)

		type ephemeralStorageLimitTest struct {
			// Per-container ephemeral-storage limit, parsed via resource.MustParse. Empty string means "no limit set".
			containerLimits []string
			expectedMessage *string
		}

		buildPodSpec := func(containerLimits []string) corev1.PodSpec {
			containers := make([]corev1.Container, len(containerLimits))
			for i, limitStr := range containerLimits {
				containers[i] = corev1.Container{
					Name:  fmt.Sprintf("c%d", i),
					Image: "ubuntu",
				}
				if limitStr != "" {
					containers[i].Resources.Limits = corev1.ResourceList{
						corev1.ResourceEphemeralStorage: resource.MustParse(limitStr),
					}
				}
			}
			return corev1.PodSpec{Containers: containers}
		}

		DescribeTable("should detect containers with too-low ephemeral-storage limits",
			func(testConfig ephemeralStorageLimitTest) {
				podSpec := buildPodSpec(testConfig.containerLimits)
				result := workloadModifier.checkEligibleForModification(&podSpec)
				if testConfig.expectedMessage == nil {
					Expect(result).To(BeNil())
				} else {
					Expect(result).ToNot(BeNil())
					Expect(result.HasBeenModified).To(BeFalse())
					Expect(result.RenderReasonMessage(testActor)).To(Equal(*testConfig.expectedMessage))
				}
			},
			Entry("no ephemeral-storage limit set is eligible", ephemeralStorageLimitTest{
				containerLimits: []string{""},
				expectedMessage: nil,
			}),
			Entry("ephemeral-storage limit equal to the threshold is eligible", ephemeralStorageLimitTest{
				containerLimits: []string{"500M"},
				expectedMessage: nil,
			}),
			Entry("ephemeral-storage limit above the threshold is eligible", ephemeralStorageLimitTest{
				containerLimits: []string{"1Gi"},
				expectedMessage: nil,
			}),
			Entry("ephemeral-storage limit below the threshold is non-eligible", ephemeralStorageLimitTest{
				containerLimits: []string{"100M"},
				expectedMessage: new(
					"The actor has not modified this workload since container \"c0\" has an ephemeral-storage limit " +
						"of 100M, which is below the required threshold of 500M. The Dash0 init-container instrumentation " +
						"requires at least 500M of ephemeral storage, raise the limit or remove it to enable " +
						"instrumentation, or use image volume instrumentation delivery.",
				),
			}),
			Entry("ephemeral-storage limit just below the threshold is non-eligible", ephemeralStorageLimitTest{
				containerLimits: []string{"499999999"},
				expectedMessage: new(
					"The actor has not modified this workload since container \"c0\" has an ephemeral-storage limit " +
						"of 499999999, which is below the required threshold of 500M. The Dash0 init-container instrumentation " +
						"requires at least 500M of ephemeral storage, raise the limit or remove it to enable instrumentation, " +
						"or use image volume instrumentation delivery.",
				),
			}),
			Entry("only the second container has a too-low limit; workload is non-eligible", ephemeralStorageLimitTest{
				containerLimits: []string{"1Gi", "50M"},
				expectedMessage: new(
					"The actor has not modified this workload since container \"c1\" has an ephemeral-storage limit " +
						"of 50M, which is below the required threshold of 500M. The Dash0 init-container instrumentation " +
						"requires at least 500M of ephemeral storage, raise the limit or remove it to enable " +
						"instrumentation, or use image volume instrumentation delivery.",
				),
			}),
		)

		type addLdPreloadTest struct {
			value                         string
			valueFrom                     string
			ldPreloadExpectation          EnvVarExpectation
			expectedInstrumentationIssues []string
		}

		DescribeTable("should add the OpenTelemetry injector to LD_PRELOAD",
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

				instrumentationIssues := workloadModifier.addEnvironmentVariables(
					container,
					&metav1.ObjectMeta{},
					&metav1.ObjectMeta{},
					logger,
				)

				VerifyEnvVar(testConfig.ldPreloadExpectation, container.Env, envVarLdPreloadName, "")
				Expect(instrumentationIssues).To(Equal(testConfig.expectedInstrumentationIssues))
			},
			Entry("should add LD_PRELOAD with only the OpenTelemetry injector if it does not exist", addLdPreloadTest{
				ldPreloadExpectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should add LD_PRELOAD with only the OpenTelemetry injector the env var exists but is empty", addLdPreloadTest{
				value: "",
				ldPreloadExpectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should add LD_PRELOAD with only the OpenTelemetry injector the env var exists but is only whitespace", addLdPreloadTest{
				value: "   ",
				ldPreloadExpectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should do nothing if LD_PRELOAD already has the OpenTelemetry injector as its only element", addLdPreloadTest{
				value: envVarLdPreloadValue,
				ldPreloadExpectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should do nothing if LD_PRELOAD already has the OpenTelemetry injector as its first element", addLdPreloadTest{
				value: envVarLdPreloadValue + " one.so two.so",
				ldPreloadExpectation: EnvVarExpectation{
					Value: envVarLdPreloadValue + " one.so two.so",
				},
			}),
			Entry("should do nothing if LD_PRELOAD already has the OpenTelemetry injector in the middle", addLdPreloadTest{
				value: "one.so " + envVarLdPreloadValue + " two.so",
				ldPreloadExpectation: EnvVarExpectation{
					Value: "one.so " + envVarLdPreloadValue + " two.so",
				},
			}),
			Entry("should do nothing if LD_PRELOAD already has the OpenTelemetry injector at the end", addLdPreloadTest{
				value: "one.so two.so " + envVarLdPreloadValue,
				ldPreloadExpectation: EnvVarExpectation{
					Value: "one.so two.so " + envVarLdPreloadValue,
				},
			}),
			Entry("should prepend the OpenTelemetry injector to LD_PRELOAD if there are other libraries", addLdPreloadTest{
				value: "one.so",
				ldPreloadExpectation: EnvVarExpectation{
					Value: envVarLdPreloadValue + " one.so",
				},
			}),
			Entry("should create an instrumentation issue if LD_PRELOAD exists with ValueFrom", addLdPreloadTest{
				valueFrom: "whatever",
				ldPreloadExpectation: EnvVarExpectation{
					ValueFrom: "whatever",
				},
				expectedInstrumentationIssues: []string{
					"Dash0 cannot prepend anything to the environment variable LD_PRELOAD as it is specified via " +
						"ValueFrom, this container will not be instrumented to send telemetry to Dash0.",
				},
			}),
			Entry("should remove the legacy Dash0 injector and add the OpenTelemetry injector to LD_PRELOAD", addLdPreloadTest{
				value: envVarLdPreloadLegacyValue,
				ldPreloadExpectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should remove the legacy Dash0 injector and add the OpenTelemetry injector to LD_PRELOAD in combination with other libs", addLdPreloadTest{
				value: "one.so " + envVarLdPreloadLegacyValue + " two.so",
				ldPreloadExpectation: EnvVarExpectation{
					Value: envVarLdPreloadValue + " one.so two.so",
				},
			}),
			Entry("should remove the legacy Dash0 injector and leave the existing OpenTelemetry injector entry in place", addLdPreloadTest{
				value: envVarLdPreloadValue + " " + envVarLdPreloadLegacyValue,
				ldPreloadExpectation: EnvVarExpectation{
					Value: envVarLdPreloadValue,
				},
			}),
			Entry("should remove the legacy Dash0 injector and leave the existing OpenTelemetry injector entry in place with other libraries present", addLdPreloadTest{
				value: "one.so " + envVarLdPreloadValue + " two.so " + envVarLdPreloadLegacyValue,
				ldPreloadExpectation: EnvVarExpectation{
					Value: "one.so " + envVarLdPreloadValue + " two.so",
				},
			}),
		)

		DescribeTable("should remove the OpenTelemetry injector from LD_PRELOAD",
			func(testConfig envVarModificationTest) {
				container := &corev1.Container{Env: testConfig.envVars}
				workloadModifier.removeEnvironmentVariables(container)
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
			Entry("should do nothing if LD_PRELOAD does not list the OpenTelemetry injector", envVarModificationTest{
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
			Entry("should remove LD_PRELOAD if the OpenTelemetry injector is the only library", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: envVarLdPreloadValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should remove LD_PRELOAD if the OpenTelemetry injector is the only library and has surrounding whitespace", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "  " + envVarLdPreloadValue + "   "},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should remove the OpenTelemetry injector from LD_PRELOAD at the start (space separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: envVarLdPreloadValue + " one.so two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so"},
				},
			}),
			Entry("should remove the OpenTelemetry injector from LD_PRELOAD in the middle (space separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so " + envVarLdPreloadValue + " two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so"},
				},
			}),
			Entry("should remove the OpenTelemetry injector from LD_PRELOAD with extraneous whitespace (space separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "  one.so    " + envVarLdPreloadValue + "   two.so  "},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so"},
				},
			}),
			Entry("should remove the OpenTelemetry injector from LD_PRELOAD at the end (space separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so two.so " + envVarLdPreloadValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so"},
				},
			}),
			Entry("should remove the OpenTelemetry injector from LD_PRELOAD at the start (colon separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: envVarLdPreloadValue + ":one.so:two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so:two.so"},
				},
			}),
			Entry("should remove the OpenTelemetry injector from LD_PRELOAD in the middle (colon separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so:" + envVarLdPreloadValue + ":two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so:two.so"},
				},
			}),
			Entry("should remove the OpenTelemetry injector from LD_PRELOAD with extraneous whitespace (colon separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "  one.so  :  " + envVarLdPreloadValue + " :  two.so  "},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so:two.so"},
				},
			}),
			Entry("should remove the OpenTelemetry injector from LD_PRELOAD at the end (colon separated)", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so:two.so:" + envVarLdPreloadValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so:two.so"},
				},
			}),
			Entry("should remove LD_PRELOAD if the legacy Dash0 injector is the only library", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: envVarLdPreloadLegacyValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should remove the legacy Dash0 injector from LD_PRELOAD", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so " + envVarLdPreloadLegacyValue + " two.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so"},
				},
			}),
			Entry("should remove LD_PRELOAD if it only has the OpenTelemetry injector and the legacy Dash0 injector", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: envVarLdPreloadValue + " " + envVarLdPreloadLegacyValue},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
			Entry("should remove the OpenTelemetry injector and the legacy Dash0 injector from LD_PRELOAD", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: envVarLdPreloadName, Value: "one.so " + envVarLdPreloadValue + " two.so " + envVarLdPreloadLegacyValue + " three.so"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER":             {Value: "value"},
					envVarLdPreloadName: {Value: "one.so two.so three.so"},
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
			Entry("should remove legacy DASH0_* injector resource attribute variables", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "OTHER", Value: "value"},
					{Name: "DASH0_NAMESPACE_NAME", Value: "legacy value"},
					{Name: "DASH0_POD_NAME", Value: "legacy value"},
					{Name: "DASH0_POD_UID", Value: "legacy value"},
					{Name: "DASH0_CONTAINER_NAME", Value: "legacy value"},
					{Name: "DASH0_SERVICE_NAME", Value: "legacy value"},
					{Name: "DASH0_SERVICE_NAMESPACE", Value: "legacy value"},
					{Name: "DASH0_SERVICE_VERSION", Value: "legacy value"},
					{Name: "DASH0_RESOURCE_ATTRIBUTES", Value: "legacy value"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTHER": {Value: "value"},
				},
			}),
		)

		type nodeIpEnvVarTest struct {
			existingEnvVars     []corev1.EnvVar
			expectedEnvVarCount int
		}

		DescribeTable("should prepend DASH0_NODE_IP",
			func(testConfig nodeIpEnvVarTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				workloadModifier.prependDash0NodeIp(container)

				envVars := container.Env
				Expect(len(envVars)).To(Equal(testConfig.expectedEnvVarCount))
				nodeIpEnvVar := envVars[0]
				Expect(nodeIpEnvVar.Name).To(Equal(util.EnvVarDash0NodeIp))
				Expect(nodeIpEnvVar.Value).To(BeEmpty())
				Expect(nodeIpEnvVar.ValueFrom.FieldRef.FieldPath).To(Equal("status.hostIP"))
				if len(envVars) > 1 {
					for _, envVar := range envVars[1:] {
						Expect(envVar.Name).ToNot(Equal(util.EnvVarDash0NodeIp))
					}
				}
			},
			Entry("should add DASH0_NODE_IP to container without environment variables", nodeIpEnvVarTest{
				existingEnvVars:     nil,
				expectedEnvVarCount: 1,
			}),
			Entry("should prepend DASH0_NODE_IP to container with environment variables", nodeIpEnvVarTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  "ENV_VAR_1",
					Value: "VALUE 1",
				}, {
					Name:  "ENV_VAR_2",
					Value: "VALUE 2",
				}},
				expectedEnvVarCount: 3,
			}),
			Entry("should move DASH0_NODE_IP to first entry", nodeIpEnvVarTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  "ENV_VAR_1",
					Value: "VALUE 1",
				}, {
					Name:  "ENV_VAR_2",
					Value: "VALUE 2",
				}, {
					Name: util.EnvVarDash0NodeIp,
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "status.hostIP",
						},
					},
				}},
				expectedEnvVarCount: 3,
			}),
			Entry("should move DASH0_NODE_IP to first entry and remove value", nodeIpEnvVarTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  "ENV_VAR_1",
					Value: "VALUE 1",
				}, {
					Name:  "ENV_VAR_2",
					Value: "VALUE 2",
				}, {
					Name:  util.EnvVarDash0NodeIp,
					Value: "whatever",
				}},
				expectedEnvVarCount: 3,
			}),
		)

		type otelExporterEnvVarsTest struct {
			existingEnvVars                       []corev1.EnvVar
			clusterInstrumentationConfig          *util.ClusterInstrumentationConfig
			expectedPreInstrumentationCheckResult bool
			expectedEnvVars                       map[string]*EnvVarExpectation
			expectedInstrumentationIssue          string
			expectedLogMessage                    string
		}

		DescribeTable("should add or update OTEL_EXPORTER_OTLP_ENDPOINT/OTEL_EXPORTER_OTLP_PROTOCOL",
			func(testConfig otelExporterEnvVarsTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				clusterInstrumentationConfig := testConfig.clusterInstrumentationConfig
				if clusterInstrumentationConfig == nil {
					clusterInstrumentationConfig = clusterInstrumentationConfigWithInitContainer
				}

				preInstrumentationCheckResult := otelExportEnvVarsWillBeUpdatedForAtLeastOneContainer([]corev1.Container{
					*container,
				}, clusterInstrumentationConfig)
				Expect(preInstrumentationCheckResult).To(Equal(testConfig.expectedPreInstrumentationCheckResult))

				modifier := NewResourceModifier(
					clusterInstrumentationConfig,
					DefaultNamespaceInstrumentationConfig,
					testActor,
					logger,
				)

				capturingLogger, capturingLogSink := NewCapturingLogger()
				instrumentationIssues := modifier.addEnvironmentVariables(
					container,
					&metav1.ObjectMeta{},
					&metav1.ObjectMeta{},
					capturingLogger,
				)

				envVars := container.Env
				VerifyEnvVarsFromMap(testConfig.expectedEnvVars, envVars)
				if testConfig.expectedInstrumentationIssue == "" {
					Expect(instrumentationIssues).To(BeNil())
				} else {
					Expect(instrumentationIssues).To(HaveLen(1))
					Expect(instrumentationIssues).To(ContainElement(testConfig.expectedInstrumentationIssue))
				}

				if testConfig.expectedLogMessage == "" {
					capturingLogSink.HasNoLogMessages(Default)
				} else {
					capturingLogSink.HasLogMessage(Default, testConfig.expectedLogMessage)
				}
			},
			Entry("should add OTEL_EXPORTER_OTLP_* to container without environment variables", otelExporterEnvVarsTest{
				existingEnvVars:                       nil,
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: OTelCollectorNodeLocalBaseUrlTest},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
			}),
			Entry("should add OTEL_EXPORTER_OTLP_* to container with existing environment variables", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  "ENV_VAR_1",
					Value: "VALUE 1",
				}, {
					Name:  "ENV_VAR_2",
					Value: "VALUE 2",
				}},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: OTelCollectorNodeLocalBaseUrlTest},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
			}),
			Entry("should update OTEL_EXPORTER_OTLP_ENDPOINT when switching from node-local to service URL", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelExporterOtlpEndpointName,
					Value: OTelCollectorNodeLocalBaseUrlTest,
				}, {
					Name:  envVarOtelExporterOtlpProtocolName,
					Value: common.ProtocolHttpProtobuf,
				}},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: OTelCollectorServiceBaseUrlTest},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
				clusterInstrumentationConfig: clusterInstrumentationConfigWithServiceUrl,
			}),
			Entry("should update OTEL_EXPORTER_OTLP_ENDPOINT when switching from service URL to node-local", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelExporterOtlpEndpointName,
					Value: OTelCollectorServiceBaseUrlTest,
				}, {
					Name:  envVarOtelExporterOtlpProtocolName,
					Value: common.ProtocolHttpProtobuf,
				}},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: OTelCollectorNodeLocalBaseUrlTest},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
			}),
			Entry("should update OTEL_EXPORTER_OTLP_ENDPOINT and add missing _PROTOCOL when switching from node-local to service URL", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelExporterOtlpEndpointName,
					Value: OTelCollectorNodeLocalBaseUrlTest,
				}},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: OTelCollectorServiceBaseUrlTest},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
				clusterInstrumentationConfig: clusterInstrumentationConfigWithServiceUrl,
			}),
			Entry("should not update OTEL_EXPORTER_OTLP_ENDPOINT when _PROTOCOL is set and has an unexpected value", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelExporterOtlpEndpointName,
					Value: OTelCollectorServiceBaseUrlTest,
				}, {
					Name:  envVarOtelExporterOtlpProtocolName,
					Value: common.ProtocolGrpc,
				}},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: OTelCollectorServiceBaseUrlTest},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolGrpc},
				},
				expectedInstrumentationIssue: otelExporterOtlpNoOverwriteMsg,
				expectedLogMessage:           otelExporterOtlpNoOverwriteMsg,
			}),
			Entry("should not overwrite OTEL_EXPORTER_OTLP_* when OTEL_EXPORTER_OTLP_ENDPOINT is already set to a value not set by the operator", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelExporterOtlpEndpointName,
					Value: "http://some-endpoint.tld",
				}},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: "http://some-endpoint.tld"},
					envVarOtelExporterOtlpProtocolName: nil,
				},
				expectedInstrumentationIssue: otelExporterOtlpNoOverwriteMsg,
				expectedLogMessage:           otelExporterOtlpNoOverwriteMsg,
			}),
			Entry("should not overwrite OTEL_EXPORTER_OTLP_* when OTEL_EXPORTER_OTLP_PROTOCOL is already set", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelExporterOtlpProtocolName,
					Value: common.ProtocolGrpc,
				}},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: nil,
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolGrpc},
				},
				expectedInstrumentationIssue: otelExporterOtlpNoOverwriteMsg,
				expectedLogMessage:           otelExporterOtlpNoOverwriteMsg,
			}),
			Entry("should not overwrite OTEL_EXPORTER_OTLP_* when both OTEL_EXPORTER_OTLP_* variables are already set", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpEndpointName,
						Value: "http://some-endpoint.tld",
					},
					{
						Name:  envVarOtelExporterOtlpProtocolName,
						Value: common.ProtocolGrpc,
					}},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: "http://some-endpoint.tld"},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolGrpc},
				},
				expectedInstrumentationIssue: otelExporterOtlpNoOverwriteMsg,
				expectedLogMessage:           otelExporterOtlpNoOverwriteMsg,
			}),
			Entry("should not log an issue if both OTEL_EXPORTER_OTLP_* variables are already set correctly", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpEndpointName,
						Value: OTelCollectorNodeLocalBaseUrlTest,
					},
					{
						Name:  envVarOtelExporterOtlpProtocolName,
						Value: common.ProtocolHttpProtobuf,
					}},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: OTelCollectorNodeLocalBaseUrlTest},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
			}),
			Entry("should add OTEL_EXPORTER_OTLP_PROTOCOL if only OTEL_EXPORTER_OTLP_ENDPOINT is set correctly", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpEndpointName,
						Value: OTelCollectorNodeLocalBaseUrlTest,
					}},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: OTelCollectorNodeLocalBaseUrlTest},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
			}),
			Entry("should not add OTEL_EXPORTER_OTLP_ENDPOINT but log an issue if only OTEL_EXPORTER_OTLP_PROTOCOL is set correctly (endpoint empty)", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpEndpointName,
						Value: " ",
					},
					{
						Name:  envVarOtelExporterOtlpProtocolName,
						Value: common.ProtocolHttpProtobuf,
					}},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: " "},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
				expectedInstrumentationIssue: otelExporterOtlpNoOverwriteMsg,
				expectedLogMessage:           otelExporterOtlpNoOverwriteMsg,
			}),
			Entry("should not add OTEL_EXPORTER_OTLP_ENDPOINT but log an issue if only OTEL_EXPORTER_OTLP_PROTOCOL is set correctly", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpProtocolName,
						Value: common.ProtocolHttpProtobuf,
					}},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: nil,
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
				expectedInstrumentationIssue: otelExporterOtlpNoOverwriteMsg,
				expectedLogMessage:           otelExporterOtlpNoOverwriteMsg,
			}),
		)

		DescribeTable("should remove OTEL_EXPORTER_OTLP_ENDPOINT/OTEL_EXPORTER_OTLP_PROTOCOL",
			func(testConfig otelExporterEnvVarsTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				clusterInstrumentationConfig := testConfig.clusterInstrumentationConfig
				if clusterInstrumentationConfig == nil {
					clusterInstrumentationConfig = clusterInstrumentationConfigWithInitContainer
				}
				modifier := NewResourceModifier(
					clusterInstrumentationConfig,
					DefaultNamespaceInstrumentationConfig,
					testActor,
					logger,
				)
				modifier.removeEnvironmentVariables(container)

				envVars := container.Env
				VerifyEnvVarsFromMap(testConfig.expectedEnvVars, envVars)
			},
			Entry("should remove OTEL_EXPORTER_OTLP_* when values match the current config", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpEndpointName,
						Value: OTelCollectorNodeLocalBaseUrlTest,
					},
					{
						Name:  envVarOtelExporterOtlpProtocolName,
						Value: common.ProtocolHttpProtobuf,
					}},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: nil,
					envVarOtelExporterOtlpProtocolName: nil,
				},
			}),
			Entry("should remove OTEL_EXPORTER_OTLP_* when current env var is service and new config is node-local", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpEndpointName,
						Value: OTelCollectorServiceBaseUrlTest,
					},
					{
						Name:  envVarOtelExporterOtlpProtocolName,
						Value: common.ProtocolHttpProtobuf,
					}},
				clusterInstrumentationConfig: clusterInstrumentationConfigWithInitContainer,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: nil,
					envVarOtelExporterOtlpProtocolName: nil,
				},
			}),
			Entry("should remove OTEL_EXPORTER_OTLP_* when current env var is node local and new config is service", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpEndpointName,
						Value: OTelCollectorNodeLocalBaseUrlTest,
					},
					{
						Name:  envVarOtelExporterOtlpProtocolName,
						Value: common.ProtocolHttpProtobuf,
					}},
				clusterInstrumentationConfig: clusterInstrumentationConfigWithServiceUrl,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: nil,
					envVarOtelExporterOtlpProtocolName: nil,
				},
			}),
			Entry("should leave OTEL_EXPORTER_OTLP_* in place when value is not a value that the operator would set", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpEndpointName,
						Value: "some-endpoint.tld",
					},
					{
						Name:  envVarOtelExporterOtlpProtocolName,
						Value: common.ProtocolGrpc,
					}},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: "some-endpoint.tld"},
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolGrpc},
				},
			}),
			Entry("should leave OTEL_EXPORTER_OTLP_* in place when only endpoint is set", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpEndpointName,
						Value: OTelCollectorNodeLocalBaseUrlTest,
					}},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: {Value: OTelCollectorNodeLocalBaseUrlTest},
					envVarOtelExporterOtlpProtocolName: nil,
				},
			}),
			Entry("should leave OTEL_EXPORTER_OTLP_* in place when only protocol is set", otelExporterEnvVarsTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name:  envVarOtelExporterOtlpProtocolName,
						Value: common.ProtocolHttpProtobuf,
					}},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelExporterOtlpEndpointName: nil,
					envVarOtelExporterOtlpProtocolName: {Value: common.ProtocolHttpProtobuf},
				},
			}),
		)

		type otelLogsExporterEnvVarTest struct {
			logCollectionEnabled         bool
			previousLogCollectionEnabled bool
			existingEnvVars              []corev1.EnvVar
			expectedEnvVar               *EnvVarExpectation
		}

		DescribeTable("should set OTEL_LOGS_EXPORTER=none when log collection is enabled in the namespace",
			func(testConfig otelLogsExporterEnvVarTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				nsConfig := DefaultNamespaceInstrumentationConfig
				nsConfig.LogCollectionEnabled = testConfig.logCollectionEnabled
				modifier := NewResourceModifier(clusterInstrumentationConfigWithInitContainer, nsConfig, testActor, logger)
				modifier.addOtelLogsExporterEnvVar(container)

				VerifyEnvVarsFromMap(
					map[string]*EnvVarExpectation{envVarOtelLogsExporterName: testConfig.expectedEnvVar},
					container.Env,
				)
			},
			Entry("should set OTEL_LOGS_EXPORTER=none when log collection is enabled and env var is absent", otelLogsExporterEnvVarTest{
				logCollectionEnabled: true,
				existingEnvVars:      nil,
				expectedEnvVar:       &EnvVarExpectation{Value: "none"},
			}),
			Entry("should not override an existing OTEL_LOGS_EXPORTER user value when log collection is enabled", otelLogsExporterEnvVarTest{
				logCollectionEnabled: true,
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelLogsExporterName,
					Value: "otlp",
				}},
				expectedEnvVar: &EnvVarExpectation{Value: "otlp"},
			}),
			Entry("should not override an existing OTEL_LOGS_EXPORTER=none set by the user when log collection is enabled", otelLogsExporterEnvVarTest{
				logCollectionEnabled: true,
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelLogsExporterName,
					Value: "none",
				}},
				expectedEnvVar: &EnvVarExpectation{Value: "none"},
			}),
			Entry("should not override OTEL_LOGS_EXPORTER set via ValueFrom when log collection is enabled", otelLogsExporterEnvVarTest{
				logCollectionEnabled: true,
				existingEnvVars: []corev1.EnvVar{{
					Name: envVarOtelLogsExporterName,
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					},
				}},
				expectedEnvVar: &EnvVarExpectation{ValueFrom: "metadata.name"},
			}),
			Entry("should not set OTEL_LOGS_EXPORTER when log collection is disabled", otelLogsExporterEnvVarTest{
				logCollectionEnabled: false,
				existingEnvVars:      nil,
				expectedEnvVar:       nil,
			}),
			Entry("should not override an existing OTEL_LOGS_EXPORTER user value when log collection is disabled", otelLogsExporterEnvVarTest{
				logCollectionEnabled: false,
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelLogsExporterName,
					Value: "otlp",
				}},
				expectedEnvVar: &EnvVarExpectation{Value: "otlp"},
			}),
		)

		DescribeTable("should remove OTEL_LOGS_EXPORTER on uninstrumentation only when we set it",
			func(testConfig otelLogsExporterEnvVarTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				nsConfig := DefaultNamespaceInstrumentationConfig
				nsConfig.PreviousLogCollectionEnabled = testConfig.previousLogCollectionEnabled
				modifier := NewResourceModifier(clusterInstrumentationConfigWithInitContainer, nsConfig, testActor, logger)
				modifier.removeOtelLogsExporterEnvVar(container)

				VerifyEnvVarsFromMap(
					map[string]*EnvVarExpectation{envVarOtelLogsExporterName: testConfig.expectedEnvVar},
					container.Env,
				)
			},
			Entry("should remove OTEL_LOGS_EXPORTER=none when we previously set it", otelLogsExporterEnvVarTest{
				previousLogCollectionEnabled: true,
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelLogsExporterName,
					Value: "none",
				}},
				expectedEnvVar: nil,
			}),
			Entry("should not remove a user-set OTEL_LOGS_EXPORTER value", otelLogsExporterEnvVarTest{
				previousLogCollectionEnabled: true,
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelLogsExporterName,
					Value: "otlp",
				}},
				expectedEnvVar: &EnvVarExpectation{Value: "otlp"},
			}),
			Entry("should not touch OTEL_LOGS_EXPORTER when log collection was not enabled previously", otelLogsExporterEnvVarTest{
				previousLogCollectionEnabled: false,
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelLogsExporterName,
					Value: "none",
				}},
				expectedEnvVar: &EnvVarExpectation{Value: "none"},
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
				actualResourceAttributes := FindEnvVarByName(envVars, envVarOTelInjectorResourceAttributesName)
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
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: nil,
				},
			}),
			Entry("should not add env vars if metadata is empty", objectMetaResourceAttributesTest{
				workloadLabels:      map[string]string{},
				workloadAnnotations: map[string]string{},
				podLabels:           map[string]string{},
				podAnnotations:      map[string]string{},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: nil,
				},
			}),
			Entry("should not add env vars if metadata is unrelated", objectMetaResourceAttributesTest{
				workloadLabels:      map[string]string{"a": "b"},
				workloadAnnotations: map[string]string{"a": "b"},
				podLabels:           map[string]string{"a": "b"},
				podAnnotations:      map[string]string{"a": "b"},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: nil,
				},
			}),
			Entry("should derive env vars from workload labels", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "workload-name",
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        {Value: "workload-name"},
					envVarOTelInjectorServiceNamespace:   {Value: "workload-part-of"},
					envVarOTelInjectorServiceVersionName: {Value: "workload-version"},
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
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   {Value: "workload-part-of"},
					envVarOTelInjectorServiceVersionName: {Value: "workload-version"},
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
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   {Value: "workload-part-of"},
					envVarOTelInjectorServiceVersionName: {Value: "workload-version"},
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
					envVarOTelInjectorServiceName:        {Value: "workload-name"},
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: {Value: "workload-version"},
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
					envVarOTelInjectorServiceName:        {Value: "workload-name"},
					envVarOTelInjectorServiceNamespace:   {Value: "workload-part-of"},
					envVarOTelInjectorServiceVersionName: nil,
				},
			}),
			Entry("resource.opentelemetry.io/service.name should win over app.kubernetes.io/name", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "app.kubernetes.io/name-value",
					util.AppKubernetesIoPartOfLabel:  "app.kubernetes.io/part-of-value",
					util.AppKubernetesIoVersionLabel: "app.kubernetes.io/version-value",
				},
				workloadAnnotations: map[string]string{
					"resource.opentelemetry.io/service.name": "resource.opentelemetry.io/service.name-value",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        {Value: "resource.opentelemetry.io/service.name-value"},
					envVarOTelInjectorServiceNamespace:   {Value: "app.kubernetes.io/part-of-value"},
					envVarOTelInjectorServiceVersionName: {Value: "app.kubernetes.io/version-value"},
				},
				expectedResourceAttributeEnvVar: []string{
					"service.name=resource.opentelemetry.io/service.name-value",
				},
			}),
			Entry("resource.opentelemetry.io/service.namespace should win over app.kubernetes.io/part-of", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "app.kubernetes.io/name-value",
					util.AppKubernetesIoPartOfLabel:  "app.kubernetes.io/part-of-value",
					util.AppKubernetesIoVersionLabel: "app.kubernetes.io/version-value",
				},
				workloadAnnotations: map[string]string{
					"resource.opentelemetry.io/service.namespace": "resource.opentelemetry.io/service.namespace-value",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        {Value: "app.kubernetes.io/name-value"},
					envVarOTelInjectorServiceNamespace:   {Value: "resource.opentelemetry.io/service.namespace-value"},
					envVarOTelInjectorServiceVersionName: {Value: "app.kubernetes.io/version-value"},
				},
				expectedResourceAttributeEnvVar: []string{
					"service.namespace=resource.opentelemetry.io/service.namespace-value",
				},
			}),
			Entry("resource.opentelemetry.io/service.version should win over app.kubernetes.io/version", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "app.kubernetes.io/name-value",
					util.AppKubernetesIoPartOfLabel:  "app.kubernetes.io/part-of-value",
					util.AppKubernetesIoVersionLabel: "app.kubernetes.io/version-value",
				},
				workloadAnnotations: map[string]string{
					"resource.opentelemetry.io/service.version": "resource.opentelemetry.io/service.version-value",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        {Value: "app.kubernetes.io/name-value"},
					envVarOTelInjectorServiceNamespace:   {Value: "app.kubernetes.io/part-of-value"},
					envVarOTelInjectorServiceVersionName: {Value: "resource.opentelemetry.io/service.version-value"},
				},
				expectedResourceAttributeEnvVar: []string{
					"service.version=resource.opentelemetry.io/service.version-value",
				},
			}),
			Entry("resource.opentelemetry.io/service.* should win over app.kubernetes.io/*", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "app.kubernetes.io/name-value",
					util.AppKubernetesIoPartOfLabel:  "app.kubernetes.io/part-of-value",
					util.AppKubernetesIoVersionLabel: "app.kubernetes.io/version-value",
				},
				workloadAnnotations: map[string]string{
					"resource.opentelemetry.io/service.name":      "resource.opentelemetry.io/service.name-value",
					"resource.opentelemetry.io/service.namespace": "resource.opentelemetry.io/service.namespace-value",
					"resource.opentelemetry.io/service.version":   "resource.opentelemetry.io/service.version-value",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        {Value: "resource.opentelemetry.io/service.name-value"},
					envVarOTelInjectorServiceNamespace:   {Value: "resource.opentelemetry.io/service.namespace-value"},
					envVarOTelInjectorServiceVersionName: {Value: "resource.opentelemetry.io/service.version-value"},
				},
				expectedResourceAttributeEnvVar: []string{
					"service.name=resource.opentelemetry.io/service.name-value",
					"service.namespace=resource.opentelemetry.io/service.namespace-value",
					"service.version=resource.opentelemetry.io/service.version-value",
				},
			}),
			Entry("resource.opentelemetry.io/service.* should be set independently of app.kubernetes.io/*", objectMetaResourceAttributesTest{
				workloadAnnotations: map[string]string{
					"resource.opentelemetry.io/service.name":      "resource.opentelemetry.io/service.name-value",
					"resource.opentelemetry.io/service.namespace": "resource.opentelemetry.io/service.namespace-value",
					"resource.opentelemetry.io/service.version":   "resource.opentelemetry.io/service.version-value",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        {Value: "resource.opentelemetry.io/service.name-value"},
					envVarOTelInjectorServiceNamespace:   {Value: "resource.opentelemetry.io/service.namespace-value"},
					envVarOTelInjectorServiceVersionName: {Value: "resource.opentelemetry.io/service.version-value"},
				},
				expectedResourceAttributeEnvVar: []string{
					"service.name=resource.opentelemetry.io/service.name-value",
					"service.namespace=resource.opentelemetry.io/service.namespace-value",
					"service.version=resource.opentelemetry.io/service.version-value",
				},
			}),
			Entry("should ignore workload labels if name is not set", objectMetaResourceAttributesTest{
				workloadLabels: map[string]string{
					util.AppKubernetesIoPartOfLabel:  "workload-part-of",
					util.AppKubernetesIoVersionLabel: "workload-version",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: nil,
				},
			}),
			Entry("should derive env vars from pod labels", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoNameLabel:    "pod-name",
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarOTelInjectorServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarOTelInjectorServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
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
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarOTelInjectorServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
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
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarOTelInjectorServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
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
					envVarOTelInjectorServiceName:        {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
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
					envVarOTelInjectorServiceName:        {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarOTelInjectorServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarOTelInjectorServiceVersionName: nil,
				},
			}),
			Entry("should ignore pod labels if name is not set", objectMetaResourceAttributesTest{
				podLabels: map[string]string{
					util.AppKubernetesIoPartOfLabel:  "pod-part-of",
					util.AppKubernetesIoVersionLabel: "pod-version",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: nil,
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
					envVarOTelInjectorServiceName:        {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoNameLabel)},
					envVarOTelInjectorServiceNamespace:   {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoPartOfLabel)},
					envVarOTelInjectorServiceVersionName: {ValueFrom: fmt.Sprintf("metadata.labels['%s']", util.AppKubernetesIoVersionLabel)},
				},
			}),
			Entry("should derive resource attributes from workload annotations", objectMetaResourceAttributesTest{
				workloadAnnotations: map[string]string{
					"resource.opentelemetry.io/workload.ra.1": "workload-value-1",
					"resource.opentelemetry.io/workload.ra.2": "workload-value-2",
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: nil,
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
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: nil,
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
					envVarOTelInjectorServiceName:        nil,
					envVarOTelInjectorServiceNamespace:   nil,
					envVarOTelInjectorServiceVersionName: nil,
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

		It("the DASH0_RESOURCE_ATTRIBUTES value must be stable against reordering of annotations", func() {
			workloadMeta1 := metav1.ObjectMeta{
				Annotations: map[string]string{
					"resource.opentelemetry.io/workload.key.1": "value 1",
					"resource.opentelemetry.io/workload.key.2": "value 2",
					"resource.opentelemetry.io/workload.key.3": "value 3",
				},
			}
			podMeta1 := metav1.ObjectMeta{
				Annotations: map[string]string{
					"resource.opentelemetry.io/pod.key.1": "value 4",
					"resource.opentelemetry.io/pod.key.2": "value 5",
					"resource.opentelemetry.io/pod.key.3": "value 6",
				},
			}
			container1 := &corev1.Container{}
			workloadModifier.addEnvironmentVariables(
				container1,
				&workloadMeta1,
				&podMeta1,
				logger,
			)

			// now re-order the annotations and generate the DASH0_RESOURCE_ATTRIBUTES value again
			workloadMeta2 := metav1.ObjectMeta{
				Annotations: map[string]string{
					"resource.opentelemetry.io/workload.key.3": "value 3",
					"resource.opentelemetry.io/workload.key.2": "value 2",
					"resource.opentelemetry.io/pod.key.1":      "value 4",
				},
			}
			podMeta2 := metav1.ObjectMeta{
				Annotations: map[string]string{
					"resource.opentelemetry.io/pod.key.3":      "value 6",
					"resource.opentelemetry.io/pod.key.2":      "value 5",
					"resource.opentelemetry.io/workload.key.1": "value 1",
				},
			}
			container2 := &corev1.Container{}
			workloadModifier.addEnvironmentVariables(
				container2,
				&workloadMeta2,
				&podMeta2,
				logger,
			)

			// Verify that the value of DASH0_RESOURCE_ATTRIBUTES is independent of the order in which annotations
			// are returned by the Kubernetes API:
			dash0ResourceAttributesValue1 := FindEnvVarByName(container1.Env, envVarOTelInjectorResourceAttributesName).Value
			dash0ResourceAttributesValue2 := FindEnvVarByName(container2.Env, envVarOTelInjectorResourceAttributesName).Value
			Expect(dash0ResourceAttributesValue1).To(Equal(dash0ResourceAttributesValue2))
		})

		type otelPropagatorsTest struct {
			existingEnvVars                       []corev1.EnvVar
			namespaceInstrumentationConfig        dash0v1beta1.NamespaceInstrumentationConfig
			expectedPreInstrumentationCheckResult bool
			expectedEnvVars                       map[string]*EnvVarExpectation
		}

		DescribeTable("should add or update OTEL_PROPAGATORS",
			func(testConfig otelPropagatorsTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				// addEnvironmentVariables/addOTelPropagatorsEnvVar and
				// otelPropagatorsEnvVarWillBeUpdatedForAtLeastOneContainer are called at different points in time, but
				// they deliberately share the exact same logic. We opportunistically test both methods here.
				preInstrumentationCheckResult := otelPropagatorsEnvVarWillBeUpdatedForAtLeastOneContainer([]corev1.Container{
					*container,
				}, testConfig.namespaceInstrumentationConfig)
				Expect(preInstrumentationCheckResult).To(Equal(testConfig.expectedPreInstrumentationCheckResult))

				NewResourceModifier(
					clusterInstrumentationConfigWithInitContainer,
					testConfig.namespaceInstrumentationConfig,
					testActor,
					logger,
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					PreviousTraceContextPropagators: new("something,else"),
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					PreviousTraceContextPropagators: new("tracecontext,baggage"),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
			Entry("should not add OTEL_PROPAGATORS if configured as empty string", otelPropagatorsTest{
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new(""),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
			Entry("should not add OTEL_PROPAGATORS if configured string is only whitespace", otelPropagatorsTest{
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new("   "),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
			Entry("should add OTEL_PROPAGATORS if configured and the env var does not exist on the container", otelPropagatorsTest{
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new("tracecontext,xray"),
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new("tracecontext,xray"),
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new("tracecontext,xray"),
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators:         new("tracecontext,xray"),
					PreviousTraceContextPropagators: new("something,else"),
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators:         new("tracecontext,xray"),
					PreviousTraceContextPropagators: new("jaeger,b3multi"),
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators:         new("tracecontext,xray"),
					PreviousTraceContextPropagators: new("tracecontext,xray"),
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new("tracecontext,xray"),
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
					clusterInstrumentationConfigWithInitContainer,
					testConfig.namespaceInstrumentationConfig,
					testActor,
					logger,
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new("tracecontext,xray"),
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new("tracecontext,xray"),
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
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new("tracecontext,xray"),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
			Entry("should do nothing if env var is not set", otelPropagatorsTest{
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					TraceContextPropagators: new("tracecontext,xray"),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					util.OtelPropagatorsEnvVarName: nil,
				},
			}),
		)

		type captureSqlQueryParametersTest struct {
			existingEnvVars                       []corev1.EnvVar
			namespaceInstrumentationConfig        dash0v1beta1.NamespaceInstrumentationConfig
			expectedPreInstrumentationCheckResult bool
			expectedEnvVars                       map[string]*EnvVarExpectation
		}

		DescribeTable("should add or remove SQL query parameter capture env vars",
			func(testConfig captureSqlQueryParametersTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				preInstrumentationCheckResult := captureSqlQueryParametersEnvVarWillBeUpdatedForAtLeastOneContainer([]corev1.Container{
					*container,
				}, testConfig.namespaceInstrumentationConfig)
				Expect(preInstrumentationCheckResult).To(Equal(testConfig.expectedPreInstrumentationCheckResult))

				NewResourceModifier(
					clusterInstrumentationConfigWithInitContainer,
					testConfig.namespaceInstrumentationConfig,
					testActor,
					logger,
				).addEnvironmentVariables(
					container,
					&metav1.ObjectMeta{},
					&metav1.ObjectMeta{},
					logger,
				)

				VerifyEnvVarsFromMap(testConfig.expectedEnvVars, container.Env)
			},
			Entry("should not add the env vars if not configured", captureSqlQueryParametersTest{
				namespaceInstrumentationConfig:        DefaultNamespaceInstrumentationConfig,
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: nil,
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     nil,
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        nil,
				},
			}),
			Entry("should not add the env vars if configured to false", captureSqlQueryParametersTest{
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(false),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: nil,
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     nil,
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        nil,
				},
			}),
			Entry("should add all env vars if configured to true and none exist on the container", captureSqlQueryParametersTest{
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "true"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "true"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "true"},
				},
			}),
			Entry("should do nothing if configured to true and all env vars are already set to true", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName, Value: "true"},
				},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "true"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "true"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "true"},
				},
			}),
			Entry("should apply per-env-var decisions independently when configured to true (mix of true, user-set non-true and ValueFrom)", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "false"},
					{
						Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName,
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "field.path"},
						},
					},
				},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "true"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "false"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {ValueFrom: "field.path"},
				},
			}),
			Entry("should not update JDBC env var if already set to true, but should add missing .NET env vars", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName,
					Value: "true",
				}},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "true"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "true"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "true"},
				},
			}),
			Entry("should not update .NET SqlClient env var if already set to true, but should add missing JDBC and EF Core env vars", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName,
					Value: "true",
				}},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "true"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "true"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "true"},
				},
			}),
			Entry("should leave existing JDBC env var with non-true value alone if configured to true (user-set, no previous), but should add .NET env vars", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName,
					Value: "false",
				}},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "false"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "true"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "true"},
				},
			}),
			Entry("should leave existing .NET EF Core env var with non-true value alone if configured to true (user-set, no previous), but should add other env vars", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName,
					Value: "false",
				}},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "true"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "true"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "false"},
				},
			}),
			Entry("should leave existing JDBC env var via ValueFrom alone even if configured to true, but should add .NET env vars", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{{
					Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName,
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "field.path",
						},
					},
				}},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {ValueFrom: "field.path"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "true"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "true"},
				},
			}),
			Entry("should leave existing .NET SqlClient env var via ValueFrom alone even if configured to true, but should add other env vars", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{{
					Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName,
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "field.path",
						},
					},
				}},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "true"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {ValueFrom: "field.path"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "true"},
				},
			}),
			Entry("should remove existing env vars if not configured anymore and previous was true and value matches", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName, Value: "true"},
				},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					PreviousCaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: nil,
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     nil,
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        nil,
				},
			}),
			Entry("should remove existing env vars if configured to false and previous was true and value matches", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName, Value: "true"},
				},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters:         ptr.To(false),
					PreviousCaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: nil,
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     nil,
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        nil,
				},
			}),
			Entry("should not remove existing env vars if previous was true but value differs (user changed them)", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "false"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "false"},
					{Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName, Value: "false"},
				},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					PreviousCaptureSqlQueryParameters: ptr.To(true),
				},
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "false"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "false"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "false"},
				},
			}),
			Entry("should not remove existing env vars if no previous setting (env vars are user-set)", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName, Value: "true"},
				},
				namespaceInstrumentationConfig:        DefaultNamespaceInstrumentationConfig,
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "true"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "true"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "true"},
				},
			}),
		)

		DescribeTable("should remove SQL query parameter capture env vars when uninstrumenting workloads",
			func(testConfig captureSqlQueryParametersTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				NewResourceModifier(
					clusterInstrumentationConfigWithInitContainer,
					testConfig.namespaceInstrumentationConfig,
					testActor,
					logger,
				).removeEnvironmentVariables(container)

				VerifyEnvVarsFromMap(testConfig.expectedEnvVars, container.Env)
			},
			Entry("should not remove any env var if not configured", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName, Value: "true"},
				},
				namespaceInstrumentationConfig: DefaultNamespaceInstrumentationConfig,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "true"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "true"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "true"},
				},
			}),
			Entry("should not remove any env var if configured but current values differ from what we would set", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "false"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "false"},
					{Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName, Value: "false"},
				},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {Value: "false"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "false"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {Value: "false"},
				},
			}),
			Entry("should not remove any env var if configured but existing env vars use ValueFrom", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{
						Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName,
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "field.path"},
						},
					},
					{
						Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName,
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "field.path"},
						},
					},
					{
						Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName,
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "field.path"},
						},
					},
				},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: {ValueFrom: "field.path"},
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {ValueFrom: "field.path"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {ValueFrom: "field.path"},
				},
			}),
			Entry("should remove env vars individually based on per-env-var state (mix of matching, user-set and ValueFrom)", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "false"},
					{
						Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName,
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "field.path"},
						},
					},
				},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: nil,
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     {Value: "false"},
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        {ValueFrom: "field.path"},
				},
			}),
			Entry("should remove all env vars if configured and current values match what we would set", captureSqlQueryParametersTest{
				existingEnvVars: []corev1.EnvVar{
					{Name: envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName, Value: "true"},
					{Name: envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName, Value: "true"},
				},
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: nil,
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     nil,
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        nil,
				},
			}),
			Entry("should do nothing if env vars are not set", captureSqlQueryParametersTest{
				namespaceInstrumentationConfig: dash0v1beta1.NamespaceInstrumentationConfig{
					CaptureSqlQueryParameters: ptr.To(true),
				},
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInstrumentationJdbcExperimentalCaptureQueryParametersName: nil,
					envVarOtelDotnetExperimentalSqlClientCaptureQueryParametersName:     nil,
					envVarOtelDotnetExperimentalEfCoreCaptureQueryParametersName:        nil,
				},
			}),
		)

		type pythonAutoInstrumentationTest struct {
			existingEnvVars                       []corev1.EnvVar
			clusterInstrumentationConfig          *util.ClusterInstrumentationConfig
			expectedPreInstrumentationCheckResult bool
			expectedEnvVars                       map[string]*EnvVarExpectation
		}

		DescribeTable("should update Python auto-instrumentation support",
			func(testConfig pythonAutoInstrumentationTest) {
				container := &corev1.Container{}
				if testConfig.existingEnvVars != nil {
					container.Env = testConfig.existingEnvVars
				}

				preInstrumentationCheckResult := otelInjectorConfEnvVarWillBeUpdatedForAtLeastOneContainer([]corev1.Container{
					*container,
				}, testConfig.clusterInstrumentationConfig)
				Expect(preInstrumentationCheckResult).To(Equal(testConfig.expectedPreInstrumentationCheckResult))

				NewResourceModifier(
					testConfig.clusterInstrumentationConfig,
					dash0v1beta1.NamespaceInstrumentationConfig{},
					testActor,
					logger,
				).addEnvironmentVariables(
					container,
					&metav1.ObjectMeta{},
					&metav1.ObjectMeta{},
					logger,
				)

				envVars := container.Env
				VerifyEnvVarsFromMap(testConfig.expectedEnvVars, envVars)
			},
			Entry("should change OTEL_INJECTOR_CONFIG_FILE to injector-with-python.conf when enabling Python auto-instrumentation", pythonAutoInstrumentationTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelInjectorConfigFileName,
					Value: envVarOtelInjectorConfigFileValue,
				}},
				clusterInstrumentationConfig: util.NewClusterInstrumentationConfig(
					TestImages,
					PossibleCollectorUrlsTest,
					OTelCollectorNodeLocalBaseUrlTest,
					util.ExtraConfigDefaults,
					cluster.ResolvedInstrumentationDeliveryInitContainer,
					nil,
					false,
					true,
				),
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInjectorConfigFileName: {Value: envVarOtelInjectorConfigFilePythonEnabledValue},
				},
			}),
			Entry("should do nothing if Python auto-instrumentation is already enabled for the container", pythonAutoInstrumentationTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelInjectorConfigFileName,
					Value: envVarOtelInjectorConfigFilePythonEnabledValue,
				}},
				clusterInstrumentationConfig: util.NewClusterInstrumentationConfig(
					TestImages,
					PossibleCollectorUrlsTest,
					OTelCollectorNodeLocalBaseUrlTest,
					util.ExtraConfigDefaults,
					cluster.ResolvedInstrumentationDeliveryInitContainer,
					nil,
					false,
					true,
				),
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInjectorConfigFileName: {Value: envVarOtelInjectorConfigFilePythonEnabledValue},
				},
			}),
			Entry("should change OTEL_INJECTOR_CONFIG_FILE to injector.conf when disabling Python auto-instrumentation", pythonAutoInstrumentationTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelInjectorConfigFileName,
					Value: envVarOtelInjectorConfigFilePythonEnabledValue,
				}},
				clusterInstrumentationConfig: util.NewClusterInstrumentationConfig(
					TestImages,
					PossibleCollectorUrlsTest,
					OTelCollectorNodeLocalBaseUrlTest,
					util.ExtraConfigDefaults,
					cluster.ResolvedInstrumentationDeliveryInitContainer,
					nil,
					false,
					false,
				),
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInjectorConfigFileName: {Value: envVarOtelInjectorConfigFileValue},
				},
			}),
			Entry("should do nothing if Python auto-instrumentation is already disabled for the container", pythonAutoInstrumentationTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelInjectorConfigFileName,
					Value: envVarOtelInjectorConfigFileValue,
				}},
				clusterInstrumentationConfig: util.NewClusterInstrumentationConfig(
					TestImages,
					PossibleCollectorUrlsTest,
					OTelCollectorNodeLocalBaseUrlTest,
					util.ExtraConfigDefaults,
					cluster.ResolvedInstrumentationDeliveryInitContainer,
					nil,
					false,
					false,
				),
				expectedPreInstrumentationCheckResult: false,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInjectorConfigFileName: {Value: envVarOtelInjectorConfigFileValue},
				},
			}),
			Entry("should change OTEL_INJECTOR_CONFIG_FILE to injector-with-python.conf if the existing env var uses ValueFrom", pythonAutoInstrumentationTest{
				existingEnvVars: []corev1.EnvVar{{
					Name: envVarOtelInjectorConfigFileName,
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "whatever",
						},
					},
				}},
				clusterInstrumentationConfig: util.NewClusterInstrumentationConfig(
					TestImages,
					PossibleCollectorUrlsTest,
					OTelCollectorNodeLocalBaseUrlTest,
					util.ExtraConfigDefaults,
					cluster.ResolvedInstrumentationDeliveryInitContainer,
					nil,
					false,
					true,
				),
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInjectorConfigFileName: {Value: envVarOtelInjectorConfigFilePythonEnabledValue},
				},
			}),
			Entry("should change OTEL_INJECTOR_CONFIG_FILE to injector.conf if the existing env var uses ValueFrom", pythonAutoInstrumentationTest{
				existingEnvVars: []corev1.EnvVar{{
					Name: envVarOtelInjectorConfigFileName,
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "whatever",
						},
					},
				}},
				clusterInstrumentationConfig: util.NewClusterInstrumentationConfig(
					TestImages,
					PossibleCollectorUrlsTest,
					OTelCollectorNodeLocalBaseUrlTest,
					util.ExtraConfigDefaults,
					cluster.ResolvedInstrumentationDeliveryInitContainer,
					nil,
					false,
					false,
				),
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInjectorConfigFileName: {Value: envVarOtelInjectorConfigFileValue},
				},
			}),
			Entry("should set OTEL_INJECTOR_CONFIG_FILE to injector-with-python.conf if the env var does not exist", pythonAutoInstrumentationTest{
				clusterInstrumentationConfig: util.NewClusterInstrumentationConfig(
					TestImages,
					PossibleCollectorUrlsTest,
					OTelCollectorNodeLocalBaseUrlTest,
					util.ExtraConfigDefaults,
					cluster.ResolvedInstrumentationDeliveryInitContainer,
					nil,
					false,
					true,
				),
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInjectorConfigFileName: {Value: envVarOtelInjectorConfigFilePythonEnabledValue},
				},
			}),
			Entry("should set OTEL_INJECTOR_CONFIG_FILE to injector.conf if the existing env var is empty", pythonAutoInstrumentationTest{
				existingEnvVars: []corev1.EnvVar{{
					Name:  envVarOtelInjectorConfigFileName,
					Value: "",
				}},
				clusterInstrumentationConfig: util.NewClusterInstrumentationConfig(
					TestImages,
					PossibleCollectorUrlsTest,
					OTelCollectorNodeLocalBaseUrlTest,
					util.ExtraConfigDefaults,
					cluster.ResolvedInstrumentationDeliveryInitContainer,
					nil,
					false,
					false,
				),
				expectedPreInstrumentationCheckResult: true,
				expectedEnvVars: map[string]*EnvVarExpectation{
					envVarOtelInjectorConfigFileName: {Value: envVarOtelInjectorConfigFileValue},
				},
			}),
		)

		DescribeTable("migrate the legacy Dash0 injector log level",
			func(testConfig envVarModificationTest) {
				container := &corev1.Container{Env: testConfig.envVars}
				workloadModifier.migrateLegacyInjectorLogLevel(container)
				Expect(container.Env).To(HaveLen(len(testConfig.expectations)))
				VerifyEnvVarsFromMap(
					testConfig.expectations,
					container.Env,
				)
			},
			Entry("should migrate the legacy Dash0 injector log level", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "DASH0_INJECTOR_LOG_LEVEL", Value: "none"},
				},
				expectations: map[string]*EnvVarExpectation{
					"OTEL_INJECTOR_LOG_LEVEL": {Value: "none"},
				},
			}),
			Entry("should not migrate the legacy Dash0 injector log level if OTEL_INJECTOR_LOG_LEVEL is already set", envVarModificationTest{
				envVars: []corev1.EnvVar{
					{Name: "DASH0_INJECTOR_LOG_LEVEL", Value: "none"},
					{Name: "OTEL_INJECTOR_LOG_LEVEL", Value: "info"},
				},
				expectations: map[string]*EnvVarExpectation{
					"DASH0_INJECTOR_LOG_LEVEL": {Value: "none"},
					"OTEL_INJECTOR_LOG_LEVEL":  {Value: "info"},
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
