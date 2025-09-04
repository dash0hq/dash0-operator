// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"
	"maps"
	"strconv"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	instrumentationInitContainer = corev1.Container{
		Name:            "dash0-instrumentation",
		Image:           InitContainerImageTest,
		ImagePullPolicy: corev1.PullAlways,
		Env: []corev1.EnvVar{{
			Name:  "DASH0_INSTRUMENTATION_FOLDER_DESTINATION",
			Value: "/__dash0__",
		}},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			Privileged:               ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			RunAsNonRoot:             ptr.To(false),
			RunAsUser:                &ArbitraryNumer,
			RunAsGroup:               &ArbitraryNumer,
		},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "dash0-instrumentation",
			ReadOnly:  false,
			MountPath: "/__dash0__",
		}},
	}
)

func Namespace(name string) *corev1.Namespace {
	namespace := &corev1.Namespace{}
	namespace.Name = name
	return namespace
}

func EnsureTestNamespaceExists(ctx context.Context, k8sClient client.Client) *corev1.Namespace {
	return EnsureNamespaceExists(ctx, k8sClient, TestNamespaceName)
}

func EnsureOperatorNamespaceExists(ctx context.Context, k8sClient client.Client) *corev1.Namespace {
	return EnsureNamespaceExists(ctx, k8sClient, OperatorNamespace)
}

func DeleteNamespace(ctx context.Context, k8sClient client.Client, name string) {
	Expect(k8sClient.Delete(ctx, Namespace(name), &client.DeleteOptions{
		GracePeriodSeconds: new(int64),
	})).To(Succeed())
}

func EnsureNamespaceExists(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
) *corev1.Namespace {
	By("creating the test namespace")
	object := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		types.NamespacedName{Name: namespace},
		&corev1.Namespace{},
		Namespace(namespace),
	)
	return object.(*corev1.Namespace)
}

func EnsureKubernetesObjectExists(
	ctx context.Context,
	k8sClient client.Client,
	qualifiedName types.NamespacedName,
	receiver client.Object,
	blueprint client.Object,
) client.Object {
	err := k8sClient.Get(ctx, qualifiedName, receiver)
	if err == nil {
		return receiver
	}
	if errors.IsNotFound(err) {
		Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())
		return blueprint
	} else {
		Expect(err).ToNot(HaveOccurred())
		return nil
	}
}

func UniqueName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, uuid.New())
}

func BasicCronJob(namespace string, name string) *batchv1.CronJob {
	workload := &batchv1.CronJob{}
	workload.Namespace = namespace
	workload.Name = name
	workload.Spec = batchv1.CronJobSpec{}
	workload.Spec.Schedule = "*/1 * * * *"
	workload.Spec.JobTemplate.Spec.Template = basicPodSpecTemplate()
	workload.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	return workload
}

func CreateBasicCronJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.CronJob {
	return CreateWorkload(ctx, k8sClient, BasicCronJob(namespace, name)).(*batchv1.CronJob)
}

func InstrumentedCronJob(namespace string, name string) *batchv1.CronJob {
	workload := BasicCronJob(namespace, name)
	simulateInstrumentedResource(&workload.Spec.JobTemplate.Spec.Template, &workload.ObjectMeta)
	return workload
}

func CreateInstrumentedCronJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.CronJob {
	return CreateWorkload(ctx, k8sClient, InstrumentedCronJob(namespace, name)).(*batchv1.CronJob)
}

func CronJobWithOptOutLabel(namespace string, name string) *batchv1.CronJob {
	workload := BasicCronJob(namespace, name)
	AddOptOutLabel(&workload.ObjectMeta)
	return workload
}

