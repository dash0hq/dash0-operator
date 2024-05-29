// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
)

type instrumentableWorkload interface {
	getObjectMeta() *metav1.ObjectMeta
	getKind() string
	asRuntimeObject() runtime.Object
	asClientObject() client.Object
	instrument(*logr.Logger) bool
	revert(*logr.Logger) bool
}

type cronJobWorkload struct {
	cronJob  *batchv1.CronJob
	versions util.Versions
}

func (w *cronJobWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.cronJob.ObjectMeta }
func (w *cronJobWorkload) getKind() string                   { return "CronJob" }
func (w *cronJobWorkload) asRuntimeObject() runtime.Object   { return w.cronJob }
func (w *cronJobWorkload) asClientObject() client.Object     { return w.cronJob }
func (w *cronJobWorkload) instrument(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).ModifyCronJob(w.cronJob)
}
func (w *cronJobWorkload) revert(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).RevertCronJob(w.cronJob)
}

type daemonSetWorkload struct {
	daemonSet *appsv1.DaemonSet
	versions  util.Versions
}

func (w *daemonSetWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.daemonSet.ObjectMeta }
func (w *daemonSetWorkload) getKind() string                   { return "DaemonSet" }
func (w *daemonSetWorkload) asRuntimeObject() runtime.Object   { return w.daemonSet }
func (w *daemonSetWorkload) asClientObject() client.Object     { return w.daemonSet }
func (w *daemonSetWorkload) instrument(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).ModifyDaemonSet(w.daemonSet)
}
func (w *daemonSetWorkload) revert(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).RevertDaemonSet(w.daemonSet)
}

type deploymentWorkload struct {
	deployment *appsv1.Deployment
	versions   util.Versions
}

func (w *deploymentWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.deployment.ObjectMeta }
func (w *deploymentWorkload) getKind() string                   { return "Deployment" }
func (w *deploymentWorkload) asRuntimeObject() runtime.Object   { return w.deployment }
func (w *deploymentWorkload) asClientObject() client.Object     { return w.deployment }
func (w *deploymentWorkload) instrument(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).ModifyDeployment(w.deployment)
}
func (w *deploymentWorkload) revert(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).RevertDeployment(w.deployment)
}

type replicaSetWorkload struct {
	replicaSet *appsv1.ReplicaSet
	versions   util.Versions
}

func (w *replicaSetWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.replicaSet.ObjectMeta }
func (w *replicaSetWorkload) getKind() string                   { return "ReplicaSet" }
func (w *replicaSetWorkload) asRuntimeObject() runtime.Object   { return w.replicaSet }
func (w *replicaSetWorkload) asClientObject() client.Object     { return w.replicaSet }
func (w *replicaSetWorkload) instrument(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).ModifyReplicaSet(w.replicaSet)
}
func (w *replicaSetWorkload) revert(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).RevertReplicaSet(w.replicaSet)
}

type statefulSetWorkload struct {
	statefulSet *appsv1.StatefulSet
	versions    util.Versions
}

func (w *statefulSetWorkload) getObjectMeta() *metav1.ObjectMeta { return &w.statefulSet.ObjectMeta }
func (w *statefulSetWorkload) getKind() string                   { return "StatefulSet" }
func (w *statefulSetWorkload) asRuntimeObject() runtime.Object   { return w.statefulSet }
func (w *statefulSetWorkload) asClientObject() client.Object     { return w.statefulSet }
func (w *statefulSetWorkload) instrument(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).ModifyStatefulSet(w.statefulSet)
}
func (w *statefulSetWorkload) revert(logger *logr.Logger) bool {
	return newWorkloadModifier(w.versions, logger).RevertStatefulSet(w.statefulSet)
}
