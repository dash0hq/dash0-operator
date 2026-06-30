// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package exporters

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("exporter headers", func() {

	Describe("HeaderSecretEnvVarName", func() {
		It("should build a name from protocol, name suffix and header index", func() {
			Expect(HeaderSecretEnvVarName("GRPC", "default_0", 0)).
				To(Equal("DASH0_HEADER_GRPC_DEFAULT_0_0"))
		})

		It("should support the HTTP protocol", func() {
			Expect(HeaderSecretEnvVarName("HTTP", "default_0", 2)).
				To(Equal("DASH0_HEADER_HTTP_DEFAULT_0_2"))
		})

		It("should sanitize namespaced name suffixes", func() {
			Expect(HeaderSecretEnvVarName("GRPC", "ns/some-namespace_0", 1)).
				To(Equal("DASH0_HEADER_GRPC_NS_SOME_NAMESPACE_0_1"))
		})

		It("should produce distinct names for different protocols", func() {
			Expect(HeaderSecretEnvVarName("GRPC", "default_0", 0)).
				NotTo(Equal(HeaderSecretEnvVarName("HTTP", "default_0", 0)))
		})

		It("should produce distinct names for different name suffixes", func() {
			Expect(HeaderSecretEnvVarName("GRPC", "default_0", 0)).
				NotTo(Equal(HeaderSecretEnvVarName("GRPC", "ns/some-namespace_0", 0)))
		})

		It("should produce distinct names for different header indices", func() {
			Expect(HeaderSecretEnvVarName("GRPC", "default_0", 0)).
				NotTo(Equal(HeaderSecretEnvVarName("GRPC", "default_0", 1)))
		})
	})

	Describe("sanitizeForEnvVarName", func() {
		It("should leave uppercase letters, digits and underscores untouched", func() {
			Expect(sanitizeForEnvVarName("ABC_123")).To(Equal("ABC_123"))
		})

		It("should uppercase lowercase letters", func() {
			Expect(sanitizeForEnvVarName("abcDef")).To(Equal("ABCDEF"))
		})

		It("should replace slashes with underscores", func() {
			Expect(sanitizeForEnvVarName("ns/some-namespace")).To(Equal("NS_SOME_NAMESPACE"))
		})

		It("should replace hyphens, dots and colons with underscores", func() {
			Expect(sanitizeForEnvVarName("a-b.c:d")).To(Equal("A_B_C_D"))
		})

		It("should replace non-ASCII characters with underscores", func() {
			Expect(sanitizeForEnvVarName("café")).To(Equal("CAF_"))
		})

		It("should return an empty string for an empty input", func() {
			Expect(sanitizeForEnvVarName("")).To(Equal(""))
		})
	})
})
