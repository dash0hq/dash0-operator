// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const restartedAt = "2026-10-15T10:00:00Z"

func podTemplateWithAnnotations(annotations map[string]string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: annotations}}
}

func deploymentWithPodTemplateAnnotations(annotations map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "namespace"},
		Spec:       appsv1.DeploymentSpec{Template: podTemplateWithAnnotations(annotations)},
	}
}

func daemonSetWithPodTemplateAnnotations(annotations map[string]string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "namespace"},
		Spec:       appsv1.DaemonSetSpec{Template: podTemplateWithAnnotations(annotations)},
	}
}

var _ = Describe("PreserveKubectlRestartedAtAnnotation", func() {
	logger := logd.Discard()

	It("copies the annotation into the pod template of a deployment without pod template annotations", func() {
		existing := deploymentWithPodTemplateAnnotations(map[string]string{KubectlRestartedAtAnnotation: restartedAt})
		desired := deploymentWithPodTemplateAnnotations(nil)

		PreserveKubectlRestartedAtAnnotation(existing, desired, logger)

		Expect(desired.Spec.Template.Annotations).To(
			Equal(map[string]string{KubectlRestartedAtAnnotation: restartedAt}))
	})

	It("copies the annotation into the pod template of a daemonset and keeps its other annotations", func() {
		existing := daemonSetWithPodTemplateAnnotations(map[string]string{
			KubectlRestartedAtAnnotation: restartedAt,
			"live-only":                  "value",
		})
		desired := daemonSetWithPodTemplateAnnotations(map[string]string{"desired": "value"})

		PreserveKubectlRestartedAtAnnotation(existing, desired, logger)

		Expect(desired.Spec.Template.Annotations).To(Equal(map[string]string{
			KubectlRestartedAtAnnotation: restartedAt,
			"desired":                    "value",
		}))
	})

	It("does nothing when the existing workload has not been restarted", func() {
		existing := deploymentWithPodTemplateAnnotations(map[string]string{"live-only": "value"})
		desired := deploymentWithPodTemplateAnnotations(nil)

		PreserveKubectlRestartedAtAnnotation(existing, desired, logger)

		Expect(desired.Spec.Template.Annotations).To(BeNil())
	})

	It("does nothing for objects other than deployments and daemonsets", func() {
		existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{KubectlRestartedAtAnnotation: restartedAt},
		}}
		desired := &corev1.ConfigMap{}

		PreserveKubectlRestartedAtAnnotation(existing, desired, logger)

		Expect(desired.Annotations).To(BeNil())
	})

	It("does nothing when the kinds of the existing and the desired object differ", func() {
		existing := daemonSetWithPodTemplateAnnotations(map[string]string{KubectlRestartedAtAnnotation: restartedAt})
		desired := deploymentWithPodTemplateAnnotations(nil)

		PreserveKubectlRestartedAtAnnotation(existing, desired, logger)

		Expect(desired.Spec.Template.Annotations).To(BeNil())
	})
})
