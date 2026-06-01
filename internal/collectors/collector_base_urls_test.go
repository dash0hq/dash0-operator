// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("collector base URLs", func() {

	Describe("RenderCollectorBaseUrls", func() {
		It("should render both possible collector base URLs", func() {
			possibleUrls := RenderCollectorBaseUrls(OTelCollectorNamePrefixTest, OperatorNamespace)
			Expect(possibleUrls.ServiceBaseUrl).To(Equal(OTelCollectorServiceBaseUrlTest))
			Expect(possibleUrls.NodeLocalBaseUrl).To(Equal(OTelCollectorNodeLocalBaseUrlTest))
		})
	})

	Describe("SelectCollectorBaseUrl", func() {
		DescribeTable("should select the correct collector base URL",
			func(forceOTelCollectorServiceUrl bool, isIPv6Cluster bool, expected string) {
				actual := SelectCollectorBaseUrl(PossibleCollectorUrlsTest, forceOTelCollectorServiceUrl, isIPv6Cluster)
				Expect(actual).To(Equal(expected))
			},
			Entry("when forceOTelCollectorServiceUrl is false and cluster is IPv4", false, false, OTelCollectorNodeLocalBaseUrlTest),
			Entry("when forceOTelCollectorServiceUrl is false and cluster is IPv6", false, true, OTelCollectorServiceBaseUrlTest),
			Entry("when forceOTelCollectorServiceUrl is true and cluster is IPv4", true, false, OTelCollectorServiceBaseUrlTest),
			Entry("when forceOTelCollectorServiceUrl is true and cluster is IPv6", true, true, OTelCollectorServiceBaseUrlTest),
		)
	})
})
