// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The node SELinux indication", func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	node := func(name string, containerRuntimeVersion string, kubeletVersion string) runtime.Object {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					ContainerRuntimeVersion: containerRuntimeVersion,
					KubeletVersion:          kubeletVersion,
				},
			},
		}
	}

	DescribeTable("should detect nodes whose container runtime likely confines containers with SELinux",
		func(containerRuntimeVersion string, kubeletVersion string, expectedIndication string) {
			indication := DetectNodeSELinuxIndication(
				ctx,
				fake.NewClientset(
					node("node-1", "containerd://2.1.4", "v1.34.1"),
					node("node-2", containerRuntimeVersion, kubeletVersion),
				),
				logger,
			)
			Expect(indication.InspectionFailed).To(BeFalse())
			Expect(indication.Indication).To(Equal(expectedIndication))
		},
		Entry("CRI-O", "cri-o://1.33.4", "v1.33.4",
			"the node node-2 runs the container runtime CRI-O (cri-o://1.33.4)"),
		Entry("RKE2", "containerd://2.0.5-k3s1", "v1.33.4+rke2r1",
			"the node node-2 runs RKE2 (kubelet version v1.33.4+rke2r1)"),
		Entry("K3s", "containerd://2.0.5-k3s1", "v1.33.4+k3s1",
			"the node node-2 runs K3s (kubelet version v1.33.4+k3s1)"),
		Entry("k0s", "containerd://1.7.27", "v1.33.4+k0s",
			"the node node-2 runs k0s (kubelet version v1.33.4+k0s)"),
		Entry("a plain node with containerd", "containerd://2.1.4", "v1.34.1", ""),
		Entry("a node with Docker", "docker://28.3.2", "v1.34.1", ""),
		Entry("a GKE node", "containerd://1.7.27", "v1.33.4-gke.1245000", ""),
		Entry("an EKS node", "containerd://1.7.27", "v1.33.4-eks-113cf36", ""),
	)

	It("should report no indication for a cluster without nodes", func() {
		indication := DetectNodeSELinuxIndication(ctx, fake.NewClientset(), logger)
		Expect(indication).To(Equal(NodeSELinuxIndication{}))
	})

	It("should inspect a single bounded page of nodes", func() {
		clientset := fake.NewClientset(node("node-1", "containerd://2.1.4", "v1.34.1"))
		var listOptions []metav1.ListOptions
		clientset.PrependReactor("list", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			listOptions = append(listOptions, action.(k8stesting.ListActionImpl).ListOptions)
			return false, nil, nil
		})

		DetectNodeSELinuxIndication(ctx, clientset, logger)

		Expect(listOptions).To(HaveLen(1))
		Expect(listOptions[0].Limit).To(Equal(int64(nodeSELinuxInspectionLimit)))
	})

	It("should report a failed inspection if the nodes cannot be listed", func() {
		clientset := fake.NewClientset()
		clientset.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("connection refused")
		})

		indication := DetectNodeSELinuxIndication(ctx, clientset, logger)

		Expect(indication).To(Equal(NodeSELinuxIndication{InspectionFailed: true}))
	})
})
