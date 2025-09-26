// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	K8sApiVersionAppsV1  = "apps/v1"
	K8sApiVersionBatchV1 = "batch/v1"
	// K8sApiVersionCoreV1 this is actually the string used for core/v1
	K8sApiVersionCoreV1 = "v1"
)

var (
	K8sTypeMetaDaemonSet = metav1.TypeMeta{
		APIVersion: K8sApiVersionAppsV1,
		Kind:       "DaemonSet",
	}
	K8sTypeMetaDeployment = metav1.TypeMeta{
		APIVersion: K8sApiVersionAppsV1,
		Kind:       "Deployment",
	}
	K8sTypeMetaReplicaSet = metav1.TypeMeta{
		APIVersion: K8sApiVersionAppsV1,
		Kind:       "ReplicaSet",
	}
	K8sTypeMetaStatefulSet = metav1.TypeMeta{
		APIVersion: K8sApiVersionAppsV1,
		Kind:       "StatefulSet",
	}
	K8sTypeMetaCronJob = metav1.TypeMeta{
		APIVersion: K8sApiVersionBatchV1,
		Kind:       "CronJob",
	}
	K8sTypeMetaJob = metav1.TypeMeta{
		APIVersion: K8sApiVersionBatchV1,
		Kind:       "Job",
	}
	K8sTypeMetaPod = metav1.TypeMeta{
		APIVersion: K8sApiVersionCoreV1,
		Kind:       "Pod",
	}
)

func GetReceiverForWorkloadType(
	apiVersion string,
	kind string,
) (client.Object, error) {
	switch apiVersion {
	case K8sApiVersionAppsV1:
		switch kind {
		case K8sTypeMetaDaemonSet.Kind:
			return &appsv1.DaemonSet{}, nil
		case K8sTypeMetaDeployment.Kind:
			return &appsv1.Deployment{}, nil
		case K8sTypeMetaReplicaSet.Kind:
			return &appsv1.ReplicaSet{}, nil
		case K8sTypeMetaStatefulSet.Kind:
			return &appsv1.StatefulSet{}, nil
		}
	case K8sApiVersionBatchV1:
		switch kind {
		case K8sTypeMetaCronJob.Kind:
			return &batchv1.CronJob{}, nil
		case K8sTypeMetaJob.Kind:
			return &batchv1.Job{}, nil
		}
	case K8sApiVersionCoreV1:
		switch kind {
		case K8sTypeMetaPod.Kind:
			return &corev1.Pod{}, nil
		}
	}

	return nil, fmt.Errorf(
		"unexpected combination of APIVersion and Kind for referenced object: '%s/%s'", apiVersion, kind)
}
