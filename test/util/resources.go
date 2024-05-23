// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	TestNamespaceName = "test-namespace"
	CronJobName       = "cronjob"
	DaemonSetName     = "daemonset"
	DeploymentName    = "deployment"
	JobName           = "job"
	ReplicaSetName    = "replicaset"
	StatefulSetName   = "statefulset"
)

func EnsureDash0CustomResourceExists(
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

func TestNamespace(name string) *corev1.Namespace {
	namespace := &corev1.Namespace{}
	namespace.Name = name
	return namespace
}

func CreateTestNamespace(ctx context.Context, k8sClient client.Client, name string) *corev1.Namespace {
	namespace := TestNamespace(name)
	Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())
	return namespace
}

func EnsureTestNamespaceExists(
	ctx context.Context,
	k8sClient client.Client,
	name string,
) *corev1.Namespace {
	object := EnsureDash0CustomResourceExists(
		ctx,
		k8sClient,
		types.NamespacedName{Name: name},
		&corev1.Namespace{},
		TestNamespace(name),
	)
	return object.(*corev1.Namespace)
}

func BasicCronJob(namespace string, name string) *batchv1.CronJob {
	resource := &batchv1.CronJob{}
	resource.Namespace = namespace
	resource.Name = name
	resource.Spec = batchv1.CronJobSpec{}
	resource.Spec.Schedule = "*/1 * * * *"
	resource.Spec.JobTemplate.Spec.Template = basicPodSpecTemplate()
	resource.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	return resource
}

func CreateBasicCronJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.CronJob {
	return create(ctx, k8sClient, BasicCronJob(namespace, name)).(*batchv1.CronJob)
}

func BasicDaemonSet(namespace string, name string) *appsv1.DaemonSet {
	resource := &appsv1.DaemonSet{}
	resource.Namespace = namespace
	resource.Name = name
	resource.Spec = appsv1.DaemonSetSpec{}
	resource.Spec.Template = basicPodSpecTemplate()
	resource.Spec.Selector = createSelector()
	return resource
}

func CreateBasicDaemonSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.DaemonSet {
	return create(ctx, k8sClient, BasicDaemonSet(namespace, name)).(*appsv1.DaemonSet)
}

func BasicDeployment(namespace string, name string) *appsv1.Deployment {
	resource := &appsv1.Deployment{}
	resource.Namespace = namespace
	resource.Name = name
	resource.Spec = appsv1.DeploymentSpec{}
	resource.Spec.Template = basicPodSpecTemplate()
	resource.Spec.Selector = createSelector()
	return resource
}

func CreateBasicDeployment(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.Deployment {
	return create(ctx, k8sClient, BasicDeployment(namespace, name)).(*appsv1.Deployment)
}

func BasicJob(namespace string, name string) *batchv1.Job {
	resource := &batchv1.Job{}
	resource.Namespace = namespace
	resource.Name = name
	resource.Spec = batchv1.JobSpec{}
	resource.Spec.Template = basicPodSpecTemplate()
	resource.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	return resource
}

func CreateBasicJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.Job {
	return create(ctx, k8sClient, BasicJob(namespace, name)).(*batchv1.Job)
}

func BasicReplicaSet(namespace string, name string) *appsv1.ReplicaSet {
	resource := &appsv1.ReplicaSet{}
	resource.Namespace = namespace
	resource.Name = name
	resource.Spec = appsv1.ReplicaSetSpec{}
	resource.Spec.Template = basicPodSpecTemplate()
	resource.Spec.Selector = createSelector()
	return resource
}

func CreateBasicReplicaSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	return create(ctx, k8sClient, BasicReplicaSet(namespace, name)).(*appsv1.ReplicaSet)
}

func ReplicaSetOwnedByDeployment(namespace string, name string) *appsv1.ReplicaSet {
	resource := BasicReplicaSet(namespace, name)
	resource.ObjectMeta = metav1.ObjectMeta{
		Namespace: namespace,
		Name:      name,
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "deployment",
			UID:        "1234",
		}},
	}
	return resource
}

func CreateReplicaSetOwnedByDeployment(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	return create(ctx, k8sClient, ReplicaSetOwnedByDeployment(namespace, name)).(*appsv1.ReplicaSet)
}

func BasicStatefulSet(namespace string, name string) *appsv1.StatefulSet {
	resource := &appsv1.StatefulSet{}
	resource.Namespace = namespace
	resource.Name = name
	resource.Spec = appsv1.StatefulSetSpec{}
	resource.Spec.Template = basicPodSpecTemplate()
	resource.Spec.Selector = createSelector()
	return resource
}

func CreateBasicStatefulSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.StatefulSet {
	return create(ctx, k8sClient, BasicStatefulSet(namespace, name)).(*appsv1.StatefulSet)
}

func basicPodSpecTemplate() corev1.PodTemplateSpec {
	podSpecTemplate := corev1.PodTemplateSpec{}
	podSpecTemplate.Labels = map[string]string{"app": "test"}
	podSpecTemplate.Spec.Containers = []corev1.Container{{
		Name:  "test-container-0",
		Image: "ubuntu",
	}}
	return podSpecTemplate
}

func createSelector() *metav1.LabelSelector {
	selector := &metav1.LabelSelector{}
	selector.MatchLabels = map[string]string{"app": "test"}
	return selector
}

func create(ctx context.Context, k8sClient client.Client, resource client.Object) client.Object {
	Expect(k8sClient.Create(ctx, resource)).Should(Succeed())
	return resource
}

func DeploymentWithMoreBellsAndWhistles(namespace string, name string) *appsv1.Deployment {
	resource := BasicDeployment(namespace, name)
	podSpec := &resource.Spec.Template.Spec
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

	return resource
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
		{
			Name:  "dash0-instrumentation",
			Image: "ubuntu",
		},
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
					// Dash0 does not support injecting into containers that already have NODE_OPTIONS set via a
					// ValueFrom clause, thus this env var will not be modified.
					Name:      "NODE_OPTIONS",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
				},
				{
					// this ValueFrom will be removed and replaced by a simple Value
					Name:      "DASH0_OTEL_COLLECTOR_BASE_URL",
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
					Name:  "NODE_OPTIONS",
					Value: "--require something-else --experimental-modules",
				},
				{
					Name:  "TEST2",
					Value: "value",
				},
			},
		},
	}

	return deployment
}

func GetCronJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.CronJob {
	resource := &batchv1.CronJob{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, resource)).Should(Succeed())
	return resource
}

func GetDaemonSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.DaemonSet {
	resource := &appsv1.DaemonSet{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, resource)).Should(Succeed())
	return resource
}

func GetDeployment(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.Deployment {
	resource := &appsv1.Deployment{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, resource)).Should(Succeed())
	return resource
}

func GetJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.Job {
	resource := &batchv1.Job{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, resource)).Should(Succeed())
	return resource
}

func GetReplicaSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	resource := &appsv1.ReplicaSet{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, resource)).Should(Succeed())
	return resource
}

func GetStatefulSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.StatefulSet {
	resource := &appsv1.StatefulSet{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, resource)).Should(Succeed())
	return resource
}