func CreateCronJobWithOptOutLabel(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.CronJob {
	return CreateWorkload(ctx, k8sClient, CronJobWithOptOutLabel(namespace, name)).(*batchv1.CronJob)
}

func BasicDaemonSet(namespace string, name string) *appsv1.DaemonSet {
	workload := &appsv1.DaemonSet{}
	workload.Namespace = namespace
	workload.Name = name
	workload.Spec = appsv1.DaemonSetSpec{}
	workload.Spec.Template = basicPodSpecTemplate()
	workload.Spec.Selector = createSelector()
	return workload
}

func CreateBasicDaemonSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.DaemonSet {
	return CreateWorkload(ctx, k8sClient, BasicDaemonSet(namespace, name)).(*appsv1.DaemonSet)
}

func InstrumentedDaemonSet(namespace string, name string) *appsv1.DaemonSet {
	workload := BasicDaemonSet(namespace, name)
	simulateInstrumentedResource(&workload.Spec.Template, &workload.ObjectMeta)
	return workload
}

func CreateInstrumentedDaemonSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.DaemonSet {
	return CreateWorkload(ctx, k8sClient, InstrumentedDaemonSet(namespace, name)).(*appsv1.DaemonSet)
}

func DaemonSetWithOptOutLabel(namespace string, name string) *appsv1.DaemonSet {
	workload := BasicDaemonSet(namespace, name)
	AddOptOutLabel(&workload.ObjectMeta)
	return workload
}

func CreateDaemonSetWithOptOutLabel(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.DaemonSet {
	return CreateWorkload(ctx, k8sClient, DaemonSetWithOptOutLabel(namespace, name)).(*appsv1.DaemonSet)
}

func BasicDeployment(namespace string, name string) *appsv1.Deployment {
	workload := &appsv1.Deployment{}
	workload.Namespace = namespace
	workload.Name = name
	workload.Spec = appsv1.DeploymentSpec{}
	workload.Spec.Template = basicPodSpecTemplate()
	workload.Spec.Selector = createSelector()
	return workload
}

func CreateBasicDeployment(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.Deployment {
	return CreateWorkload(ctx, k8sClient, BasicDeployment(namespace, name)).(*appsv1.Deployment)
}

func InstrumentedDeployment(namespace string, name string) *appsv1.Deployment {
	workload := BasicDeployment(namespace, name)
	simulateInstrumentedResource(&workload.Spec.Template, &workload.ObjectMeta)
	return workload
}

func CreateInstrumentedDeployment(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.Deployment {
	return CreateWorkload(ctx, k8sClient, InstrumentedDeployment(namespace, name)).(*appsv1.Deployment)
}

func DeploymentWithOptOutLabel(namespace string, name string) *appsv1.Deployment {
	workload := BasicDeployment(namespace, name)
	AddOptOutLabel(&workload.ObjectMeta)
	return workload
}

func CreateDeploymentWithOptOutLabel(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.Deployment {
	return CreateWorkload(ctx, k8sClient, DeploymentWithOptOutLabel(namespace, name)).(*appsv1.Deployment)
}

func BasicJob(namespace string, name string) *batchv1.Job {
	workload := &batchv1.Job{}
	workload.Namespace = namespace
	workload.Name = name
	workload.Spec = batchv1.JobSpec{}
	workload.Spec.Template = basicPodSpecTemplate()
	workload.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	return workload
}

func CreateBasicJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.Job {
	return CreateWorkload(ctx, k8sClient, BasicJob(namespace, name)).(*batchv1.Job)
}

func InstrumentedJob(namespace string, name string) *batchv1.Job {
	workload := BasicJob(namespace, name)
	simulateInstrumentedResource(&workload.Spec.Template, &workload.ObjectMeta)
	return workload
}

func CreateInstrumentedJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.Job {
	return CreateWorkload(ctx, k8sClient, InstrumentedJob(namespace, name)).(*batchv1.Job)
}

func JobForWhichAnInstrumentationAttemptHasFailed(namespace string, name string) *batchv1.Job {
	workload := BasicJob(namespace, name)
	addInstrumentationLabels(&workload.ObjectMeta, false)
	return workload
}

func CreateJobForWhichAnInstrumentationAttemptHasFailed(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.Job {
	return CreateWorkload(ctx, k8sClient, JobForWhichAnInstrumentationAttemptHasFailed(namespace, name)).(*batchv1.Job)
}

func JobWithOptOutLabel(namespace string, name string) *batchv1.Job {
	workload := BasicJob(namespace, name)
	AddOptOutLabel(&workload.ObjectMeta)
	return workload
}

func CreateJobWithOptOutLabel(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.Job {
	return CreateWorkload(ctx, k8sClient, JobWithOptOutLabel(namespace, name)).(*batchv1.Job)
}

func BasicPod(namespace string, name string) *corev1.Pod {
	workload := &corev1.Pod{}
	workload.Namespace = namespace
	workload.Name = name
	workload.Spec = corev1.PodSpec{}
	workload.Spec = basicPodSpec()
	return workload
}

func CreateBasicPod(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *corev1.Pod {
	return CreateWorkload(ctx, k8sClient, BasicPod(namespace, name)).(*corev1.Pod)
}

func InstrumentedPod(namespace string, name string) *corev1.Pod {
	pod := BasicPod(namespace, name)
	simulateInstrumentedPodSpec(&pod.Spec, &pod.ObjectMeta)
	addPodAnnotations(&pod.ObjectMeta)
	return pod
}

func CreateInstrumentedPod(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *corev1.Pod {
	return CreateWorkload(ctx, k8sClient, InstrumentedPod(namespace, name)).(*corev1.Pod)
}

func PodOwnedByReplicaSet(namespace string, name string) *corev1.Pod {
	workload := BasicPod(namespace, name)
	workload.ObjectMeta = metav1.ObjectMeta{
		Namespace: namespace,
		Name:      name,
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: util.K8sApiVersionAppsV1,
			Kind:       "ReplicaSet",
			Name:       "replicaset",
			UID:        "1234",
		}},
	}
	return workload
}

func CreatePodOwnedByReplicaSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *corev1.Pod {
	return CreateWorkload(ctx, k8sClient, PodOwnedByReplicaSet(namespace, name)).(*corev1.Pod)
}

func PodWithOptOutLabel(namespace string, name string) *corev1.Pod {
	workload := BasicPod(namespace, name)
	AddOptOutLabel(&workload.ObjectMeta)
	return workload
}

func CreatePodWithOptOutLabel(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *corev1.Pod {
	return CreateWorkload(ctx, k8sClient, PodWithOptOutLabel(namespace, name)).(*corev1.Pod)
}

func BasicReplicaSet(namespace string, name string) *appsv1.ReplicaSet {
	workload := &appsv1.ReplicaSet{}
	workload.Namespace = namespace
	workload.Name = name
	workload.Spec = appsv1.ReplicaSetSpec{}
	workload.Spec.Template = basicPodSpecTemplate()
	workload.Spec.Selector = createSelector()
	return workload
}

func CreateBasicReplicaSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	return CreateWorkload(ctx, k8sClient, BasicReplicaSet(namespace, name)).(*appsv1.ReplicaSet)
}

func InstrumentedReplicaSet(namespace string, name string) *appsv1.ReplicaSet {
	workload := BasicReplicaSet(namespace, name)
	simulateInstrumentedResource(&workload.Spec.Template, &workload.ObjectMeta)
	return workload
}

func CreateInstrumentedReplicaSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	return CreateWorkload(ctx, k8sClient, InstrumentedReplicaSet(namespace, name)).(*appsv1.ReplicaSet)
}

func ReplicaSetOwnedByDeployment(namespace string, name string) *appsv1.ReplicaSet {
	workload := BasicReplicaSet(namespace, name)
	workload.ObjectMeta = metav1.ObjectMeta{
		Namespace: namespace,
		Name:      name,
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: util.K8sApiVersionAppsV1,
			Kind:       "Deployment",
			Name:       "deployment",
			UID:        "1234",
		}},
	}
	return workload
}

func CreateReplicaSetOwnedByDeployment(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	return CreateWorkload(ctx, k8sClient, ReplicaSetOwnedByDeployment(namespace, name)).(*appsv1.ReplicaSet)
}

func InstrumentedReplicaSetOwnedByDeployment(namespace string, name string) *appsv1.ReplicaSet {
	workload := ReplicaSetOwnedByDeployment(namespace, name)
	simulateInstrumentedResource(&workload.Spec.Template, &workload.ObjectMeta)
	return workload
}

func ReplicaSetWithOptOutLabel(namespace string, name string) *appsv1.ReplicaSet {
	workload := BasicReplicaSet(namespace, name)
	AddOptOutLabel(&workload.ObjectMeta)
	return workload
}

func CreateReplicaSetWithOptOutLabel(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	return CreateWorkload(ctx, k8sClient, ReplicaSetWithOptOutLabel(namespace, name)).(*appsv1.ReplicaSet)
}

func BasicStatefulSet(namespace string, name string) *appsv1.StatefulSet {
	workload := &appsv1.StatefulSet{}
	workload.Namespace = namespace
	workload.Name = name
	workload.Spec = appsv1.StatefulSetSpec{}
	workload.Spec.Template = basicPodSpecTemplate()
	workload.Spec.Selector = createSelector()
	return workload
}

func CreateBasicStatefulSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.StatefulSet {
	return CreateWorkload(ctx, k8sClient, BasicStatefulSet(namespace, name)).(*appsv1.StatefulSet)
}

func InstrumentedStatefulSet(namespace string, name string) *appsv1.StatefulSet {
	workload := BasicStatefulSet(namespace, name)
	simulateInstrumentedResource(&workload.Spec.Template, &workload.ObjectMeta)
	return workload
}

func CreateInstrumentedStatefulSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.StatefulSet {
	return CreateWorkload(ctx, k8sClient, InstrumentedStatefulSet(namespace, name)).(*appsv1.StatefulSet)
}

func StatefulSetWithOptOutLabel(namespace string, name string) *appsv1.StatefulSet {
	workload := BasicStatefulSet(namespace, name)
	AddOptOutLabel(&workload.ObjectMeta)
	return workload
}

func CreateStatefulSetWithOptOutLabel(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.StatefulSet {
	return CreateWorkload(ctx, k8sClient, StatefulSetWithOptOutLabel(namespace, name)).(*appsv1.StatefulSet)
}

func basicPodSpecTemplate() corev1.PodTemplateSpec {
	podSpecTemplate := corev1.PodTemplateSpec{}
	podSpecTemplate.Labels = map[string]string{"app": "test"}
	podSpecTemplate.Spec = basicPodSpec()
	return podSpecTemplate
}

func basicPodSpec() corev1.PodSpec {
	podSpec := corev1.PodSpec{}
	podSpec.Containers = []corev1.Container{{
		Name:  "test-container-0",
		Image: "ubuntu",
	}}
	return podSpec
}

func createSelector() *metav1.LabelSelector {
	selector := &metav1.LabelSelector{}
	selector.MatchLabels = map[string]string{"app": "test"}
	return selector
}

func CreateWorkload(ctx context.Context, k8sClient client.Client, workload client.Object) client.Object {
	err := k8sClient.Create(ctx, workload)
	Expect(err).Should(Succeed())
	return workload
}

func UpdateWorkload(ctx context.Context, k8sClient client.Client, workload client.Object) client.Object {
	Expect(k8sClient.Update(ctx, workload)).Should(Succeed())
	return workload
}

func DeploymentWithMoreBellsAndWhistles(namespace string, name string) *appsv1.Deployment {
	workload := BasicDeployment(namespace, name)
	workload.ObjectMeta.Annotations = map[string]string{
		"resource.opentelemetry.io/workload.only.1":  "workload-value-1",
		"resource.opentelemetry.io/workload.only.2":  "workload-value-2",
		"resource.opentelemetry.io/pod.and.workload": "workload-value",
	}
	workload.ObjectMeta.Labels = map[string]string{
		"app.kubernetes.io/name":    "workload-aki-name",
		"app.kubernetes.io/part-of": "workload-aki-part-of",
		"app.kubernetes.io/version": "workload-aki-version",
	}
	maps.Copy(workload.Spec.Template.ObjectMeta.Labels,
		map[string]string{
			"app.kubernetes.io/name":    "pod-aki-name",
			"app.kubernetes.io/part-of": "pod-aki-part-of",
			"app.kubernetes.io/version": "pod-aki-version",
		})
	workload.Spec.Template.ObjectMeta.Annotations = map[string]string{
		"resource.opentelemetry.io/pod.only.1":       "pod-value-1",
		"resource.opentelemetry.io/pod.only.2":       "pod-value-2",
		"resource.opentelemetry.io/pod.and.workload": "pod-value",
	}
	podSpec := &workload.Spec.Template.Spec
	podSpec.Volumes = []corev1.Volume{
		{
			Name:         "test-volume-0",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name:         "test-volume-1",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
	}
	podSpec.InitContainers = []corev1.Container{
		{
			Name:  "test-init-container-0",
			Image: "ubuntu",
			Env: []corev1.EnvVar{{
				Name:  "TEST_INIT_0",
				Value: "value",
			}},
		},
		{
			Name:  "test-init-container-1",
			Image: "ubuntu",
			Env: []corev1.EnvVar{
				{
					Name:  "TEST_INIT_0",
					Value: "value",
				},
				{
					Name:  "TEST_INIT_1",
					Value: "value",
				},
			},
		},
	}
	podSpec.Containers = []corev1.Container{
		{
			Name:  "test-container-0",
			Image: "ubuntu",
			VolumeMounts: []corev1.VolumeMount{{
				Name:      "test-volume-0",
				MountPath: "/test-1",
			}},
			Env: []corev1.EnvVar{{
				Name:  "TEST0",
				Value: "value",
			}},
		},
		{
			Name:  "test-container-1",
			Image: "ubuntu",
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "test-volume-0",
					MountPath: "/test-0",
				},
				{
					Name:      "test-volume-1",
					MountPath: "/test-1",
				},
			},
			Env: []corev1.EnvVar{
				{
					Name:  "TEST0",
					Value: "value",
				},
				{
					Name:      "TEST1",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
				},
			},
		},
	}

	return workload
}

func DeploymentWithExistingDash0Artifacts(namespace string, name string) *appsv1.Deployment {
	deployment := BasicDeployment(namespace, name)
	podSpec := &deployment.Spec.Template.Spec
	podSpec.Volumes = []corev1.Volume{
		{
			Name:         "test-volume-0",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name: "dash0-instrumentation",
			// the volume source should be updated/overwritten by the webhook
			VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/foo/bar"}},
		},
		{
			Name:         "test-volume-2",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
	}
	podSpec.InitContainers = []corev1.Container{
		{
			Name:  "test-init-container-0",
			Image: "ubuntu",
			Env: []corev1.EnvVar{{
				Name:  "TEST_INIT_0",
				Value: "value",
			}},
		},
		instrumentationInitContainer,
		{
			Name:  "test-init-container-2",
			Image: "ubuntu",
			Env: []corev1.EnvVar{
				{
					Name:  "TEST_INIT_0",
					Value: "value",
				},
				{
					Name:  "TEST_INIT_1",
					Value: "value",
				},
				{
					Name:  "TEST_INIT_2",
					Value: "value",
				},
			},
		},
	}
	podSpec.Containers = []corev1.Container{
		{
			Name:  "test-container-0",
			Image: "ubuntu",
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "test-volume-0",
					MountPath: "/test-1",
				},
				{
					Name:      "dash0-instrumentation",
					MountPath: "/will/be/overwritten",
				},
			},
			Env: []corev1.EnvVar{
				{
					Name:  "TEST0",
					Value: "value",
				},
				{
					// The operator does not support injecting into containers that already have LD_PRELOAD set via a
					// ValueFrom clause, thus this env var will not be modified.
					Name:      "LD_PRELOAD",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
				},
				{
					Name:      "DASH0_NODE_IP",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "wrong.field.ref"}},
				},
				{
					// this ValueFrom will be removed and replaced by a simple Value
					Name:      "DASH0_OTEL_COLLECTOR_BASE_URL",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
				},
				{
					// this ValueFrom will be removed and replaced by a simple Value
					Name:      "OTEL_EXPORTER_OTLP_ENDPOINT",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
				},
			},
		},
		{
			Name:  "test-container-1",
			Image: "ubuntu",
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "test-volume-0",
					MountPath: "/test-0",
				},
				{
					Name:      "dash0-instrumentation",
					MountPath: "/will/be/overwritten",
				},
				{
					Name:      "test-volume-2",
					MountPath: "/test-2",
				},
			},
			Env: []corev1.EnvVar{
				{
					Name:  "DASH0_OTEL_COLLECTOR_BASE_URL",
					Value: "base url will be replaced",
				},
				{
					Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
					Value: "base url will be replaced",
				},
				{
					// to test the removal of the legacy NODE_OPTIONS setting prior to operator version 0.28.0.
					Name:  "NODE_OPTIONS",
					Value: "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry",
				},
				{
					Name:  "LD_PRELOAD",
					Value: "third_party_preload.so another_third_party_preload.so",
				},
				{
					Name:  "DASH0_NODE_IP",
					Value: "will be replaced by a value from",
				},
				{
					Name:  "TEST4",
					Value: "value",
				},
			},
		},
	}

	return deployment
}

