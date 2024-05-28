// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	TestNamespaceName = "test-namespace"
	CronJobName       = "cronjob"
	DaemonSetName     = "daemonset"
	DeploymentName    = "deployment"
	JobName1          = "job1"
	JobName2          = "job2"
	JobName3          = "job3"
	ReplicaSetName    = "replicaset"
	StatefulSetName   = "statefulset"
)

var (
	True                 = true
	False                = false
	ArbitraryNumer int64 = 1302

	instrumentationInitContainer = corev1.Container{
		Name:  "dash0-instrumentation",
		Image: "dash0-instrumentation:1.2.3",
		Env: []corev1.EnvVar{{
			Name:  "DASH0_INSTRUMENTATION_FOLDER_DESTINATION",
			Value: "/opt/dash0",
		}},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &False,
			Privileged:               &False,
			ReadOnlyRootFilesystem:   &True,
			RunAsNonRoot:             &False,
			RunAsUser:                &ArbitraryNumer,
			RunAsGroup:               &ArbitraryNumer,
		},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "dash0-instrumentation",
			ReadOnly:  false,
			MountPath: "/opt/dash0",
		}},
	}
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
	return createResource(ctx, k8sClient, BasicCronJob(namespace, name)).(*batchv1.CronJob)
}

func InstrumentedCronJob(namespace string, name string) *batchv1.CronJob {
	resource := BasicCronJob(namespace, name)
	simulateInstrumentedResource(&resource.Spec.JobTemplate.Spec.Template, &resource.ObjectMeta, namespace)
	return resource
}

func CreateInstrumentedCronJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.CronJob {
	return createResource(ctx, k8sClient, InstrumentedCronJob(namespace, name)).(*batchv1.CronJob)
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
	return createResource(ctx, k8sClient, BasicDaemonSet(namespace, name)).(*appsv1.DaemonSet)
}

func InstrumentedDaemonSet(namespace string, name string) *appsv1.DaemonSet {
	resource := BasicDaemonSet(namespace, name)
	simulateInstrumentedResource(&resource.Spec.Template, &resource.ObjectMeta, namespace)
	return resource
}

func CreateInstrumentedDaemonSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.DaemonSet {
	return createResource(ctx, k8sClient, InstrumentedDaemonSet(namespace, name)).(*appsv1.DaemonSet)
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
	return createResource(ctx, k8sClient, BasicDeployment(namespace, name)).(*appsv1.Deployment)
}

func InstrumentedDeployment(namespace string, name string) *appsv1.Deployment {
	resource := BasicDeployment(namespace, name)
	simulateInstrumentedResource(&resource.Spec.Template, &resource.ObjectMeta, namespace)
	return resource
}

