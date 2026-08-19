// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResolveInstrumentationDelivery", func() {
	type resolveDeliveryTest struct {
		delivery                      dash0v1alpha1.InstrumentationDelivery
		apiServerVersion              KubernetesVersion
		apiServerVersionDetected      bool
		minimumKubeletVersion         KubernetesVersion
		minimumKubeletVersionDetected bool
		expectedDelivery              ResolvedInstrumentationDelivery
	}

	DescribeTable("should resolve the instrumentation delivery mechanism",
		func(testConfig resolveDeliveryTest) {
			result := ResolveInstrumentationDelivery(
				testConfig.delivery,
				KubernetesVersionInfo{
					Version:  testConfig.apiServerVersion,
					Detected: testConfig.apiServerVersionDetected,
				},
				KubernetesVersionInfo{
					Version:  testConfig.minimumKubeletVersion,
					Detected: testConfig.minimumKubeletVersionDetected,
				},
				true,
				logd.Discard(),
			)
			Expect(result).To(Equal(testConfig.expectedDelivery))
		},

		// image-volume: explicit request honored unless one of the two versions is known to be too old
		Entry("image-volume with undetected versions trusts the explicit choice", resolveDeliveryTest{
			delivery:         dash0v1alpha1.InstrumentationDeliveryImageVolume,
			expectedDelivery: ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("image-volume with an undetected minimum kubelet version trusts the explicit choice", resolveDeliveryTest{
			delivery:                 dash0v1alpha1.InstrumentationDeliveryImageVolume,
			apiServerVersion:         KubernetesVersion{Major: 1, Minor: 31},
			apiServerVersionDetected: true,
			expectedDelivery:         ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("image-volume on K8s 1.31 (lowest supported) resolves to image volume", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryImageVolume,
			apiServerVersion:              KubernetesVersion{Major: 1, Minor: 31},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 1, Minor: 31},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("image-volume on K8s 2.0 resolves to image volume", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryImageVolume,
			apiServerVersion:              KubernetesVersion{Major: 2, Minor: 0},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 2, Minor: 0},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("image-volume on K8s server 1.30 falls back to init container", resolveDeliveryTest{
			delivery:                 dash0v1alpha1.InstrumentationDeliveryImageVolume,
			apiServerVersion:         KubernetesVersion{Major: 1, Minor: 30},
			apiServerVersionDetected: true,
			expectedDelivery:         ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("image-volume with a node on K8s 1.30 falls back to init container", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryImageVolume,
			apiServerVersion:              KubernetesVersion{Major: 1, Minor: 36},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 1, Minor: 30},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryInitContainer,
		}),

		// auto: picks image volume only when the Kubernetes API server version and all kubelet versions are >= 1.36
		Entry("auto with undetected versions falls back to init container", resolveDeliveryTest{
			delivery:         dash0v1alpha1.InstrumentationDeliveryAuto,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("auto with a pending minimum kubelet version detection falls back to init container", resolveDeliveryTest{
			delivery:                 dash0v1alpha1.InstrumentationDeliveryAuto,
			apiServerVersion:         KubernetesVersion{Major: 1, Minor: 36},
			apiServerVersionDetected: true,
			expectedDelivery:         ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("auto on K8s 1.36 (lowest auto-enabled) resolves to image volume", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryAuto,
			apiServerVersion:              KubernetesVersion{Major: 1, Minor: 36},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 1, Minor: 36},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("auto on K8s 1.37 resolves to image volume", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryAuto,
			apiServerVersion:              KubernetesVersion{Major: 1, Minor: 37},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 1, Minor: 37},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("auto on K8s 2.0 resolves to image volume", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryAuto,
			apiServerVersion:              KubernetesVersion{Major: 2, Minor: 0},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 2, Minor: 0},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("auto on K8s server 1.35 resolves to init container", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryAuto,
			apiServerVersion:              KubernetesVersion{Major: 1, Minor: 35},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 1, Minor: 36},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("auto with a node on K8s 1.35 resolves to init container", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryAuto,
			apiServerVersion:              KubernetesVersion{Major: 1, Minor: 36},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 1, Minor: 35},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryInitContainer,
		}),

		// init-container: explicit request honored unconditionally
		Entry("init-container with undetected versions resolves to init container", resolveDeliveryTest{
			delivery:         dash0v1alpha1.InstrumentationDeliveryInitContainer,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("init-container on K8s 1.36 still resolves to init container", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryInitContainer,
			apiServerVersion:              KubernetesVersion{Major: 1, Minor: 36},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 1, Minor: 36},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("init-container on K8s 2.0 still resolves to init container", resolveDeliveryTest{
			delivery:                      dash0v1alpha1.InstrumentationDeliveryInitContainer,
			apiServerVersion:              KubernetesVersion{Major: 2, Minor: 0},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 2, Minor: 0},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryInitContainer,
		}),

		// unknown/invalid values fall back to init container
		Entry("unknown delivery value falls back to init container", resolveDeliveryTest{
			delivery:                      "nonsense",
			apiServerVersion:              KubernetesVersion{Major: 1, Minor: 36},
			apiServerVersionDetected:      true,
			minimumKubeletVersion:         KubernetesVersion{Major: 1, Minor: 36},
			minimumKubeletVersionDetected: true,
			expectedDelivery:              ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("empty delivery value falls back to init container", resolveDeliveryTest{
			delivery:         "",
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),
	)
})