func InstrumentedDeploymentWithMoreBellsAndWhistles(namespace string, name string) *appsv1.Deployment {
	deployment := DeploymentWithMoreBellsAndWhistles(namespace, name)
	podSpec := &deployment.Spec.Template.Spec
	podSpec.Volumes = []corev1.Volume{
		{
			Name:         "test-volume-0",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name:         "test-volume-1",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name: "dash0-instrumentation",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: resource.NewScaledQuantity(500, resource.Mega),
				},
			},
		},
	}
	podSpec.InitContainers = []corev1.Container{
		{
			Name:  "test-init-container-0",
			Image: "ubuntu",
			Env: []corev1.EnvVar{{
				Name:  "TEST_INIT_0",
				Value: "value",
			}},
		},
		{
			Name:  "test-init-container-1",
			Image: "ubuntu",
			Env: []corev1.EnvVar{
				{
					Name:  "TEST_INIT_0",
					Value: "value",
				},
				{
					Name:  "TEST_INIT_1",
					Value: "value",
				},
			},
		},
		instrumentationInitContainer,
	}
	podSpec.Containers = []corev1.Container{
		{
			Name:  "test-container-0",
			Image: "ubuntu",
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "test-volume-0",
					MountPath: "/test-1",
				},
				{
					Name:      "dash0-instrumentation",
					MountPath: "/__dash0__",
				},
			},
			Env: []corev1.EnvVar{
				{
					Name:  "TEST0",
					Value: "value",
				},
				{
					// to test the removal of the legacy NODE_OPTIONS setting prior to operator version 0.28.0.
					Name:  "NODE_OPTIONS",
					Value: "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry",
				},
				{
					Name:  "LD_PRELOAD",
					Value: "/some/preload.so /__dash0__/dash0_injector.so /another/preload.so",
				},
				{
					Name:      "DASH0_NODE_IP",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}},
				},
				{
					Name:  "DASH0_OTEL_COLLECTOR_BASE_URL",
					Value: OTelCollectorNodeLocalBaseUrlTest,
				},
				{
					Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
					Value: OTelCollectorNodeLocalBaseUrlTest,
				},
			},
		},
		{
			Name:  "test-container-1",
			Image: "ubuntu",
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "test-volume-0",
					MountPath: "/test-0",
				},
				{
					Name:      "test-volume-1",
					MountPath: "/test-1",
				},
				{
					Name:      "dash0-instrumentation",
					MountPath: "/__dash0__",
				},
			},
			Env: []corev1.EnvVar{
				{
					Name:  "TEST0",
					Value: "value",
				},
				{
					Name:      "TEST1",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
				},
				{
					Name:  "LD_PRELOAD",
					Value: "/__dash0__/dash0_injector.so",
				},
				{
					Name:  "DASH0_OTEL_COLLECTOR_BASE_URL",
					Value: OTelCollectorNodeLocalBaseUrlTest,
				},
				{
					Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
					Value: OTelCollectorNodeLocalBaseUrlTest,
				},
			},
		},
	}

	return deployment
}

