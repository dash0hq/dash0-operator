// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetReceiverForWorkloadType(
	apiVersion string,
	kind string,
) (client.Object, error) {

	if apiVersion == "apps/v1" {
		switch kind {
		case "DaemonSet":
			return &appsv1.DaemonSet{}, nil
		case "Deployment":
			return &appsv1.Deployment{}, nil
		case "ReplicaSet":
			return &appsv1.ReplicaSet{}, nil
		case "StatefulSet":
			return &appsv1.StatefulSet{}, nil
		}
	} else if apiVersion == "batch/v1" {
		switch kind {
		case "CronJob":
			return &batchv1.CronJob{}, nil
		case "Job":
			return &batchv1.Job{}, nil
		}
	} else if apiVersion == "v1" /* this is core/v1 */ {
		switch kind {
		case "Pod":
			return &corev1.Pod{}, nil
		}
	}

	return nil, fmt.Errorf(
		"unexpected combination of APIVersion and Kind for referenced object: '%s/%s'", apiVersion, kind)
}
