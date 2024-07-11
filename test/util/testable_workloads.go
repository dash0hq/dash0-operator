// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type TestableWorkload interface {
	Get() client.Object
	GetObjectMeta() *metav1.ObjectMeta
}

type CronJobTestableWorkload struct {
	cronJob *batchv1.CronJob
}

func (w CronJobTestableWorkload) Get() client.Object                { return w.cronJob }
func (w CronJobTestableWorkload) GetObjectMeta() *metav1.ObjectMeta { return &w.cronJob.ObjectMeta }
func WrapCronJobFnAsTestableWorkload(
	fn func(context.Context, client.Client, string, string) *batchv1.CronJob,
) func(context.Context, client.Client, string, string) TestableWorkload {
	return func(ctx context.Context, k8sClient client.Client, namespace string, name string) TestableWorkload {
		return CronJobTestableWorkload{cronJob: fn(ctx, k8sClient, namespace, name)}
	}
}

type DaemonSetTestableWorkload struct {
	daemonSet *appsv1.DaemonSet
}

func (w DaemonSetTestableWorkload) Get() client.Object                { return w.daemonSet }
func (w DaemonSetTestableWorkload) GetObjectMeta() *metav1.ObjectMeta { return &w.daemonSet.ObjectMeta }
func WrapDaemonSetFnAsTestableWorkload(
	fn func(context.Context, client.Client, string, string) *appsv1.DaemonSet,
) func(context.Context, client.Client, string, string) TestableWorkload {
	return func(ctx context.Context, k8sClient client.Client, namespace string, name string) TestableWorkload {
		return DaemonSetTestableWorkload{daemonSet: fn(ctx, k8sClient, namespace, name)}
	}
}

type DeploymentTestableWorkload struct {
	deployment *appsv1.Deployment
}

func (w DeploymentTestableWorkload) Get() client.Object { return w.deployment }
func (w DeploymentTestableWorkload) GetObjectMeta() *metav1.ObjectMeta {
	return &w.deployment.ObjectMeta
}
func WrapDeploymentFnAsTestableWorkload(
	fn func(context.Context, client.Client, string, string) *appsv1.Deployment,
) func(context.Context, client.Client, string, string) TestableWorkload {
	return func(ctx context.Context, k8sClient client.Client, namespace string, name string) TestableWorkload {
		return DeploymentTestableWorkload{deployment: fn(ctx, k8sClient, namespace, name)}
	}
}

type JobTestableWorkload struct {
	job *batchv1.Job
}

func (w JobTestableWorkload) Get() client.Object { return w.job }
func (w JobTestableWorkload) GetObjectMeta() *metav1.ObjectMeta {
	return &w.job.ObjectMeta
}
func WrapJobFnAsTestableWorkload(
	fn func(context.Context, client.Client, string, string) *batchv1.Job,
) func(context.Context, client.Client, string, string) TestableWorkload {
	return func(ctx context.Context, k8sClient client.Client, namespace string, name string) TestableWorkload {
		return JobTestableWorkload{job: fn(ctx, k8sClient, namespace, name)}
	}
}

type PodTestableWorkload struct {
	pod *corev1.Pod
}

func (w PodTestableWorkload) Get() client.Object { return w.pod }
func (w PodTestableWorkload) GetObjectMeta() *metav1.ObjectMeta {
	return &w.pod.ObjectMeta
}
func WrapPodFnAsTestableWorkload(
	fn func(context.Context, client.Client, string, string) *corev1.Pod,
) func(context.Context, client.Client, string, string) TestableWorkload {
	return func(ctx context.Context, k8sClient client.Client, namespace string, name string) TestableWorkload {
		return PodTestableWorkload{pod: fn(ctx, k8sClient, namespace, name)}
	}
}

type ReplicaSetTestableWorkload struct {
	replicaSet *appsv1.ReplicaSet
}

func (w ReplicaSetTestableWorkload) Get() client.Object { return w.replicaSet }
func (w ReplicaSetTestableWorkload) GetObjectMeta() *metav1.ObjectMeta {
	return &w.replicaSet.ObjectMeta
}
func WrapReplicaSetFnAsTestableWorkload(
	fn func(context.Context, client.Client, string, string) *appsv1.ReplicaSet,
) func(context.Context, client.Client, string, string) TestableWorkload {
	return func(ctx context.Context, k8sClient client.Client, namespace string, name string) TestableWorkload {
		return ReplicaSetTestableWorkload{replicaSet: fn(ctx, k8sClient, namespace, name)}
	}
}

type StatefulSetTestableWorkload struct {
	statefulSet *appsv1.StatefulSet
}

func (w StatefulSetTestableWorkload) Get() client.Object { return w.statefulSet }
func (w StatefulSetTestableWorkload) GetObjectMeta() *metav1.ObjectMeta {
	return &w.statefulSet.ObjectMeta
}
func WrapStatefulSetFnAsTestableWorkload(
	fn func(context.Context, client.Client, string, string) *appsv1.StatefulSet,
) func(context.Context, client.Client, string, string) TestableWorkload {
	return func(ctx context.Context, k8sClient client.Client, namespace string, name string) TestableWorkload {
		return StatefulSetTestableWorkload{statefulSet: fn(ctx, k8sClient, namespace, name)}
	}
}
