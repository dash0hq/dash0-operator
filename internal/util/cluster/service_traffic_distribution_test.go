// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResolveServiceTrafficDistribution", func() {
	type resolveTrafficDistributionTest struct {
		versionInfo     KubernetesVersionInfo
		versionDetected bool
		expectOmitted   bool
	}

	DescribeTable("should resolve the service traffic distribution",
		func(testConfig resolveTrafficDistributionTest) {
			result := ResolveServiceTrafficDistribution(
				testConfig.versionInfo,
				testConfig.versionDetected,
				logd.Discard(),
			)
			if testConfig.expectOmitted {
				Expect(result).To(BeNil())
				return
			}
			Expect(result).ToNot(BeNil())
			// Never PreferSameZone: that value additionally depends on the PreferSameTrafficDistribution feature gate,
			// which is beta and can be switched off by the cluster administrator.
			Expect(*result).To(Equal("PreferClose"))
		},

		Entry("omits the field on Kubernetes 1.29", resolveTrafficDistributionTest{
			versionInfo:     KubernetesVersionInfo{Major: 1, Minor: 29, VersionString: "1.29"},
			versionDetected: true,
			expectOmitted:   true,
		}),
		// 1.30 has the field, but its ServiceTrafficDistribution feature gate defaults to false there, so the API
		// server would prune the value and the service would never converge.
		Entry("omits the field on Kubernetes 1.30", resolveTrafficDistributionTest{
			versionInfo:     KubernetesVersionInfo{Major: 1, Minor: 30, VersionString: "1.30"},
			versionDetected: true,
			expectOmitted:   true,
		}),
		Entry("sets the field on Kubernetes 1.31, where the feature gate defaults to true", resolveTrafficDistributionTest{
			versionInfo:     KubernetesVersionInfo{Major: 1, Minor: 31, VersionString: "1.31"},
			versionDetected: true,
		}),
		Entry("sets the field on Kubernetes 1.33", resolveTrafficDistributionTest{
			versionInfo:     KubernetesVersionInfo{Major: 1, Minor: 33, VersionString: "1.33"},
			versionDetected: true,
		}),
		Entry("sets PreferClose (not PreferSameZone) on Kubernetes 1.34", resolveTrafficDistributionTest{
			versionInfo:     KubernetesVersionInfo{Major: 1, Minor: 34, VersionString: "1.34"},
			versionDetected: true,
		}),
		Entry("sets the field on a hypothetical Kubernetes 2.0", resolveTrafficDistributionTest{
			versionInfo:     KubernetesVersionInfo{Major: 2, Minor: 0, VersionString: "2.0"},
			versionDetected: true,
		}),
		// Setting a field the API server does not know would make the desired and the live object never converge,
		// so the operator would rewrite the service on every reconcile.
		Entry("omits the field when the version could not be detected", resolveTrafficDistributionTest{
			versionDetected: false,
			expectOmitted:   true,
		}),
	)
})