func simulateInstrumentedResource(podTemplateSpec *corev1.PodTemplateSpec, meta *metav1.ObjectMeta) {
	simulateInstrumentedPodSpec(&podTemplateSpec.Spec, meta)
	addInstrumentationLabels(&podTemplateSpec.ObjectMeta, true)
	addPodAnnotations(&podTemplateSpec.ObjectMeta)
	AddManagedFields(meta)
}

func simulateInstrumentedPodSpec(podSpec *corev1.PodSpec, meta *metav1.ObjectMeta) {
	podSpec.Volumes = []corev1.Volume{
		{
			Name: "dash0-instrumentation",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: resource.NewScaledQuantity(500, resource.Mega),
				},
			},
		},
	}
	podSpec.InitContainers = []corev1.Container{instrumentationInitContainer}

	container := &podSpec.Containers[0]
	container.VolumeMounts = []corev1.VolumeMount{{
		Name:      "dash0-instrumentation",
		MountPath: "/__dash0__",
	}}
	container.Env = []corev1.EnvVar{
		{
			Name:  "LD_PRELOAD",
			Value: "/__dash0__/dash0_injector.so",
		},
		{
			Name:      "DASH0_NODE_IP",
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}},
		},
		{
			Name:  "DASH0_OTEL_COLLECTOR_BASE_URL",
			Value: OTelCollectorNodeLocalBaseUrlTest,
		},
		{
			Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
			Value: OTelCollectorNodeLocalBaseUrlTest,
		},
		{
			Name:  "OTEL_EXPORTER_OTLP_PROTOCOL",
			Value: common.ProtocolHttpProtobuf,
		},
		{
			Name: "DASH0_NAMESPACE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name: "DASH0_POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "DASH0_POD_UID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.uid",
				},
			},
		},
		{
			Name:  "DASH0_CONTAINER_NAME",
			Value: "test-container-0",
		},
	}

	addInstrumentationLabels(meta, true)
}

func GetCronJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.CronJob {
	workload := &batchv1.CronJob{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, workload)).Should(Succeed())
	return workload
}

func GetDaemonSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.DaemonSet {
	workload := &appsv1.DaemonSet{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, workload)).Should(Succeed())
	return workload
}

func GetDeployment(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.Deployment {
	return GetDeploymentEventually(ctx, k8sClient, Default, namespace, name)
}

func GetDeploymentEventually(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	namespace string,
	name string,
) *appsv1.Deployment {
	workload := &appsv1.Deployment{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	g.ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, workload)).Should(Succeed())
	return workload
}

func GetJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.Job {
	workload := &batchv1.Job{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, workload)).Should(Succeed())
	return workload
}

func GetPod(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *corev1.Pod {
	workload := &corev1.Pod{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, workload)).Should(Succeed())
	return workload
}

func GetReplicaSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	workload := &appsv1.ReplicaSet{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, workload)).Should(Succeed())
	return workload
}

func GetStatefulSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.StatefulSet {
	workload := &appsv1.StatefulSet{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, workload)).Should(Succeed())
	return workload
}

func addInstrumentationLabels(meta *metav1.ObjectMeta, successful bool) {
	AddLabel(meta, "dash0.com/instrumented", strconv.FormatBool(successful))
	AddLabel(meta, "dash0.com/operator-image", "some-registry.com_1234_dash0hq_operator-controller_1.2.3")
	AddLabel(meta, "dash0.com/init-container-image", "some-registry.com_1234_dash0hq_instrumentation_4.5.6")
	AddLabel(meta, "dash0.com/instrumented-by", "someone")
}

