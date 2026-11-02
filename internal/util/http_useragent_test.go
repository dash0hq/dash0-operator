// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("user agent", func() {
	Describe("RenderUserAgent", func() {
		It("renders the Dash0Operator product with the given version", func() {
			Expect(RenderUserAgent("1.2.3")).To(Equal("Dash0Operator/1.2.3"))
		})

		It("falls back to /unknown when version is empty", func() {
			Expect(RenderUserAgent("")).To(Equal("Dash0Operator/unknown"))
		})

		It("replaces colons in digest-pinned versions so the token is RFC 7230 compliant", func() {
			Expect(RenderUserAgent("sha256:abc123")).To(Equal("Dash0Operator/sha256-abc123"))
		})
	})
})
