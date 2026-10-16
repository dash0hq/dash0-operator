// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("indicatesUninstallation", func() {
	operatorCreatedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	DescribeTable("detects pre-delete hook jobs of an ongoing uninstallation",
		func(createdAt time.Time, conditions []batchv1.JobCondition, expected bool) {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(createdAt)},
				Status:     batchv1.JobStatus{Conditions: conditions},
			}
			Expect(indicatesUninstallation(job, operatorCreatedAt)).To(Equal(expected))
		},
		Entry("running", operatorCreatedAt.Add(time.Hour), nil, true),
		Entry("complete", operatorCreatedAt.Add(time.Hour), []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}, true),
		Entry("failed", operatorCreatedAt.Add(time.Hour), []batchv1.JobCondition{
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
		}, false),
		Entry("failed condition not true", operatorCreatedAt.Add(time.Hour), []batchv1.JobCondition{
			{Type: batchv1.JobFailed, Status: corev1.ConditionFalse},
		}, true),
		Entry("created in the same second as the operator", operatorCreatedAt, nil, true),
		Entry("created before the operator", operatorCreatedAt.Add(-time.Second), []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}, false),
	)
})