func addPodAnnotations(meta *metav1.ObjectMeta) {
	AddAnnotation(meta, safeToEviceLocalVolumesAnnotationName, "dash0-instrumentation")
}

func AddOptOutLabel(meta *metav1.ObjectMeta) {
	AddLabel(meta, "dash0.com/enable", "false")
}

func RemoveOptOutLabel(meta *metav1.ObjectMeta) {
	RemoveLabel(meta, "dash0.com/enable")
}

func AddLabel(meta *metav1.ObjectMeta, key string, value string) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string, 1)
	}
	meta.Labels[key] = value
}

func AddAnnotation(meta *metav1.ObjectMeta, key string, value string) {
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string, 1)
	}
	meta.Annotations[key] = value
}

func UpdateLabel(meta *metav1.ObjectMeta, key string, value string) {
	meta.Labels[key] = value
}

func RemoveLabel(meta *metav1.ObjectMeta, key string) {
	delete(meta.Labels, key)
}

func AddManagedFields(meta *metav1.ObjectMeta) {
	meta.ManagedFields = append(meta.ManagedFields, metav1.ManagedFieldsEntry{
		Manager:    util.FieldManager,
		Operation:  metav1.ManagedFieldsOperationUpdate,
		Time:       ptr.To(metav1.Now()),
		FieldsType: "FieldsV1",
		FieldsV1: &metav1.FieldsV1{
			//nolint:lll
			Raw: []byte(`{\"f:metadata\":{\"f:labels\":{\".\":{},\"f:dash0.com/init-container-image\":{},\"f:dash0.com/instrumented\":{},\"f:dash0.com/instrumented-by\":{},\"f:dash0.com/operator-image\":{}}},\"f:spec\":{\"f:template\":{\"f:metadata\":{\"f:labels\":{\"f:dash0.com/init-container-image\":{},\"f:dash0.com/instrumented\":{},\"f:dash0.com/instrumented-by\":{},\"f:dash0.com/operator-image\":{}}},\"f:spec\":{\"f:containers\":{\"k:{\\\"name\\\":\\\"test-container-0\\\"}\":{\"f:env\":{\".\":{},\"k:{\\\"name\\\":\\\"DASH0_NODE_IP\\\"}\":{\".\":{},\"f:name\":{},\"f:valueFrom\":{\".\":{},\"f:fieldRef\":{}}},\"k:{\\\"name\\\":\\\"DASH0_OTEL_COLLECTOR_BASE_URL\\\"}\":{\".\":{},\"f:name\":{},\"f:value\":{}},\"k:{\\\"name\\\":\\\"OTEL_EXPORTER_OTLP_ENDPOINT\\\"}\":{\".\":{},\"f:name\":{},\"f:value\":{}},\"k:{\\\"name\\\":\\\"LD_PRELOAD\\\"}\":{\".\":{},\"f:name\":{},\"f:value\":{}}},\"f:volumeMounts\":{\".\":{},\"k:{\\\"mountPath\\\":\\\"/__dash0__\\\"}\":{\".\":{},\"f:mountPath\":{},\"f:name\":{}}}}},\"f:initContainers\":{\".\":{},\"k:{\\\"name\\\":\\\"dash0-instrumentation\\\"}\":{\".\":{},\"f:env\":{\".\":{},\"k:{\\\"name\\\":\\\"DASH0_INSTRUMENTATION_FOLDER_DESTINATION\\\"}\":{\".\":{},\"f:name\":{},\"f:value\":{}}},\"f:image\":{},\"f:imagePullPolicy\":{},\"f:name\":{},\"f:resources\":{},\"f:securityContext\":{\".\":{},\"f:allowPrivilegeEscalation\":{},\"f:privileged\":{},\"f:readOnlyRootFilesystem\":{},\"f:runAsGroup\":{},\"f:runAsUser\":{}},\"f:terminationMessagePath\":{},\"f:terminationMessagePolicy\":{},\"f:volumeMounts\":{\".\":{},\"k:{\\\"mountPath\\\":\\\"/__dash0__\\\"}\":{\".\":{},\"f:mountPath\":{},\"f:name\":{}}}}},\"f:volumes\":{\".\":{},\"k:{\\\"name\\\":\\\"dash0-instrumentation\\\"}\":{\".\":{},\"f:emptyDir\":{\".\":{},\"f:sizeLimit\":{}},\"f:name\":{}}}}}}}`),
		},
	})
}

