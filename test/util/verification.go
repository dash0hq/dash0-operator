// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type EnvVarExpectation struct {
	Value                         string
	ValueFrom                     string
	UnorderedCommaSeparatedValues []string
}

type ContainerExpectations struct {
	ContainerName       string
	VolumeMounts        int
	Dash0VolumeMountIdx int
	EnvVars             map[string]*EnvVarExpectation
}

type PodSpecExpectations struct {
	Volumes               int
	Dash0VolumeIdx        int
	InitContainers        int
	Dash0InitContainerIdx int
	Containers            []ContainerExpectations
}

type VerifyOpts struct {
	VerifyManagedFields bool
	ExpectManagedFields bool
}

const (
	eventTimeout = 1 * time.Second

	safeToEvictLocalVolumesAnnotationName = "cluster-autoscaler.kubernetes.io/safe-to-evict-local-volumes"
)

var (
	ExpectManagedFields = VerifyOpts{
		VerifyManagedFields: true,
		ExpectManagedFields: true,
	}
	IgnoreManagedFields = VerifyOpts{
		VerifyManagedFields: false,
	}
	VerifyNoManagedFields = VerifyOpts{
		VerifyManagedFields: true,
		ExpectManagedFields: false,
	}
)

func BasicInstrumentedPodSpecExpectations() PodSpecExpectations {
	return PodSpecExpectations{
		Volumes:               1,
		Dash0VolumeIdx:        0,
		InitContainers:        1,
		Dash0InitContainerIdx: 0,
		Containers: []ContainerExpectations{{
			ContainerName:       "test-container-0",
			VolumeMounts:        1,
			Dash0VolumeMountIdx: 0,
			EnvVars: map[string]*EnvVarExpectation{
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
				"LD_PRELOAD": {
					Value: "/__otel_auto_instrumentation/injector/libotelinject.so",
				},
				"OTEL_INJECTOR_CONFIG_FILE": {
					Value: "/__otel_auto_instrumentation/injector/otelinject.conf",
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
		}},
	}
}

func VerifyModifiedCronJob(resource *batchv1.CronJob, expectations PodSpecExpectations, opts ...VerifyOpts) {
	verifyPodSpec(resource.Spec.JobTemplate.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
	verifyPodAnnotationsAfterSuccessfulModification(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
	verifyManagedFields(resource.ManagedFields, opts...)
}

func VerifyUnmodifiedCronJob(resource *batchv1.CronJob) {
	verifyUnmodifiedPodSpec(resource.Spec.JobTemplate.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
}

func VerifyCronJobWithOptOutLabel(resource *batchv1.CronJob) {
	verifyUnmodifiedPodSpec(resource.Spec.JobTemplate.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.JobTemplate.Spec.Template.ObjectMeta)
}

func VerifyModifiedDaemonSet(resource *appsv1.DaemonSet, expectations PodSpecExpectations, opts ...VerifyOpts) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyPodAnnotationsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyManagedFields(resource.ManagedFields, opts...)
}

func VerifyUnmodifiedDaemonSet(resource *appsv1.DaemonSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyDaemonSetWithOptOutLabel(resource *appsv1.DaemonSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyModifiedDeployment(resource *appsv1.Deployment, expectations PodSpecExpectations, opts ...VerifyOpts) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyPodAnnotationsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyManagedFields(resource.ManagedFields, opts...)
}

func VerifyUnmodifiedDeployment(resource *appsv1.Deployment) {
	VerifyUnmodifiedDeploymentEventually(Default, resource)
}

func VerifyUnmodifiedDeploymentEventually(g Gomega, resource *appsv1.Deployment) {
	verifyUnmodifiedPodSpecEventually(g, resource.Spec.Template.Spec)
	verifyNoDash0LabelsEventually(g, resource.ObjectMeta)
	verifyNoDash0LabelsEventually(g, resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotationsEventually(g, resource.Spec.Template.ObjectMeta)
}

func VerifyRevertedDeployment(resource *appsv1.Deployment, expectations PodSpecExpectations) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyDeploymentWithOptOutLabel(resource *appsv1.Deployment) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyModifiedJob(resource *batchv1.Job, expectations PodSpecExpectations, opts ...VerifyOpts) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyPodAnnotationsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyManagedFields(resource.ManagedFields, opts...)
}

func VerifyModifiedJobAfterUnsuccessfulOptOut(resource *batchv1.Job) {
	verifyPodSpec(resource.Spec.Template.Spec, BasicInstrumentedPodSpecExpectations())
	jobLabels := resource.ObjectMeta.Labels
	Expect(jobLabels["dash0.com/instrumented"]).To(Equal("true"))
	Expect(jobLabels["dash0.com/operator-image"]).To(Equal("some-registry.com_1234_dash0hq_operator-controller_1.2.3"))
	Expect(jobLabels["dash0.com/init-container-image"]).To(Equal("some-registry.com_1234_dash0hq_instrumentation_4.5.6"))
	Expect(jobLabels["dash0.com/instrumented-by"]).NotTo(Equal(""))
	Expect(jobLabels["dash0.com/enable"]).To(Equal("false"))
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
}

func VerifyImmutableJobCouldNotBeModified(resource *batchv1.Job) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsAfterFailureToModify(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyUnmodifiedJob(resource *batchv1.Job) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyJobWithOptOutLabel(resource *batchv1.Job) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyModifiedPod(resource *corev1.Pod, expectations PodSpecExpectations, opts ...VerifyOpts) {
	verifyPodSpec(resource.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyPodAnnotationsAfterSuccessfulModification(resource.ObjectMeta)
	verifyManagedFields(resource.ManagedFields, opts...)
}

func VerifyUnmodifiedPod(resource *corev1.Pod) {
	verifyUnmodifiedPodSpec(resource.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.ObjectMeta)
}

func VerifyPodWithOptOutLabel(resource *corev1.Pod) {
	verifyUnmodifiedPodSpec(resource.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.ObjectMeta)
}

func VerifyModifiedReplicaSet(resource *appsv1.ReplicaSet, expectations PodSpecExpectations, opts ...VerifyOpts) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyPodAnnotationsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyManagedFields(resource.ManagedFields, opts...)
}

func VerifyUnmodifiedReplicaSet(resource *appsv1.ReplicaSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyReplicaSetWithOptOutLabel(resource *appsv1.ReplicaSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyModifiedStatefulSet(resource *appsv1.StatefulSet, expectations PodSpecExpectations, opts ...VerifyOpts) {
	verifyPodSpec(resource.Spec.Template.Spec, expectations)
	verifyLabelsAfterSuccessfulModification(resource.ObjectMeta)
	verifyLabelsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyPodAnnotationsAfterSuccessfulModification(resource.Spec.Template.ObjectMeta)
	verifyManagedFields(resource.ManagedFields, opts...)
}

func VerifyUnmodifiedStatefulSet(resource *appsv1.StatefulSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyNoDash0Labels(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

func VerifyStatefulSetWithOptOutLabel(resource *appsv1.StatefulSet) {
	verifyUnmodifiedPodSpec(resource.Spec.Template.Spec)
	verifyLabelsForOptOutWorkload(resource.ObjectMeta)
	verifyNoDash0Labels(resource.Spec.Template.ObjectMeta)
	verifyNoDash0PodAnnotations(resource.Spec.Template.ObjectMeta)
}

//nolint:gocyclo
func verifyPodSpec(podSpec corev1.PodSpec, expectations PodSpecExpectations) {
	verifyInitContainers(podSpec, expectations)
	verifyContainers(podSpec, expectations)
	verifyVolumes(podSpec, expectations)
}

func verifyInitContainers(podSpec corev1.PodSpec, expectations PodSpecExpectations) {
	Expect(podSpec.InitContainers).To(HaveLen(expectations.InitContainers))
	for i, initContainer := range podSpec.InitContainers {
		verifyInitContainer(i, expectations, initContainer)
	}
}

func verifyInitContainer(i int, expectations PodSpecExpectations, initContainer corev1.Container) {
	if i == expectations.Dash0InitContainerIdx {
		verifyDash0InitContainer(initContainer)
	} else {
		Expect(initContainer.Name).To(Equal(fmt.Sprintf("test-init-container-%d", i)))
		Expect(initContainer.Env).To(HaveLen(i + 1))
	}
}

func verifyDash0InitContainer(initContainer corev1.Container) {
	Expect(initContainer.Name).To(Equal("dash0-instrumentation"))
	Expect(initContainer.Image).To(Equal(InitContainerImageTest))
	Expect(initContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
	Expect(initContainer.Env).To(HaveLen(1))
	Expect(initContainer.Env).To(
		ContainElement(MatchEnvVar("DASH0_INSTRUMENTATION_FOLDER_DESTINATION", "/__otel_auto_instrumentation")))
	Expect(initContainer.SecurityContext).NotTo(BeNil())
	Expect(initContainer.VolumeMounts).To(HaveLen(1))
	Expect(initContainer.VolumeMounts).To(
		ContainElement(MatchVolumeMount("dash0-instrumentation", "/__otel_auto_instrumentation")))
}

func verifyContainers(podSpec corev1.PodSpec, expectations PodSpecExpectations) {
	Expect(podSpec.Containers).To(HaveLen(len(expectations.Containers)))
	for containerIdx, container := range podSpec.Containers {
		verifyContainer(container, expectations.Containers[containerIdx])
	}
}

func verifyContainer(container corev1.Container, expectations ContainerExpectations) {
	Expect(container.Name).To(Equal(expectations.ContainerName))
	verifyVolumeMounts(container, expectations)
	verifyEnvironmentVariables(container, expectations, expectations.ContainerName)
}

func verifyVolumeMounts(container corev1.Container, expectations ContainerExpectations) {
	Expect(container.VolumeMounts).To(HaveLen(expectations.VolumeMounts))
	for j, volumeMount := range container.VolumeMounts {
		if j == expectations.Dash0VolumeMountIdx {
			Expect(volumeMount.Name).To(Equal("dash0-instrumentation"))
			Expect(volumeMount.MountPath).To(Equal("/__otel_auto_instrumentation"))
		} else {
			Expect(volumeMount.Name).To(Equal(fmt.Sprintf("test-volume-%d", j)))
		}
	}
}

func verifyEnvironmentVariables(container corev1.Container, expectations ContainerExpectations, containerName string) {
	actualEnvVars := container.Env
	expectedEnvVars := expectations.EnvVars

	Expect(container.Env).To(HaveLen(len(expectedEnvVars)), containerName)
	for envVarName, envVarExpectation := range expectedEnvVars {
		VerifyEnvVarOrUnset(envVarExpectation, actualEnvVars, envVarName, containerName)
	}

	// if DASH0_NODE_IP is in the env vars, it should be the very first env var
	if slices.Contains(slices.Collect(maps.Keys(expectedEnvVars)), util.EnvVarDash0NodeIp) {
		Expect(slices.IndexFunc(container.Env, func(envVar corev1.EnvVar) bool {
			return envVar.Name == util.EnvVarDash0NodeIp
		})).To(Equal(0), "DASH0_NODE_IP needs to be listed as the first environment variable")
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

func verifyVolumes(podSpec corev1.PodSpec, expectations PodSpecExpectations) {
	Expect(podSpec.Volumes).To(HaveLen(expectations.Volumes))
	for i, volume := range podSpec.Volumes {
		if i == expectations.Dash0VolumeIdx {
			Expect(volume.Name).To(Equal("dash0-instrumentation"))
			Expect(volume.EmptyDir).NotTo(BeNil())
		} else {
			Expect(volume.Name).To(Equal(fmt.Sprintf("test-volume-%d", i)))
		}
	}
}

func verifyLabelsAfterSuccessfulModification(meta metav1.ObjectMeta) {
	Expect(meta.Labels["dash0.com/instrumented"]).To(Equal("true"))
	Expect(meta.Labels["dash0.com/operator-image"]).To(Equal("some-registry.com_1234_dash0hq_operator-controller_1.2.3"))
	Expect(meta.Labels["dash0.com/init-container-image"]).To(Equal("some-registry.com_1234_dash0hq_instrumentation_4.5.6"))
	Expect(meta.Labels["dash0.com/instrumented-by"]).NotTo(Equal(""))
	Expect(meta.Labels["dash0.com/enable"]).To(Or(Equal(""), Equal("true")))
}

func verifyPodAnnotationsAfterSuccessfulModification(podMeta metav1.ObjectMeta) {
	Expect(podMeta.Annotations[safeToEvictLocalVolumesAnnotationName]).To(Equal("dash0-instrumentation"))
}

func verifyLabelsAfterFailureToModify(meta metav1.ObjectMeta) {
	Expect(meta.Labels["dash0.com/instrumented"]).To(Equal("false"))
	Expect(meta.Labels["dash0.com/operator-image"]).To(Equal("some-registry.com_1234_dash0hq_operator-controller_1.2.3"))
	Expect(meta.Labels["dash0.com/init-container-image"]).To(Equal("some-registry.com_1234_dash0hq_instrumentation_4.5.6"))
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

func verifyNoDash0PodAnnotations(podMeta metav1.ObjectMeta) {
	verifyNoDash0PodAnnotationsEventually(Default, podMeta)
}

func verifyNoDash0PodAnnotationsEventually(g Gomega, podMeta metav1.ObjectMeta) {
	g.Expect(podMeta.Annotations[safeToEvictLocalVolumesAnnotationName]).
		NotTo(ContainSubstring("dash0-instrumentation"))
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

func VerifyWebhookIgnoreOnceLabelIsAbsent(objectMeta *metav1.ObjectMeta) {
	Expect(objectMeta.Labels["dash0.com/webhook-ignore-once"]).To(Equal(""))
}

func verifyManagedFields(managedFields []metav1.ManagedFieldsEntry, opts ...VerifyOpts) {
	if len(opts) > 1 {
		Fail("too many VerifyOpts provided")
	}
	if len(opts) == 0 {
		opts = []VerifyOpts{ExpectManagedFields}
	}

	if !opts[0].VerifyManagedFields {
		return
	}

	managedFieldsFromOperator := make([]metav1.ManagedFieldsEntry, 0, 1)
	for _, mf := range managedFields {
		if mf.Manager == "dash0-operator" {
			managedFieldsFromOperator = append(managedFieldsFromOperator, mf)
		}
	}

	if opts[0].ExpectManagedFields {
		Expect(managedFieldsFromOperator).To(HaveLen(1))
		managedField := managedFieldsFromOperator[0]
		Expect(string(managedField.Operation)).To(Equal("Update"))
		fields := string(managedField.FieldsV1.Raw)
		Expect(fields).To(ContainSubstring("f:labels"))
	} else {
		Expect(managedFieldsFromOperator).To(HaveLen(0))
	}
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
) *corev1.Event {
	return VerifyEvent(
		ctx,
		clientset,
		namespace,
		resourceName,
		util.ReasonSuccessfulInstrumentation,
		fmt.Sprintf("Dash0 instrumentation of this workload by the %s has been successful.", eventSource),
	)
}

func VerifyInstrumentationViaHigherOrderWorkloadEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	eventSource string,
) *corev1.Event {
	return VerifyEvent(
		ctx,
		clientset,
		namespace,
		resourceName,
		util.ReasonNoInstrumentationNecessary,
		fmt.Sprintf(
			"The workload is part of a higher order workload that will be instrumented by the %s, "+
				"no modification by the %s is necessary.",
			eventSource, eventSource),
	)
}

func VerifyFailedInstrumentationEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	message string,
) *corev1.Event {
	return VerifyEvent(
		ctx,
		clientset,
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
) *corev1.Event {
	return VerifyEvent(
		ctx,
		clientset,
		namespace,
		resourceName,
		util.ReasonSuccessfulUninstrumentation,
		fmt.Sprintf("The %s successfully removed the Dash0 instrumentation from this workload.", eventSource),
	)
}

func VerifySuccessfulUninstrumentationEventEventually(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	g Gomega,
	namespace string,
	resourceName string,
	eventSource string,
) *corev1.Event {
	return verifyEventEventually(
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
) *corev1.Event {
	return VerifyEvent(
		ctx,
		clientset,
		namespace,
		resourceName,
		util.ReasonFailedUninstrumentation,
		message,
	)
}

func VerifyEvent(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	resourceName string,
	reason util.Reason,
	message string,
) *corev1.Event {
	var event *corev1.Event
	Eventually(func(g Gomega) {
		event = verifyEventEventually(ctx, clientset, g, namespace, resourceName, reason, message)
	}, eventTimeout).Should(Succeed())
	return event
}

func verifyEventEventually(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	g Gomega,
	namespace string,
	resourceName string,
	reason util.Reason,
	message string,
) *corev1.Event {
	matcher := MatchEvent(
		namespace,
		resourceName,
		reason,
		message,
	)

	allEvents, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(allEvents.Items).To(HaveLen(1))
	g.Expect(allEvents.Items).To(ContainElement(matcher))

	for _, event := range allEvents.Items {
		if success, _ := matcher.Match(event); success {
			return &event
		}
	}
	return nil
}

func VerifyEnvVarsFromMap(
	expectedEnvVars map[string]*EnvVarExpectation,
	actualEnvVars []corev1.EnvVar,
) {
	for name := range expectedEnvVars {
		VerifyEnvVarOrUnset(expectedEnvVars[name], actualEnvVars, name, "")
	}
}

func VerifyEnvVarOrUnset(
	expectation *EnvVarExpectation,
	actualEnvVars []corev1.EnvVar,
	name string,
	containerName string,
) {
	if expectation == nil {
		Expect(FindEnvVarByName(actualEnvVars, name)).To(BeNil(), containerName)
	} else {
		VerifyEnvVar(*expectation, actualEnvVars, name, containerName)
	}
}

func VerifyEnvVar(expectation EnvVarExpectation, actualEnvVars []corev1.EnvVar, name string, containerName string) {
	actualEnvVar := FindEnvVarByName(actualEnvVars, name)
	msgExtra := containerName + " / " + name
	Expect(actualEnvVar).ToNot(BeNil(), "%s: expected env var to be set but it was not", msgExtra)
	if expectation.Value != "" {
		Expect(expectation.ValueFrom).To(BeEmpty(), "%s: inconsistent env var expectation", msgExtra)
		Expect(expectation.UnorderedCommaSeparatedValues).To(BeEmpty(), "%s: inconsistent env var expectation", msgExtra)

		Expect(actualEnvVar.Value).To(Equal(expectation.Value), msgExtra)
		Expect(actualEnvVar.ValueFrom).To(BeNil(), msgExtra)
	} else if expectation.ValueFrom != "" {
		Expect(expectation.Value).To(BeEmpty(), "%s: inconsistent env var expectation", msgExtra)
		Expect(expectation.UnorderedCommaSeparatedValues).To(BeEmpty(), "%s: inconsistent env var expectation", msgExtra)

		Expect(actualEnvVar.ValueFrom).ToNot(BeNil(), msgExtra)
		Expect(actualEnvVar.ValueFrom.FieldRef).ToNot(BeNil(), msgExtra)
		Expect(actualEnvVar.ValueFrom.FieldRef.FieldPath).To(Equal(expectation.ValueFrom), msgExtra)
		Expect(actualEnvVar.Value).To(BeEmpty(), msgExtra)
	} else if len(expectation.UnorderedCommaSeparatedValues) > 0 {
		Expect(expectation.Value).To(BeEmpty(), "%s: inconsistent env var expectation", msgExtra)
		Expect(expectation.ValueFrom).To(BeEmpty(), "%s: inconsistent env var expectation", msgExtra)

		Expect(actualEnvVar.Value).NotTo(BeNil(), msgExtra)
		keyValuePairs := strings.Split(actualEnvVar.Value, ",")
		Expect(keyValuePairs).To(HaveLen(len(expectation.UnorderedCommaSeparatedValues)), msgExtra)
		for _, expectedPair := range expectation.UnorderedCommaSeparatedValues {
			Expect(keyValuePairs).To(ContainElement(expectedPair), msgExtra)
		}
		Expect(actualEnvVar.ValueFrom).To(BeNil(), msgExtra)
	} else {
		Fail(fmt.Sprintf("%s: inconsistent env var expectation", msgExtra))
	}
}

func FindContainerByName(containers []corev1.Container, name string) *corev1.Container {
	for _, container := range containers {
		if container.Name == name {
			return &container
		}
	}
	return nil
}

func FindEnvVarByName(envVars []corev1.EnvVar, name string) *corev1.EnvVar {
	for _, envVar := range envVars {
		if envVar.Name == name {
			return &envVar
		}
	}
	return nil
}

func FindVolumeByName(volumes []corev1.Volume, name string) *corev1.Volume {
	for _, volume := range volumes {
		if volume.Name == name {
			return &volume
		}
	}
	return nil
}

func FindVolumeMountByName(volumeMounts []corev1.VolumeMount, name string) *corev1.VolumeMount {
	for _, volumeMount := range volumeMounts {
		if volumeMount.Name == name {
			return &volumeMount
		}
	}
	return nil
}
