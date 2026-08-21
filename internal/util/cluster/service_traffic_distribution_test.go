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
		version         KubernetesVersion
		versionDetected bool
		expectOmitted   bool
	}

	DescribeTable("should resolve the service traffic distribution",
		func(testConfig resolveTrafficDistributionTest) {
			result := ResolveServiceTrafficDistribution(
				KubernetesVersionInfo{Version: testConfig.version, Detected: testConfig.versionDetected},
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
			version:         KubernetesVersion{Major: 1, Minor: 29},
			versionDetected: true,
			expectOmitted:   true,
		}),
		// 1.30 has the field, but its ServiceTrafficDistribution feature gate defaults to false there, so the API
		// server would prune the value and the service would never converge.
		Entry("omits the field on Kubernetes 1.30", resolveTrafficDistributionTest{
			version:         KubernetesVersion{Major: 1, Minor: 30},
			versionDetected: true,
			expectOmitted:   true,
		}),
		Entry("sets the field on Kubernetes 1.31, where the feature gate defaults to true", resolveTrafficDistributionTest{
			version:         KubernetesVersion{Major: 1, Minor: 31},
			versionDetected: true,
		}),
		Entry("sets the field on Kubernetes 1.33", resolveTrafficDistributionTest{
			version:         KubernetesVersion{Major: 1, Minor: 33},
			versionDetected: true,
		}),
		Entry("sets PreferClose (not PreferSameZone) on Kubernetes 1.34", resolveTrafficDistributionTest{
			version:         KubernetesVersion{Major: 1, Minor: 34},
			versionDetected: true,
		}),
		Entry("sets the field on a hypothetical Kubernetes 2.0", resolveTrafficDistributionTest{
			version:         KubernetesVersion{Major: 2, Minor: 0},
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