func CreateInstrumentedDeployment(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.Deployment {
	return createResource(ctx, k8sClient, InstrumentedDeployment(namespace, name)).(*appsv1.Deployment)
}

func DeploymentWithInstrumentedFalseLabel(namespace string, name string) *appsv1.Deployment {
	resource := BasicDeployment(namespace, name)
	addInstrumentationLabels(&resource.ObjectMeta, false)
	return resource
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
	return createResource(ctx, k8sClient, BasicJob(namespace, name)).(*batchv1.Job)
}

func InstrumentedJob(namespace string, name string) *batchv1.Job {
	resource := BasicJob(namespace, name)
	simulateInstrumentedResource(&resource.Spec.Template, &resource.ObjectMeta, namespace)
	return resource
}

func CreateInstrumentedJob(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.Job {
	return createResource(ctx, k8sClient, InstrumentedJob(namespace, name)).(*batchv1.Job)
}

func JobWithInstrumentationLabels(namespace string, name string) *batchv1.Job {
	resource := BasicJob(namespace, name)
	addInstrumentationLabels(&resource.ObjectMeta, false)
	return resource
}

func CreateJobWithInstrumentationLabels(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *batchv1.Job {
	return createResource(ctx, k8sClient, JobWithInstrumentationLabels(namespace, name)).(*batchv1.Job)
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
	return createResource(ctx, k8sClient, BasicReplicaSet(namespace, name)).(*appsv1.ReplicaSet)
}

func InstrumentedReplicaSet(namespace string, name string) *appsv1.ReplicaSet {
	resource := BasicReplicaSet(namespace, name)
	simulateInstrumentedResource(&resource.Spec.Template, &resource.ObjectMeta, namespace)
	return resource
}

func CreateInstrumentedReplicaSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	return createResource(ctx, k8sClient, InstrumentedReplicaSet(namespace, name)).(*appsv1.ReplicaSet)
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
	return createResource(ctx, k8sClient, ReplicaSetOwnedByDeployment(namespace, name)).(*appsv1.ReplicaSet)
}

func InstrumentedReplicaSetOwnedByDeployment(namespace string, name string) *appsv1.ReplicaSet {
	resource := ReplicaSetOwnedByDeployment(namespace, name)
	simulateInstrumentedResource(&resource.Spec.Template, &resource.ObjectMeta, namespace)
	return resource
}

func CreateInstrumentedReplicaSetOwnedByDeployment(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.ReplicaSet {
	return createResource(ctx, k8sClient, InstrumentedReplicaSetOwnedByDeployment(namespace, name)).(*appsv1.ReplicaSet)
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
	return createResource(ctx, k8sClient, BasicStatefulSet(namespace, name)).(*appsv1.StatefulSet)
}

func InstrumentedStatefulSet(namespace string, name string) *appsv1.StatefulSet {
	resource := BasicStatefulSet(namespace, name)
	simulateInstrumentedResource(&resource.Spec.Template, &resource.ObjectMeta, namespace)
	return resource
}

func CreateInstrumentedStatefulSet(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) *appsv1.StatefulSet {
	return createResource(ctx, k8sClient, InstrumentedStatefulSet(namespace, name)).(*appsv1.StatefulSet)
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

func createResource(ctx context.Context, k8sClient client.Client, resource client.Object) client.Object {
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
					SizeLimit: resource.NewScaledQuantity(150, resource.Mega),
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
					MountPath: "/opt/dash0",
				},
			},
			Env: []corev1.EnvVar{
				{
					Name:  "TEST0",
					Value: "value",
				},
				{
					Name:  "NODE_OPTIONS",
					Value: "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js",
				},
				{
					Name:  "DASH0_OTEL_COLLECTOR_BASE_URL",
					Value: fmt.Sprintf("http://dash0-opentelemetry-collector-daemonset.%s.svc.cluster.local:4318", namespace),
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
					MountPath: "/opt/dash0",
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
					Name:  "NODE_OPTIONS",
					Value: "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js",
				},
				{
					Name:  "DASH0_OTEL_COLLECTOR_BASE_URL",
					Value: fmt.Sprintf("http://dash0-opentelemetry-collector-daemonset.%s.svc.cluster.local:4318", namespace),
				},
			},
		},
	}

	return deployment
}

func simulateInstrumentedResource(podTemplateSpec *corev1.PodTemplateSpec, meta *metav1.ObjectMeta, namespace string) {
	podSpec := &podTemplateSpec.Spec
	podSpec.Volumes = []corev1.Volume{
		{
			Name: "dash0-instrumentation",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: resource.NewScaledQuantity(150, resource.Mega),
				},
			},
		},
	}
	podSpec.InitContainers = []corev1.Container{instrumentationInitContainer}

	container := &podSpec.Containers[0]
	container.VolumeMounts = []corev1.VolumeMount{{
		Name:      "dash0-instrumentation",
		MountPath: "/opt/dash0",
	}}
	container.Env = []corev1.EnvVar{
		{
			Name:  "NODE_OPTIONS",
			Value: "--require /opt/dash0/instrumentation/node.js/node_modules/@dash0/opentelemetry/src/index.js",
		},
		{
			Name:  "DASH0_OTEL_COLLECTOR_BASE_URL",
			Value: fmt.Sprintf("http://dash0-opentelemetry-collector-daemonset.%s.svc.cluster.local:4318", namespace),
		},
	}

	addInstrumentationLabels(meta, true)
	addInstrumentationLabels(&podTemplateSpec.ObjectMeta, true)
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

func addInstrumentationLabels(meta *metav1.ObjectMeta, instrumented bool) {
	addLabel(meta, util.InstrumentedLabelKey, strconv.FormatBool(instrumented))
	addLabel(meta, util.OperatorVersionLabelKey, "1.2.3")
	addLabel(meta, util.InitContainerImageVersionLabelKey, "4.5.6")
	addLabel(meta, util.InstrumentedByLabelKey, "someone")
}

func addLabel(meta *metav1.ObjectMeta, key string, value string) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string, 1)
	}
	meta.Labels[key] = value
}
