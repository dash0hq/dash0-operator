// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ContainerExpectations struct {
	VolumeMounts                             int
	Dash0VolumeMountIdx                      int
	EnvVars                                  int
	NodeOptionsEnvVarIdx                     int
	NodeOptionsValue                         string
	NodeOptionsUsesValueFrom                 bool
	Dash0CollectorBaseUrlEnvVarIdx           int
	Dash0CollectorBaseUrlEnvVarExpectedValue string
}

type PodSpecExpectations struct {
	Volumes               int
	Dash0VolumeIdx        int
	InitContainers        int
	Dash0InitContainerIdx int
	Containers            []ContainerExpectations
}

var (
	BasicPodSpecExpectations = PodSpecExpectations{
		Volumes:               1,
		Dash0VolumeIdx:        0,
		InitContainers:        1,
		Dash0InitContainerIdx: 0,
		Containers: []ContainerExpectations{{
			VolumeMounts:                   1,
			Dash0VolumeMountIdx:            0,
			EnvVars:                        2,
			NodeOptionsEnvVarIdx:           0,
			Dash0CollectorBaseUrlEnvVarIdx: 1,
			Dash0CollectorBaseUrlEnvVarExpectedValue://
			"http://dash0-opentelemetry-collector-daemonset.test-namespace.svc.cluster.local:4318",
		}},
	}
)

func VerifyModifiedCronJob(resource *batchv1.CronJob, expectations PodSpecExpectations) {
	verifyModifiedPodSpec(resource.Spec.JobTemplate.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
}

func VerifyModifiedDaemonSet(resource *appsv1.DaemonSet, expectations PodSpecExpectations) {
	verifyModifiedPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
}

func VerifyModifiedDeployment(resource *appsv1.Deployment, expectations PodSpecExpectations) {
	verifyModifiedPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
}

func VerifyModifiedJob(resource *batchv1.Job, expectations PodSpecExpectations) {
	verifyModifiedPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
}

func VerifyUnmodifiedJob(resource *batchv1.Job) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyLabelsAfterFailureToModify(resource.ObjectMeta)
}

func VerifyModifiedReplicaSet(resource *appsv1.ReplicaSet, expectations PodSpecExpectations) {
	verifyModifiedPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
}

func VerifyUnmodifiedReplicaSet(resource *appsv1.ReplicaSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0Labels(resource.ObjectMeta)
}

func VerifyModifiedStatefulSet(resource *appsv1.StatefulSet, expectations PodSpecExpectations) {
	verifyModifiedPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
}

