// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the cluster instrumentation config", func() {

	Describe("resolving the instrumentation delivery mechanism", func() {

		It("should resolve auto to init container while the minimum kubelet version is unknown", func() {
			config := newTestClusterInstrumentationConfig(
				dash0v1alpha1.InstrumentationDeliveryAuto,
				cluster.KubernetesVersion{Major: 1, Minor: 36},
				cluster.NewMinimumKubeletVersionDetector(),
			)
			Expect(config.ResolveInstrumentationDelivery()).
				To(Equal(cluster.ResolvedInstrumentationDeliveryInitContainer))
		})

		It("should resolve auto to image volume once all kubelets have been seen to be recent enough", func() {
			config := newTestClusterInstrumentationConfig(
				dash0v1alpha1.InstrumentationDeliveryAuto,
				cluster.KubernetesVersion{Major: 1, Minor: 36},
				startDetection("v1.36.0", "v1.37.1"),
			)
			Eventually(config.ResolveInstrumentationDelivery).
				Should(Equal(cluster.ResolvedInstrumentationDeliveryImageVolume))
		})

		It("should keep resolving auto to init container when one kubelet lags behind", func() {
			config := newTestClusterInstrumentationConfig(
				dash0v1alpha1.InstrumentationDeliveryAuto,
				cluster.KubernetesVersion{Major: 1, Minor: 36},
				startDetection("v1.36.0", "v1.35.4"),
			)
			Consistently(config.ResolveInstrumentationDelivery).
				Should(Equal(cluster.ResolvedInstrumentationDeliveryInitContainer))
		})

		It("should keep resolving auto to init container when the Kubernetes API server version is too old", func() {
			config := newTestClusterInstrumentationConfig(
				dash0v1alpha1.InstrumentationDeliveryAuto,
				cluster.KubernetesVersion{Major: 1, Minor: 35},
				startDetection("v1.36.0"),
			)
			Consistently(config.ResolveInstrumentationDelivery).
				Should(Equal(cluster.ResolvedInstrumentationDeliveryInitContainer))
		})

		It("should resolve an explicitly requested image volume delivery immediately", func() {
			config := newTestClusterInstrumentationConfig(
				dash0v1alpha1.InstrumentationDeliveryImageVolume,
				cluster.KubernetesVersion{Major: 1, Minor: 31},
				cluster.NewMinimumKubeletVersionDetector(),
			)
			Expect(config.ResolveInstrumentationDelivery()).
				To(Equal(cluster.ResolvedInstrumentationDeliveryImageVolume))
		})

		It("should report the previous and the current delivery mechanism when the setting changes", func() {
			config := newTestClusterInstrumentationConfig(
				dash0v1alpha1.InstrumentationDeliveryInitContainer,
				cluster.KubernetesVersion{Major: 1, Minor: 36},
				cluster.NewMinimumKubeletVersionDetector(),
			)
			previous, current := config.UpdateRequestedInstrumentationDelivery(
				dash0v1alpha1.InstrumentationDeliveryImageVolume,
				true,
				logd.Discard(),
			)
			Expect(previous).To(Equal(cluster.ResolvedInstrumentationDeliveryInitContainer))
			Expect(current).To(Equal(cluster.ResolvedInstrumentationDeliveryImageVolume))
			Expect(config.ResolveInstrumentationDelivery()).
				To(Equal(cluster.ResolvedInstrumentationDeliveryImageVolume))
		})
	})

	Describe("waiting for the instrumentation delivery mechanism to settle", func() {

		It("should wait for the minimum kubelet version detection when auto is requested", func() {
			config := newTestClusterInstrumentationConfig(
				dash0v1alpha1.InstrumentationDeliveryAuto,
				cluster.KubernetesVersion{Major: 1, Minor: 36},
				startDetection("v1.36.0", "v1.37.1"),
			)

			Expect(config.WaitForInstrumentationDeliveryAutoToBeResolved(context.Background(), time.Minute)).
				To(Equal(cluster.ResolvedInstrumentationDeliveryImageVolume))
		})

		DescribeTable("should not wait for the detection when the mechanism has been requested explicitly",
			func(
				requested dash0v1alpha1.InstrumentationDelivery,
				expected cluster.ResolvedInstrumentationDelivery,
			) {
				// The detection is never started, so it never settles. Passing a timeout far beyond the test's own
				// budget means the call can only return by skipping the wait altogether.
				config := newTestClusterInstrumentationConfig(
					requested,
					cluster.KubernetesVersion{Major: 1, Minor: 36},
					cluster.NewMinimumKubeletVersionDetector(),
				)

				Expect(config.WaitForInstrumentationDeliveryAutoToBeResolved(context.Background(), time.Hour)).
					To(Equal(expected))
			},
			Entry("init container",
				dash0v1alpha1.InstrumentationDeliveryInitContainer,
				cluster.ResolvedInstrumentationDeliveryInitContainer),
			Entry("image volume",
				dash0v1alpha1.InstrumentationDeliveryImageVolume,
				cluster.ResolvedInstrumentationDeliveryImageVolume),
		)
	})
})

func newTestClusterInstrumentationConfig(
	requestedInstrumentationDelivery dash0v1alpha1.InstrumentationDelivery,
	apiServerVersion cluster.KubernetesVersion,
	minimumKubeletVersionDetector *cluster.MinimumKubeletVersionDetector,
) *ClusterInstrumentationConfig {
	config := NewClusterInstrumentationConfig(
		Images{},
		PossibleCollectorUrls{},
		"",
		ExtraConfigDefaults,
		requestedInstrumentationDelivery,
		nil,
		false,
		false,
		false,
	)
	config.SetKubernetesVersions(
		cluster.KubernetesVersionInfo{Version: apiServerVersion, Detected: true},
		minimumKubeletVersionDetector,
	)
	return config
}

func startDetection(kubeletVersions ...string) *cluster.MinimumKubeletVersionDetector {
	nodes := make([]runtime.Object, 0, len(kubeletVersions))
	for i, kubeletVersion := range kubeletVersions {
		nodes = append(nodes, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("node-%d", i)},
			Status:     corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: kubeletVersion}},
		})
	}
	detector := cluster.NewMinimumKubeletVersionDetector()
	detector.StartDetection(context.Background(), fake.NewClientset(nodes...), nil, logd.Discard())
	return detector
}
