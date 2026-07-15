// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RedactSensitiveEnvVarsInPatch", func() {

	It("redacts the values of sensitive env vars while preserving non-sensitive ones", func() {
		patch := []byte(`{
			"spec": {"template": {"spec": {"containers": [{
				"name": "opentelemetry-collector",
				"env": [
					{"name": "OTELCOL_AUTH_TOKEN_DEFAULT_0", "value": "dummy-token"},
					{"name": "SELF_MONITORING_AUTH_TOKEN", "value": "dummy-token"},
					{"name": "DASH0_AGENT0_CONNECTOR_AUTH_TOKEN", "value": "dummy-token"},
					{"name": "EDGE_PROXY_AUTH_TOKEN", "value": "dummy-token"},
					{"name": "DASH0_HEADER_GRPC_DEFAULT_0", "value": "dummy-token"},
					{"name": "OTEL_SERVICE_NAME", "value": "dash0-operator"}
				]
			}]}}}
		}`)

		redacted := RedactSensitiveEnvVarsInPatch(patch)

		Expect(redacted).ToNot(ContainSubstring("dummy-token"))
		Expect(redacted).To(ContainSubstring("<redacted>"))
		// non-sensitive values must remain visible for debugging
		Expect(redacted).To(ContainSubstring("dash0-operator"))
		Expect(redacted).To(ContainSubstring("OTELCOL_AUTH_TOKEN_DEFAULT_0"))
	})

	It("redacts a sensitive env var value regardless of JSON key order", func() {
		patch := []byte(`{"env": [{"value": "dummy-token", "name": "OTELCOL_AUTH_TOKEN_DEFAULT_0"}]}`)

		redacted := RedactSensitiveEnvVarsInPatch(patch)

		Expect(redacted).ToNot(ContainSubstring("dummy-token"))
		Expect(redacted).To(ContainSubstring("<redacted>"))
	})

	It("leaves env vars sourced from a secret (no literal value) untouched", func() {
		patch := []byte(`{"env": [{"name": "OTELCOL_AUTH_TOKEN_DEFAULT_0", "valueFrom": {"secretKeyRef": {"name": "s", "key": "k"}}}]}`)

		redacted := RedactSensitiveEnvVarsInPatch(patch)

		Expect(redacted).To(ContainSubstring("secretKeyRef"))
		Expect(redacted).ToNot(ContainSubstring("<redacted>"))
	})

	It("omits the patch entirely when it cannot be parsed as JSON, rather than risk leaking", func() {
		redacted := RedactSensitiveEnvVarsInPatch([]byte("dummy-token not json"))

		Expect(redacted).ToNot(ContainSubstring("dummy-token"))
		Expect(redacted).To(ContainSubstring("patch omitted"))
	})
})