func DeleteAllCreatedObjects(
	ctx context.Context,
	k8sClient client.Client,
	createdObjects []client.Object,
) []client.Object {
	By("Remove all created objects")
	for _, object := range createdObjects {
		Expect(k8sClient.Delete(ctx, object, &client.DeleteOptions{
			GracePeriodSeconds: new(int64),
		})).To(Succeed())
	}
	return make([]client.Object, 0)
}

func DeleteAllEvents(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
) {
	err := clientset.CoreV1().Events(namespace).DeleteCollection(ctx, metav1.DeleteOptions{
		GracePeriodSeconds: new(int64), // delete immediately
	}, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	allEvents, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(allEvents.Items).To(BeEmpty())
}

func LoadMonitoringResourceStatusCondition(
	ctx context.Context,
	k8sClient client.Client,
	monitoringResourceName types.NamespacedName,
	conditionType dash0common.ConditionType,
) *metav1.Condition {
	monitoringResource := LoadMonitoringResourceByNameOrFail(ctx, k8sClient, Default, monitoringResourceName)
	return meta.FindStatusCondition(monitoringResource.Status.Conditions, string(conditionType))
}

func LoadOperatorConfigurationResourceStatusCondition(
	ctx context.Context,
	k8sClient client.Client,
	operatorConfigurationResourceName string,
	conditionType dash0common.ConditionType,
) *metav1.Condition {
	operatorConfigurationResource := LoadOperatorConfigurationResourceByNameOrFail(
		ctx,
		k8sClient,
		Default,
		operatorConfigurationResourceName,
	)
	return meta.FindStatusCondition(operatorConfigurationResource.Status.Conditions, string(conditionType))
}

func VerifyResourceStatusCondition(
	g Gomega,
	condition *metav1.Condition,
	expectedStatus metav1.ConditionStatus,
	expectedReason string,
	expectedMessage string,
) {
	g.Expect(condition).NotTo(BeNil())
	g.Expect(condition.Status).To(Equal(expectedStatus))
	g.Expect(condition.Reason).To(Equal(expectedReason))
	g.Expect(condition.Message).To(Equal(expectedMessage))
}
