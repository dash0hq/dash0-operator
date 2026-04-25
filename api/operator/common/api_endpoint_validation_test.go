// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ValidateDash0ApiEndpoint", func() {

	DescribeTable("accepts allowed endpoints",
		func(endpoint string) {
			Expect(ValidateDash0ApiEndpoint(endpoint)).To(Succeed())
		},
		Entry("api.dash0.com (no subdomain segment)", "https://api.dash0.com"),
		Entry("api.<region>.dash0.com", "https://api.eu1.dash0.com"),
		Entry("api.<region-with-hyphen>.dash0.com", "https://api.us-east-1.dash0.com"),
		Entry("api.<multi.segment-with-hyphens>.dash0.com", "https://api.eu-west1.aws.dash0.com"),
		Entry("api.dash0-dev.com", "https://api.dash0-dev.com"),
		Entry("api.<region>.dash0-dev.com", "https://api.eu1.aws.dash0-dev.com"),
		Entry("http scheme", "http://api.dash0.com"),
		Entry("explicit standard https port", "https://api.dash0.com:443"),
		Entry("non-standard port", "https://api.dash0.com:8443"),
		Entry("trailing slash", "https://api.dash0.com/"),
		Entry("with path", "https://api.dash0.com/api/v1"),
		Entry("uppercase host (case-insensitive)", "https://API.Dash0.COM"),
		Entry("in-cluster service", "http://my-service.my-namespace.svc.cluster.local"),
		Entry("in-cluster service with port", "http://my-service.my-namespace.svc.cluster.local:8080"),
		Entry("in-cluster service with deep namespace", "http://api.foo.bar.svc.cluster.local"),
	)

	DescribeTable("rejects disallowed endpoints",
		func(endpoint string) {
			Expect(ValidateDash0ApiEndpoint(endpoint)).NotTo(Succeed())
		},
		Entry("empty", ""),
		Entry("not a URL", "::not a url"),
		Entry("ftp scheme", "ftp://api.dash0.com"),
		Entry("file scheme", "file:///etc/passwd"),
		Entry("missing scheme", "api.dash0.com"),
		Entry("foreign host", "https://api.example.com"),
		Entry("dash0 not in TLD position", "https://api.dash0.com.evil.com"),
		Entry("userinfo SSRF trick", "https://api.dash0.com@evil.com/"),
		Entry("query string", "https://api.dash0.com/?x=1"),
		Entry("fragment", "https://api.dash0.com/#frag"),
		Entry("svc.cluster.local without subdomain", "http://svc.cluster.local"),
		Entry("foreign cluster suffix", "http://my-service.my-namespace.svc.example.com"),
	)
})
