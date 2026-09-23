// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	configuredResources = corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
		Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
	}
	adjustedResources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory:           resource.MustParse("578Mi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("76m"),
			corev1.ResourceMemory:           resource.MustParse("578Mi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
		},
	}
	configuredSidecarResources = corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("26Mi")},
		Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("12Mi")},
	}
	adjustedSidecarResources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("26Mi")},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("12Mi"),
		},
	}

	autopilotAnnotation = `{"input":{"initContainers":[{"limits":{"memory":"500Mi"},"requests":{"memory":"500Mi"},` +
		`"name":"init"}],"containers":[{"limits":{"memory":"500Mi"},"requests":{"memory":"500Mi"},"name":"main"},` +
		`{"limits":{"memory":"26Mi"},"requests":{"memory":"12Mi"},"name":"sidecar"}]},"output":{},"modified":true}`
)

func podSpecWith(initResources, mainResources, sidecarResources corev1.ResourceRequirements) corev1.PodSpec {
	return corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "init", Resources: *initResources.DeepCopy()}},
		Containers: []corev1.Container{
			{Name: "main", Resources: *mainResources.DeepCopy()},
			{Name: "sidecar", Resources: *sidecarResources.DeepCopy()},
		},
	}
}

func deploymentWith(annotations map[string]string, podSpec corev1.PodSpec) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "namespace", Annotations: annotations},
		Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: podSpec}},
	}
}

func daemonSetWith(annotations map[string]string, podSpec corev1.PodSpec) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "namespace", Annotations: annotations},
		Spec:       appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: podSpec}},
	}
}

func withAutopilotAnnotation(value string) map[string]string {
	return map[string]string{GkeAutopilotResourceAdjustmentAnnotation: value}
}

var _ = Describe("AdoptGkeAutopilotResourceAdjustments", func() {
	adjustedPodSpec := func() corev1.PodSpec {
		return podSpecWith(adjustedResources, adjustedResources, adjustedSidecarResources)
	}
	configuredPodSpec := func() corev1.PodSpec {
		return podSpecWith(configuredResources, configuredResources, configuredSidecarResources)
	}

	It("adopts the adjusted resources of all containers of a deployment when they match the recorded input", func() {
		existing := deploymentWith(withAutopilotAnnotation(autopilotAnnotation), adjustedPodSpec())
		desired := deploymentWith(nil, configuredPodSpec())

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Spec.Template.Spec).To(Equal(adjustedPodSpec()))
	})

	It("adopts the adjusted resources of all containers of a daemonset when they match the recorded input", func() {
		existing := daemonSetWith(withAutopilotAnnotation(autopilotAnnotation), adjustedPodSpec())
		desired := daemonSetWith(nil, configuredPodSpec())

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Spec.Template.Spec).To(Equal(adjustedPodSpec()))
	})

	It("does not modify the existing object", func() {
		existing := deploymentWith(withAutopilotAnnotation(autopilotAnnotation), adjustedPodSpec())
		desired := deploymentWith(nil, configuredPodSpec())

		AdoptGkeAutopilotResourceAdjustments(existing, desired)
		desired.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("1")

		Expect(existing.Spec.Template.Spec).To(Equal(adjustedPodSpec()))
	})

	It("keeps the desired resources of a container whose configured resources differ from the recorded input", func() {
		changedResources := corev1.ResourceRequirements{
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
		}
		existing := deploymentWith(withAutopilotAnnotation(autopilotAnnotation), adjustedPodSpec())
		desired := deploymentWith(nil, podSpecWith(configuredResources, changedResources, configuredSidecarResources))

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Spec.Template.Spec).To(
			Equal(podSpecWith(adjustedResources, changedResources, adjustedSidecarResources)))
	})

	It("keeps the desired resources of a container that is not recorded in the input", func() {
		existing := deploymentWith(withAutopilotAnnotation(
			`{"input":{"containers":[{"limits":{"memory":"500Mi"},"requests":{"memory":"500Mi"},"name":"main"}]}}`,
		), adjustedPodSpec())
		desired := deploymentWith(nil, configuredPodSpec())

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Spec.Template.Spec).To(
			Equal(podSpecWith(configuredResources, adjustedResources, configuredSidecarResources)))
	})

	It("keeps the desired resources of a container that does not exist in the existing object", func() {
		existingPodSpec := adjustedPodSpec()
		existingPodSpec.Containers = existingPodSpec.Containers[:1]
		existing := deploymentWith(withAutopilotAnnotation(autopilotAnnotation), existingPodSpec)
		desired := deploymentWith(nil, configuredPodSpec())

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Spec.Template.Spec).To(
			Equal(podSpecWith(adjustedResources, adjustedResources, configuredSidecarResources)))
	})

	It("compares quantities semantically", func() {
		existing := deploymentWith(withAutopilotAnnotation(
			`{"input":{"containers":[{"limits":{"memory":"1Gi"},"name":"main"}]}}`,
		), adjustedPodSpec())
		desired := deploymentWith(nil, podSpecWith(configuredResources, corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1024Mi")},
		}, configuredSidecarResources))

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Spec.Template.Spec.Containers[0].Resources).To(Equal(adjustedResources))
	})

	It("does nothing when the existing object has no resource adjustment annotation", func() {
		existing := deploymentWith(nil, adjustedPodSpec())
		desired := deploymentWith(nil, configuredPodSpec())

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Spec.Template.Spec).To(Equal(configuredPodSpec()))
	})

	It("does nothing when the resource adjustment annotation cannot be parsed", func() {
		existing := deploymentWith(withAutopilotAnnotation("{not json"), adjustedPodSpec())
		desired := deploymentWith(nil, configuredPodSpec())

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Spec.Template.Spec).To(Equal(configuredPodSpec()))
	})

	It("does nothing for objects other than deployments and daemonsets", func() {
		existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Annotations: withAutopilotAnnotation(autopilotAnnotation),
		}}
		desired := &corev1.ConfigMap{Data: map[string]string{"key": "value"}}

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Data).To(Equal(map[string]string{"key": "value"}))
	})

	It("does nothing when the kinds of the existing and the desired object differ", func() {
		existing := daemonSetWith(withAutopilotAnnotation(autopilotAnnotation), adjustedPodSpec())
		desired := deploymentWith(nil, configuredPodSpec())

		AdoptGkeAutopilotResourceAdjustments(existing, desired)

		Expect(desired.Spec.Template.Spec).To(Equal(configuredPodSpec()))
	})
})
