// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"encoding/json"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GkeAutopilotResourceAdjustmentAnnotation is the annotation GKE Autopilot adds to a workload when it has adjusted
// the resource requests and limits of its containers. It records the resources as they have been submitted ("input")
// and as they have been stored after the adjustment ("output").
const GkeAutopilotResourceAdjustmentAnnotation = "autopilot.gke.io/resource-adjustment"

type gkeAutopilotResourceAdjustment struct {
	Input gkeAutopilotResourceAdjustmentPodSpec `json:"input"`
}

type gkeAutopilotResourceAdjustmentPodSpec struct {
	InitContainers []gkeAutopilotResourceAdjustmentContainer `json:"initContainers,omitempty"`
	Containers     []gkeAutopilotResourceAdjustmentContainer `json:"containers,omitempty"`
}

type gkeAutopilotResourceAdjustmentContainer struct {
	Name     string              `json:"name"`
	Requests corev1.ResourceList `json:"requests,omitempty"`
	Limits   corev1.ResourceList `json:"limits,omitempty"`
}

// AdoptGkeAutopilotResourceAdjustments copies the container resources of the existing (live) workload into the
// desired workload, for every container whose desired resources are exactly the resources GKE Autopilot has recorded
// as its input in the autopilot.gke.io/resource-adjustment annotation of the existing workload.
//
// GKE Autopilot mutates the resource requests and limits of every workload it admits, for example it adds CPU
// requests or raises memory requests to satisfy its minimums and its CPU to memory ratio. Comparing the desired
// workload with the live workload would then always report a difference, and updating the workload would only make
// Autopilot adjust the resources again, which in turn triggers the next reconciliation, indefinitely. Adopting the
// adjusted resources for the comparison makes a workload that only differs by Autopilot's adjustments count as
// unchanged. Containers whose desired resources differ from the recorded input (because the configured resources
// have changed since the last update) keep their desired resources, so the change is still applied.
//
// Only use the modified desired object for comparing it with the existing object, and send the unmodified desired
// object when updating the workload, so that Autopilot keeps recording the configured resources as its input.
//
// Objects other than Deployments and DaemonSets, and workloads without a parseable annotation, are left unchanged.
func AdoptGkeAutopilotResourceAdjustments(existing client.Object, desired client.Object) {
	existingPodSpec, desiredPodSpec := podSpecsOf(existing, desired)
	if existingPodSpec == nil || desiredPodSpec == nil {
		return
	}
	rawAdjustment, ok := existing.GetAnnotations()[GkeAutopilotResourceAdjustmentAnnotation]
	if !ok {
		return
	}
	var adjustment gkeAutopilotResourceAdjustment
	if err := json.Unmarshal([]byte(rawAdjustment), &adjustment); err != nil {
		return
	}
	adoptAdjustedResources(adjustment.Input.InitContainers, existingPodSpec.InitContainers, desiredPodSpec.InitContainers)
	adoptAdjustedResources(adjustment.Input.Containers, existingPodSpec.Containers, desiredPodSpec.Containers)
}

func podSpecsOf(existing client.Object, desired client.Object) (*corev1.PodSpec, *corev1.PodSpec) {
	switch desiredWorkload := desired.(type) {
	case *appsv1.Deployment:
		if existingWorkload, ok := existing.(*appsv1.Deployment); ok {
			return &existingWorkload.Spec.Template.Spec, &desiredWorkload.Spec.Template.Spec
		}
	case *appsv1.DaemonSet:
		if existingWorkload, ok := existing.(*appsv1.DaemonSet); ok {
			return &existingWorkload.Spec.Template.Spec, &desiredWorkload.Spec.Template.Spec
		}
	}
	return nil, nil
}

func adoptAdjustedResources(
	adjustmentInput []gkeAutopilotResourceAdjustmentContainer,
	existingContainers []corev1.Container,
	desiredContainers []corev1.Container,
) {
	for i := range desiredContainers {
		desiredContainer := &desiredContainers[i]
		input := findAdjustmentInput(adjustmentInput, desiredContainer.Name)
		if input == nil {
			continue
		}
		existingContainer := findContainer(existingContainers, desiredContainer.Name)
		if existingContainer == nil {
			continue
		}
		if !apiequality.Semantic.DeepEqual(desiredContainer.Resources.Requests, input.Requests) ||
			!apiequality.Semantic.DeepEqual(desiredContainer.Resources.Limits, input.Limits) {
			continue
		}
		desiredContainer.Resources = *existingContainer.Resources.DeepCopy()
	}
}

func findAdjustmentInput(
	adjustmentInput []gkeAutopilotResourceAdjustmentContainer,
	name string,
) *gkeAutopilotResourceAdjustmentContainer {
	for i := range adjustmentInput {
		if adjustmentInput[i].Name == name {
			return &adjustmentInput[i]
		}
	}
	return nil
}

func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}
