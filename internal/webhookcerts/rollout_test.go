// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Evaluating a deployment rollout", func() {
	progressDeadlineExceeded := appsv1.DeploymentCondition{
		Type:    appsv1.DeploymentProgressing,
		Status:  corev1.ConditionFalse,
		Reason:  progressDeadlineExceededReason,
		Message: "ReplicaSet has timed out progressing.",
	}

	It("reports a complete rollout", func() {
		Expect(EvaluateDeploymentRollout(testDeployment(true)).Complete).To(BeTrue())
	})

	It("defaults to one desired replica", func() {
		deployment := testDeployment(true)
		deployment.Spec.Replicas = nil
		deployment.Status = appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}
		Expect(EvaluateDeploymentRollout(deployment).Complete).To(BeTrue())
	})

	It("waits until the latest generation has been observed, and ignores conditions of an older generation", func() {
		deployment := testDeployment(true)
		deployment.Generation = 3
		deployment.Status.Conditions = []appsv1.DeploymentCondition{progressDeadlineExceeded}
		status := EvaluateDeploymentRollout(deployment)
		Expect(status.Complete).To(BeFalse())
		Expect(status.Failed).To(BeFalse())
	})

	It("reports a failed rollout", func() {
		deployment := testDeployment(false)
		deployment.Status.Conditions = []appsv1.DeploymentCondition{progressDeadlineExceeded}
		status := EvaluateDeploymentRollout(deployment)
		Expect(status.Complete).To(BeFalse())
		Expect(status.Failed).To(BeTrue())
	})

	It("waits until all replicas have been updated", func() {
		deployment := testDeployment(true)
		deployment.Status.UpdatedReplicas = 1
		Expect(EvaluateDeploymentRollout(deployment).Complete).To(BeFalse())
	})

	It("waits until no replica of an older revision remains", func() {
		Expect(EvaluateDeploymentRollout(testDeployment(false)).Complete).To(BeFalse())
	})

	It("waits until all updated replicas are available", func() {
		deployment := testDeployment(true)
		deployment.Status.AvailableReplicas = 1
		Expect(EvaluateDeploymentRollout(deployment).Complete).To(BeFalse())
	})
})
