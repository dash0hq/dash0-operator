// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import (
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/workloads"
)

type instrumentableWorkload interface {
	getObjectMeta() *metav1.ObjectMeta
	getPodSpec() corev1.PodSpec
	getKind() string
	asRuntimeObject() runtime.Object
	asClientObject() client.Object
	instrument(
		clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
		namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
		logger *logr.Logger,
	) workloads.ModificationResult
	revert(
		clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
		namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
		logger *logr.Logger,
	) workloads.ModificationResult
}

type cronJobWorkload struct {
	cronJob *batchv1.CronJob
}

func (w *cronJobWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.cronJob.ObjectMeta }
func (w *cronJobWorkload) getPodSpec() corev1.PodSpec {
	return w.cronJob.Spec.JobTemplate.Spec.Template.Spec
}
func (w *cronJobWorkload) getKind() string                 { return "CronJob" }
func (w *cronJobWorkload) asRuntimeObject() runtime.Object { return w.cronJob }
func (w *cronJobWorkload) asClientObject() client.Object   { return w.cronJob }
func (w *cronJobWorkload) instrument(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).ModifyCronJob(w.cronJob)
}
func (w *cronJobWorkload) revert(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).RevertCronJob(w.cronJob)
}

type daemonSetWorkload struct {
	daemonSet *appsv1.DaemonSet
}

func (w *daemonSetWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.daemonSet.ObjectMeta }
func (w *daemonSetWorkload) getPodSpec() corev1.PodSpec {
	return w.daemonSet.Spec.Template.Spec
}
func (w *daemonSetWorkload) getKind() string                 { return "DaemonSet" }
func (w *daemonSetWorkload) asRuntimeObject() runtime.Object { return w.daemonSet }
func (w *daemonSetWorkload) asClientObject() client.Object   { return w.daemonSet }
func (w *daemonSetWorkload) instrument(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).ModifyDaemonSet(w.daemonSet)
}
func (w *daemonSetWorkload) revert(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).RevertDaemonSet(w.daemonSet)
}

type deploymentWorkload struct {
	deployment *appsv1.Deployment
}

func (w *deploymentWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.deployment.ObjectMeta }
func (w *deploymentWorkload) getPodSpec() corev1.PodSpec {
	return w.deployment.Spec.Template.Spec
}
func (w *deploymentWorkload) getKind() string                 { return "Deployment" }
func (w *deploymentWorkload) asRuntimeObject() runtime.Object { return w.deployment }
func (w *deploymentWorkload) asClientObject() client.Object   { return w.deployment }
func (w *deploymentWorkload) instrument(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).ModifyDeployment(w.deployment)
}
func (w *deploymentWorkload) revert(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).RevertDeployment(w.deployment)
}

type replicaSetWorkload struct {
	replicaSet *appsv1.ReplicaSet
}

func (w *replicaSetWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.replicaSet.ObjectMeta }
func (w *replicaSetWorkload) getPodSpec() corev1.PodSpec {
	return w.replicaSet.Spec.Template.Spec
}
func (w *replicaSetWorkload) getKind() string                 { return "ReplicaSet" }
func (w *replicaSetWorkload) asRuntimeObject() runtime.Object { return w.replicaSet }
func (w *replicaSetWorkload) asClientObject() client.Object   { return w.replicaSet }
func (w *replicaSetWorkload) instrument(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).ModifyReplicaSet(w.replicaSet)
}
func (w *replicaSetWorkload) revert(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).RevertReplicaSet(w.replicaSet)
}

type statefulSetWorkload struct {
	statefulSet *appsv1.StatefulSet
}

func (w *statefulSetWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.statefulSet.ObjectMeta }
func (w *statefulSetWorkload) getPodSpec() corev1.PodSpec {
	return w.statefulSet.Spec.Template.Spec
}
func (w *statefulSetWorkload) getKind() string                 { return "StatefulSet" }
func (w *statefulSetWorkload) asRuntimeObject() runtime.Object { return w.statefulSet }
func (w *statefulSetWorkload) asClientObject() client.Object   { return w.statefulSet }
func (w *statefulSetWorkload) instrument(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).ModifyStatefulSet(w.statefulSet)
}
func (w *statefulSetWorkload) revert(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	namespaceInstrumentationConfig util.NamespaceInstrumentationConfig,
	logger *logr.Logger,
) workloads.ModificationResult {
	return newWorkloadModifier(clusterInstrumentationConfig, namespaceInstrumentationConfig, logger).RevertStatefulSet(w.statefulSet)
}
