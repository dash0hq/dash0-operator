// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const progressDeadlineExceededReason = "ProgressDeadlineExceeded"

// RolloutStatus is the result of EvaluateDeploymentRollout.
type RolloutStatus struct {
	Complete bool
	// Failed is set if the deployment controller has given up on the rollout (progress deadline exceeded).
	Failed  bool
	Message string
}

// EvaluateDeploymentRollout applies the criteria of "kubectl rollout status" to a deployment: the controller has
// observed the latest spec, all replicas have been updated, no replicas of an older revision remain, and all updated
// replicas are available.
func EvaluateDeploymentRollout(deployment *appsv1.Deployment) RolloutStatus {
	if deployment.Status.ObservedGeneration < deployment.Generation {
		return RolloutStatus{Message: "waiting for the deployment spec update to be observed"}
	}
	// Conditions are only evaluated once the current generation has been observed, an older rollout can have left a
	// stale condition.
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing &&
			condition.Status == corev1.ConditionFalse &&
			condition.Reason == progressDeadlineExceededReason {
			return RolloutStatus{Failed: true, Message: fmt.Sprintf("the rollout has failed: %s", condition.Message)}
		}
	}
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}
	if deployment.Status.UpdatedReplicas < desiredReplicas {
		return RolloutStatus{Message: fmt.Sprintf(
			"%d of %d replicas have been updated", deployment.Status.UpdatedReplicas, desiredReplicas,
		)}
	}
	if deployment.Status.Replicas > deployment.Status.UpdatedReplicas {
		return RolloutStatus{Message: fmt.Sprintf(
			"%d replicas of an older revision are still running",
			deployment.Status.Replicas-deployment.Status.UpdatedReplicas,
		)}
	}
	if deployment.Status.AvailableReplicas < deployment.Status.UpdatedReplicas {
		return RolloutStatus{Message: fmt.Sprintf(
			"%d of %d updated replicas are available",
			deployment.Status.AvailableReplicas,
			deployment.Status.UpdatedReplicas,
		)}
	}
	return RolloutStatus{Complete: true, Message: "the rollout is complete"}
}
