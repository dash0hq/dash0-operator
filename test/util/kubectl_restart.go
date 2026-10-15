// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	KubectlRestartedAtAnnotationTest = "kubectl.kubernetes.io/restartedAt"
	KubectlRestartedAtTest           = "2026-10-15T10:00:00Z"
)

// SimulateKubectlRolloutRestart adds the annotation that kubectl rollout restart sets to the given pod template. The
// caller needs to update the workload afterward.
func SimulateKubectlRolloutRestart(podTemplate *corev1.PodTemplateSpec) {
	if podTemplate.Annotations == nil {
		podTemplate.Annotations = map[string]string{}
	}
	podTemplate.Annotations[KubectlRestartedAtAnnotationTest] = KubectlRestartedAtTest
}
