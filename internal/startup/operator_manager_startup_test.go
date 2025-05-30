// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("operator manager startup", Ordered, func() {

	type originalValues struct {
		envVarsOTelCollectorNamePrefix string
		envVarsOperatorNamespace       string
	}

	var (
		origVals originalValues
	)

	BeforeAll(func() {
		origVals = originalValues{
			envVarsOTelCollectorNamePrefix: envVars.oTelCollectorNamePrefix,
			envVarsOperatorNamespace:       envVars.operatorNamespace,
		}
		envVars.oTelCollectorNamePrefix = OTelCollectorNamePrefixTest
		envVars.operatorNamespace = OperatorNamespace
	})

	AfterAll(func() {
		envVars.oTelCollectorNamePrefix = origVals.envVarsOTelCollectorNamePrefix
		envVars.operatorNamespace = origVals.envVarsOperatorNamespace
	})

	DescribeTable("should determine the collector base URL", func(forceOTelCollectorServiceUrl bool, isIPv6Cluster bool, expected string) {
		actual := determineCollectorBaseUrl(forceOTelCollectorServiceUrl, isIPv6Cluster)
		Expect(actual).To(Equal(expected))
	},
		Entry("when forceOTelCollectorServiceUrl is false and cluster is IPv4", false, false, OTelCollectorNodeLocalBaseUrlTest),
		Entry("when forceOTelCollectorServiceUrl is false and cluster is IPv6", false, true, OTelCollectorServiceBaseUrlTest),
		Entry("when forceOTelCollectorServiceUrl is true and cluster is IPv4", true, false, OTelCollectorServiceBaseUrlTest),
		Entry("when forceOTelCollectorServiceUrl is true and cluster is IPv6", true, true, OTelCollectorServiceBaseUrlTest),
	)
})