func verifyModifiedPodSpec(podSpec corev1.PodSpec, expectations PodSpecExpectations) {
	Expect(podSpec.Volumes).To(HaveLen(expectations.Volumes))
	for i, volume := range podSpec.Volumes {
		if i == expectations.Dash0VolumeIdx {
			Expect(volume.Name).To(Equal("dash0-instrumentation"))
			Expect(volume.EmptyDir).NotTo(BeNil())
		} else {
			Expect(volume.Name).To(Equal(fmt.Sprintf("test-volume-%d", i)))
		}
	}

	Expect(podSpec.InitContainers).To(HaveLen(expectations.InitContainers))
	for i, initContainer := range podSpec.InitContainers {
		if i == expectations.Dash0InitContainerIdx {
			Expect(initContainer.Name).To(Equal("dash0-instrumentation"))
			Expect(initContainer.Image).To(MatchRegexp("^dash0-instrumentation:\\d+\\.\\d+\\.\\d+"))
			Expect(initContainer.Env).To(HaveLen(1))
			Expect(initContainer.Env).To(ContainElement(MatchEnvVar("DASH0_INSTRUMENTATION_FOLDER_DESTINATION", "/opt/dash0")))
			Expect(initContainer.SecurityContext).NotTo(BeNil())
			Expect(initContainer.VolumeMounts).To(HaveLen(1))
			Expect(initContainer.VolumeMounts).To(ContainElement(MatchVolumeMount("dash0-instrumentation", "/opt/dash0")))
		} else {
			Expect(initContainer.Name).To(Equal(fmt.Sprintf("test-init-container-%d", i)))
			Expect(initContainer.Env).To(HaveLen(i + 1))
		}
	}

	Expect(podSpec.Containers).To(HaveLen(len(expectations.Containers)))
	for i, container := range podSpec.Containers {
		Expect(container.Name).To(Equal(fmt.Sprintf("test-container-%d", i)))
		containerExpectations := expectations.Containers[i]
		Expect(container.VolumeMounts).To(HaveLen(containerExpectations.VolumeMounts))
		for i, volumeMount := range container.VolumeMounts {
			if i == containerExpectations.Dash0VolumeMountIdx {
				Expect(volumeMount.Name).To(Equal("dash0-instrumentation"))
				Expect(volumeMount.MountPath).To(Equal("/opt/dash0"))
			} else {
				Expect(volumeMount.Name).To(Equal(fmt.Sprintf("test-volume-%d", i)))
			}
		}
		Expect(container.Env).To(HaveLen(containerExpectations.EnvVars))
		for i, envVar := range container.Env {
			if i == containerExpectations.NodeOptionsEnvVarIdx {
				Expect(envVar.Name).To(Equal("NODE_OPTIONS"))
				if containerExpectations.NodeOptionsUsesValueFrom {
					Expect(envVar.Value).To(BeEmpty())
					Expect(envVar.ValueFrom).To(Not(BeNil()))
				} else if containerExpectations.NodeOptionsValue != "" {
					Expect(envVar.Value).To(Equal(containerExpectations.NodeOptionsValue))
				} else {
					Expect(envVar.Value).To(Equal(
						"--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js",
					))
				}
			} else if i == containerExpectations.Dash0CollectorBaseUrlEnvVarIdx {
				Expect(envVar.Name).To(Equal("DASH0_OTEL_COLLECTOR_BASE_URL"))
				Expect(envVar.Value).To(Equal(containerExpectations.Dash0CollectorBaseUrlEnvVarExpectedValue))
				Expect(envVar.ValueFrom).To(BeNil())
			} else {
				Expect(envVar.Name).To(Equal(fmt.Sprintf("TEST%d", i)))
			}
		}
	}
}

func verifyUnmodifiedPodSpec(podSpec corev1.PodSpec) {
	Expect(podSpec.Volumes).To(BeEmpty())
	Expect(podSpec.InitContainers).To(BeEmpty())
	Expect(podSpec.Containers).To(HaveLen(1))
	for i, container := range podSpec.Containers {
		Expect(container.Name).To(Equal(fmt.Sprintf("test-container-%d", i)))
		Expect(container.VolumeMounts).To(BeEmpty())
		Expect(container.Env).To(BeEmpty())
	}
}

func verifyLabelsAfterSuccessfulModification(meta metav1.ObjectMeta) {
	Expect(meta.Labels["dash0.instrumented"]).To(Equal("true"))
	Expect(meta.Labels["dash0.operator.version"]).To(Equal("1.2.3"))
	Expect(meta.Labels["dash0.initcontainer.image.version"]).To(Equal("4.5.6"))
}

func verifyLabelsAfterFailureToModify(meta metav1.ObjectMeta) {
	Expect(meta.Labels["dash0.instrumented"]).To(Equal("false"))
	Expect(meta.Labels["dash0.operator.version"]).To(Equal("1.2.3"))
	Expect(meta.Labels["dash0.initcontainer.image.version"]).To(Equal("4.5.6"))
}

func verifyNoDash0Labels(meta metav1.ObjectMeta) {
	Expect(meta.Labels["dash0.instrumented"]).To(Equal(""))
	Expect(meta.Labels["dash0.operator.version"]).To(Equal(""))
	Expect(meta.Labels["dash0.initcontainer.image.version"]).To(Equal(""))
}
