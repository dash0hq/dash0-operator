// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// nodeSELinuxInspectionLimit bounds the number of nodes DetectNodeSELinuxIndication inspects, it reads a single page
// of nodes.
const nodeSELinuxInspectionLimit = 100

// NodeSELinuxIndication is the outcome of DetectNodeSELinuxIndication.
type NodeSELinuxIndication struct {
	// Indication describes why the container runtime of the nodes likely confines containers with SELinux, empty if
	// none of the inspected nodes shows such an indication.
	Indication string
	// InspectionFailed is true if the nodes could not be listed, so that nothing is known about SELinux on the nodes.
	InspectionFailed bool
}

// DetectNodeSELinuxIndication inspects the nodes for indications that their container runtime confines containers
// with SELinux, in which case containers with the default SELinux type container_t cannot write to hostPath volumes,
// since the kubelet does not relabel them. The indications are the container runtime CRI-O and the Kubernetes
// distributions RKE2, K3s and k0s, which support SELinux-enforcing nodes. These are heuristics: whether SELinux is
// actually enforcing cannot be read from the Kubernetes API. Only the first nodeSELinuxInspectionLimit nodes are
// inspected, with a single API request.
func DetectNodeSELinuxIndication(
	ctx context.Context,
	clientset kubernetes.Interface,
	logger logd.Logger,
) NodeSELinuxIndication {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: nodeSELinuxInspectionLimit})
	if err != nil {
		logger.ErrorAsWarn(err, "cannot list nodes to check whether their container runtime confines containers with "+
			"SELinux")
		return NodeSELinuxIndication{InspectionFailed: true}
	}
	for i := range nodes.Items {
		if indication := nodeSELinuxIndication(&nodes.Items[i]); indication != "" {
			return NodeSELinuxIndication{Indication: indication}
		}
	}
	return NodeSELinuxIndication{}
}

func nodeSELinuxIndication(node *corev1.Node) string {
	containerRuntimeVersion := strings.ToLower(node.Status.NodeInfo.ContainerRuntimeVersion)
	kubeletVersion := strings.ToLower(node.Status.NodeInfo.KubeletVersion)
	var distribution string
	switch {
	case strings.HasPrefix(containerRuntimeVersion, "cri-o://"):
		return fmt.Sprintf("the node %s runs the container runtime CRI-O (%s)",
			node.Name, node.Status.NodeInfo.ContainerRuntimeVersion)
	case strings.Contains(kubeletVersion, "+rke2"):
		distribution = "RKE2"
	case strings.Contains(kubeletVersion, "+k3s"):
		distribution = "K3s"
	case strings.Contains(kubeletVersion, "+k0s"):
		distribution = "k0s"
	default:
		return ""
	}
	return fmt.Sprintf("the node %s runs %s (kubelet version %s)",
		node.Name, distribution, node.Status.NodeInfo.KubeletVersion)
}
