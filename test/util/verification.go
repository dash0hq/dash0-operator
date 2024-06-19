// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/util"
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
	BasicInstrumentedPodSpecExpectations = PodSpecExpectations{
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
			"http://dash0-operator-opentelemetry-collector.dash0-operator-system.svc.cluster.local:4318",
		}},
	}
)

func VerifyModifiedCronJob(resource *batchv1.CronJob, expectations PodSpecExpectations) {
	verifyPodSpec(resource.Spec.JobTemplate.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
}

func VerifyUnmodifiedCronJob(resource *batchv1.CronJob) {
	verifyUnmodifiedPodSpec(resource.Spec.JobTemplate.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
}

func VerifyCronJobWithOptOutLabel(resource *batchv1.CronJob) {
	verifyUnmodifiedPodSpec(resource.Spec.JobTemplate.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
}

func VerifyModifiedDaemonSet(resource *appsv1.DaemonSet, expectations PodSpecExpectations) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
}

func VerifyUnmodifiedDaemonSet(resource *appsv1.DaemonSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyDaemonSetWithOptOutLabel(resource *appsv1.DaemonSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyModifiedDeployment(resource *appsv1.Deployment, expectations PodSpecExpectations) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
}

func VerifyUnmodifiedDeployment(resource *appsv1.Deployment) {
	VerifyUnmodifiedDeploymentEventually(Default, resource)
}

func VerifyUnmodifiedDeploymentEventually(g Gomega, resource *appsv1.Deployment) {
	verifyUnmodifiedPodSpecEventually(g, resource.Spec.Template.Spec)
	verifyNoDash0LabelsEventually(g, resource.ObjectMeta)
	verifyNoDash0LabelsEventually(g, resource.Spec.Template.ObjectMeta)
}

func VerifyRevertedDeployment(resource *appsv1.Deployment, expectations PodSpecExpectations) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyDeploymentWithOptOutLabel(resource *appsv1.Deployment) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyModifiedJob(resource *batchv1.Job, expectations PodSpecExpectations) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
}

func VerifyImmutableJobCouldNotBeModified(resource *batchv1.Job) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsAfterFailureToModify(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyUnmodifiedJob(resource *batchv1.Job) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyJobWithOptOutLabel(resource *batchv1.Job) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyModifiedPod(resource *corev1.Pod, expectations PodSpecExpectations) {
	verifyPodSpec(resource.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
}

func VerifyUnmodifiedPod(resource *corev1.Pod) {
	verifyUnmodifiedPodSpec(resource.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
}

func VerifyPodWithOptOutLabel(resource *corev1.Pod) {
	verifyUnmodifiedPodSpec(resource.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
}

func VerifyModifiedReplicaSet(resource *appsv1.ReplicaSet, expectations PodSpecExpectations) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
}

func VerifyUnmodifiedReplicaSet(resource *appsv1.ReplicaSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyReplicaSetWithOptOutLabel(resource *appsv1.ReplicaSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyModifiedStatefulSet(resource *appsv1.StatefulSet, expectations PodSpecExpectations) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
}

func VerifyUnmodifiedStatefulSet(resource *appsv1.StatefulSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func VerifyStatefulSetWithOptOutLabel(resource *appsv1.StatefulSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
}

func verifyPodSpec(podSpec corev1.PodSpec, expectations PodSpecExpectations) {
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
			Expect(initContainer.Image).To(Equal("some-registry.com:1234/dash0-instrumentation:4.5.6"))
			Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
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
	verifyUnmodifiedPodSpecEventually(Default, podSpec)
}

func verifyUnmodifiedPodSpecEventually(g Gomega, podSpec corev1.PodSpec) {
	g.Expect(podSpec.Volumes).To(BeEmpty())
	g.Expect(podSpec.InitContainers).To(BeEmpty())
	g.Expect(podSpec.Containers).To(HaveLen(1))
	for i, container := range podSpec.Containers {
		g.Expect(container.Name).To(Equal(fmt.Sprintf("test-container-%d", i)))
		g.Expect(container.VolumeMounts).To(BeEmpty())
		g.Expect(container.Env).To(BeEmpty())
	}
}

func verifyLabelsAfterSuccessfulModification(meta metav1.ObjectMeta) {
	Expect(meta.Labels["dash0.com/instrumented"]).To(Equal("true"))
	Expect(meta.Labels["dash0.com/operator-image"]).To(Equal("some-registry.com_1234_dash0-operator-controller_1.2.3"))
	Expect(meta.Labels["dash0.com/init-container-image"]).To(Equal("some-registry.com_1234_dash0-instrumentation_4.5.6"))
	Expect(meta.Labels["dash0.com/instrumented-by"]).NotTo(Equal(""))
	Expect(meta.Labels["dash0.com/enable"]).To(Equal(""))
}

func verifyLabelsAfterFailureToModify(meta metav1.ObjectMeta) {
	Expect(meta.Labels["dash0.com/instrumented"]).To(Equal("false"))
	Expect(meta.Labels["dash0.com/operator-image"]).To(Equal("some-registry.com_1234_dash0-operator-controller_1.2.3"))
	Expect(meta.Labels["dash0.com/init-container-image"]).To(Equal("some-registry.com_1234_dash0-instrumentation_4.5.6"))
	Expect(meta.Labels["dash0.com/instrumented-by"]).NotTo(Equal(""))
	Expect(meta.Labels["dash0.com/webhook-ignore-once"]).To(Equal(""))
	Expect(meta.Labels["dash0.com/enable"]).To(Equal(""))
}

func verifyNoDash0Labels(meta metav1.ObjectMeta) {
	verifyNoDash0LabelsEventually(Default, meta)
}

func verifyNoDash0LabelsEventually(g Gomega, meta metav1.ObjectMeta) {
	g.Expect(meta.Labels["dash0.com/instrumented"]).To(Equal(""))
	g.Expect(meta.Labels["dash0.com/operator-image"]).To(Equal(""))
	g.Expect(meta.Labels["dash0.com/init-container-image"]).To(Equal(""))
	g.Expect(meta.Labels["dash0.com/instrumented-by"]).To(Equal(""))
	g.Expect(meta.Labels["dash0.com/enable"]).To(Equal(""))
}

func verifyLabelsForOptOutWorkload(meta metav1.ObjectMeta) {
	Expect(meta.Labels["dash0.com/instrumented"]).To(Equal(""))
	Expect(meta.Labels["dash0.com/operator-image"]).To(Equal(""))
	Expect(meta.Labels["dash0.com/init-container-image"]).To(Equal(""))
	Expect(meta.Labels["dash0.com/instrumented-by"]).To(Equal(""))
	Expect(meta.Labels["dash0.com/webhook-ignore-once"]).To(Equal(""))
	Expect(meta.Labels["dash0.com/enable"]).To(Equal("false"))
}

func VerifyWebhookIgnoreOnceLabelIsPresent(objectMeta *metav1.ObjectMeta) {
	VerifyWebhookIgnoreOnceLabelIsPresentEventually(Default, objectMeta)
}

func VerifyWebhookIgnoreOnceLabelIsPresentEventually(g Gomega, objectMeta *metav1.ObjectMeta) {
	g.Expect(objectMeta.Labels["dash0.com/webhook-ignore-once"]).To(Equal("true"))
}

func VerifyWebhookIgnoreOnceLabelIsAbesent(objectMeta *metav1.ObjectMeta) {
	Expect(objectMeta.Labels["dash0.com/webhook-ignore-once"]).To(Equal(""))
}

func VerifyNoEvents(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
) {
	allEvents, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(allEvents.Items).To(BeEmpty())
}

func VerifySuccessfulInstrumentationEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	eventSource string,
) {
	verifyEvent(
		ctx,
		clientset,
		Default,
		namespace,
		resourceName,
		util.ReasonSuccessfulInstrumentation,
		fmt.Sprintf("Dash0 instrumentation of this workload by the %s has been successful.", eventSource),
	)
}

func VerifyNoInstrumentationNecessaryEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	eventSource string,
) {
	verifyEvent(
		ctx,
		clientset,
		Default,
		namespace,
		resourceName,
		util.ReasonNoInstrumentationNecessary,
		fmt.Sprintf(
			"Dash0 instrumentation was already present on this workload, or the workload is part of a higher order "+
				"workload that will be instrumented, no modification by the %s is necessary.", eventSource),
	)
}

func VerifyFailedInstrumentationEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	message string,
) {
	verifyEvent(
		ctx,
		clientset,
		Default,
		namespace,
		resourceName,
		util.ReasonFailedInstrumentation,
		message,
	)
}

func VerifySuccessfulUninstrumentationEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	eventSource string,
) {
	VerifySuccessfulUninstrumentationEventEventually(ctx, clientset, Default, namespace, resourceName, eventSource)
}

func VerifySuccessfulUninstrumentationEventEventually(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	g Gomega,
	namespace string,
	resourceName string,
	eventSource string,
) {
	verifyEvent(
		ctx,
		clientset,
		g,
		namespace,
		resourceName,
		util.ReasonSuccessfulUninstrumentation,
		fmt.Sprintf("The %s successfully removed the Dash0 instrumentation from this workload.", eventSource),
	)
}

func VerifyFailedUninstrumentationEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	message string,
) {
	verifyEvent(
		ctx,
		clientset,
		Default,
		namespace,
		resourceName,
		util.ReasonFailedUninstrumentation,
		message,
	)
}

func VerifyNoUninstrumentationNecessaryEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	message string,
) {
	verifyEvent(
		ctx,
		clientset,
		Default,
		namespace,
		resourceName,
		util.ReasonNoUninstrumentationNecessary,
		message,
	)
}

func verifyEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	g Gomega,
	namespace string,
	resourceName string,
	reason util.Reason,
	message string,
) {
	allEvents, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(allEvents.Items).To(HaveLen(1))
	g.Expect(allEvents.Items).To(
		ContainElement(
			MatchEvent(
				namespace,
				resourceName,
				reason,
				message,
			)))
}
