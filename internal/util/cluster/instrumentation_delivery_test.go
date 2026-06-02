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
		delivery         string
		versionInfo      KubernetesVersionInfo
		versionDetected  bool
		expectedDelivery ResolvedInstrumentationDelivery
	}

	DescribeTable("should resolve the instrumentation delivery mechanism",
		func(testConfig resolveDeliveryTest) {
			result := ResolveInstrumentationDelivery(
				testConfig.delivery,
				testConfig.versionInfo,
				testConfig.versionDetected,
				true,
				logd.Discard(),
			)
			Expect(result).To(Equal(testConfig.expectedDelivery))
		},

		// image-volume: explicit request honored whenever the K8s version permits it
		Entry("image-volume with undetected version trusts the explicit choice", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryImageVolume),
			versionDetected:  false,
			expectedDelivery: ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("image-volume on K8s 1.31 (lowest supported) resolves to image volume", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryImageVolume),
			versionInfo:      KubernetesVersionInfo{Major: 1, Minor: 31, VersionString: "1.31"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("image-volume on K8s 1.36 resolves to image volume", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryImageVolume),
			versionInfo:      KubernetesVersionInfo{Major: 1, Minor: 36, VersionString: "1.36"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("image-volume on K8s 2.0 resolves to image volume", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryImageVolume),
			versionInfo:      KubernetesVersionInfo{Major: 2, Minor: 0, VersionString: "2.0"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("image-volume on K8s 1.30 falls back to init container", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryImageVolume),
			versionInfo:      KubernetesVersionInfo{Major: 1, Minor: 30, VersionString: "1.30"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),

		// auto: picks image volume only when the version is >= 1.36
		Entry("auto with undetected version falls back to init container", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryAuto),
			versionDetected:  false,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("auto on K8s 1.36 (lowest auto-enabled) resolves to image volume", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryAuto),
			versionInfo:      KubernetesVersionInfo{Major: 1, Minor: 36, VersionString: "1.36"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("auto on K8s 1.37 resolves to image volume", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryAuto),
			versionInfo:      KubernetesVersionInfo{Major: 1, Minor: 37, VersionString: "1.37"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("auto on K8s 2.0 resolves to image volume", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryAuto),
			versionInfo:      KubernetesVersionInfo{Major: 2, Minor: 0, VersionString: "2.0"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryImageVolume,
		}),
		Entry("auto on K8s 1.35 resolves to init container", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryAuto),
			versionInfo:      KubernetesVersionInfo{Major: 1, Minor: 35, VersionString: "1.35"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),

		// init-container: explicit request honored unconditionally
		Entry("init-container with undetected version resolves to init container", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryInitContainer),
			versionDetected:  false,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("init-container on K8s 1.36 still resolves to init container", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryInitContainer),
			versionInfo:      KubernetesVersionInfo{Major: 1, Minor: 36, VersionString: "1.36"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("init-container on K8s 2.0 still resolves to init container", resolveDeliveryTest{
			delivery:         string(dash0v1alpha1.InstrumentationDeliveryInitContainer),
			versionInfo:      KubernetesVersionInfo{Major: 2, Minor: 0, VersionString: "2.0"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),

		// unknown/invalid values fall back to init container
		Entry("unknown delivery value falls back to init container", resolveDeliveryTest{
			delivery:         "nonsense",
			versionInfo:      KubernetesVersionInfo{Major: 1, Minor: 36, VersionString: "1.36"},
			versionDetected:  true,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),
		Entry("empty delivery value falls back to init container", resolveDeliveryTest{
			delivery:         "",
			versionDetected:  false,
			expectedDelivery: ResolvedInstrumentationDeliveryInitContainer,
		}),
	)
})
