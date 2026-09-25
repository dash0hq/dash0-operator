// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
)

type gkeAutopilotContainerResources struct {
	Name     string              `json:"name"`
	Requests corev1.ResourceList `json:"requests,omitempty"`
	Limits   corev1.ResourceList `json:"limits,omitempty"`
}

// SimulateGkeAutopilotResourceAdjustment modifies the container resources of the given workload the way the GKE
// Autopilot admission webhook does: it adds CPU and ephemeral-storage requests, raises the memory requests and limits,
// and records the original resources in the autopilot.gke.io/resource-adjustment annotation. The caller needs to
// update the workload afterward.
func SimulateGkeAutopilotResourceAdjustment(workload client.Object, podSpec *corev1.PodSpec) {
	input := map[string][]gkeAutopilotContainerResources{
		"initContainers": simulateGkeAutopilotResourceAdjustmentForContainers(podSpec.InitContainers),
		"containers":     simulateGkeAutopilotResourceAdjustmentForContainers(podSpec.Containers),
	}
	annotation, err := json.Marshal(map[string]any{"input": input, "modified": true})
	Expect(err).ToNot(HaveOccurred())
	annotations := workload.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["autopilot.gke.io/resource-adjustment"] = string(annotation)
	workload.SetAnnotations(annotations)
}

func simulateGkeAutopilotResourceAdjustmentForContainers(
	containers []corev1.Container,
) []gkeAutopilotContainerResources {
	input := make([]gkeAutopilotContainerResources, 0, len(containers))
	additionalMemory := resource.MustParse("78Mi")
	for i := range containers {
		container := &containers[i]
		input = append(input, gkeAutopilotContainerResources{
			Name:     container.Name,
			Requests: container.Resources.Requests.DeepCopy(),
			Limits:   container.Resources.Limits.DeepCopy(),
		})
		if container.Resources.Requests == nil {
			container.Resources.Requests = corev1.ResourceList{}
		}
		if container.Resources.Limits == nil {
			container.Resources.Limits = corev1.ResourceList{}
		}
		for _, resourceList := range []corev1.ResourceList{container.Resources.Requests, container.Resources.Limits} {
			if memory, ok := resourceList[corev1.ResourceMemory]; ok {
				memory.Add(additionalMemory)
				resourceList[corev1.ResourceMemory] = memory
			}
			resourceList[corev1.ResourceEphemeralStorage] = resource.MustParse("1Gi")
		}
		container.Resources.Requests[corev1.ResourceCPU] = resource.MustParse("76m")
	}
	return input
}
