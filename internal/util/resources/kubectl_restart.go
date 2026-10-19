// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// KubectlRestartedAtAnnotation is the pod template annotation that kubectl rollout restart sets to the current time,
// which changes the pod template and thereby triggers a rollout.
const KubectlRestartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

// PreserveKubectlRestartedAtAnnotation copies the kubectl.kubernetes.io/restartedAt annotation of the existing pod
// template into the desired one, so that a full update does not revert a kubectl rollout restart. Call it after setting
// the last-applied annotation, so the annotation stays a live-only field that never counts as a difference.
func PreserveKubectlRestartedAtAnnotation(existing client.Object, desired client.Object, logger logd.Logger) {
	existingTemplate, desiredTemplate := podTemplatesOf(existing, desired)
	if existingTemplate == nil || desiredTemplate == nil {
		return
	}
	restartedAt, ok := existingTemplate.Annotations[KubectlRestartedAtAnnotation]
	if !ok {
		return
	}
	if desiredTemplate.Annotations == nil {
		desiredTemplate.Annotations = map[string]string{}
	}
	desiredTemplate.Annotations[KubectlRestartedAtAnnotation] = restartedAt
	logger.Debug(
		"preserving the kubectl rollout restart annotation of the pod template",
		"namespace", existing.GetNamespace(),
		"name", existing.GetName(),
		"annotation", KubectlRestartedAtAnnotation,
		"value", restartedAt,
	)
}
