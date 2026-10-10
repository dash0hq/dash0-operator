// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// CreatePreDeleteHookJob creates a job that looks like the Helm chart's pre-delete hook job, which signals that the
// operator is being uninstalled, see util.UninstallationDetector.
func CreatePreDeleteHookJob(ctx context.Context, k8sClient client.Client) *batchv1.Job {
	By("creating the pre-delete hook job")
	EnsureOperatorNamespaceExists(ctx, k8sClient)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dash0-operator-pre-delete",
			Namespace: OperatorNamespace,
			Labels: map[string]string{
				util.AppKubernetesIoComponentLabel: util.UninstallationProcessComponent,
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{{
						Name:  "pre-delete-job",
						Image: "operator-controller:test",
					}},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, job)).To(Succeed())
	return job
}

func DeletePreDeleteHookJob(ctx context.Context, k8sClient client.Client, job *batchv1.Job) {
	By("deleting the pre-delete hook job")
	Expect(client.IgnoreNotFound(
		k8sClient.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)),
	)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{})).ToNot(Succeed())
	}).Should(Succeed())
}
