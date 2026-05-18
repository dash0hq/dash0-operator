// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"net/http"
	"net/http/httptest"

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

	Describe("WithUserAgent", func() {
		var (
			server      *httptest.Server
			lastRequest *http.Request
		)

		BeforeEach(func() {
			lastRequest = nil
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				lastRequest = r.Clone(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))
		})

		AfterEach(func() {
			server.Close()
		})

		It("adds the User-Agent header to outgoing requests", func() {
			client := WithUserAgent(&http.Client{}, "1.2.3")

			resp, err := client.Get(server.URL)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Body.Close()).To(Succeed())

			Expect(lastRequest).NotTo(BeNil())
			Expect(lastRequest.Header.Get("User-Agent")).To(Equal("Dash0Operator/1.2.3"))
		})

		It("uses the unknown fallback when no version is given", func() {
			client := WithUserAgent(&http.Client{}, "")

			resp, err := client.Get(server.URL)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Body.Close()).To(Succeed())

			Expect(lastRequest.Header.Get("User-Agent")).To(Equal("Dash0Operator/unknown"))
		})

		It("preserves a User-Agent header that the caller already set", func() {
			client := WithUserAgent(&http.Client{}, "1.2.3")

			req, err := http.NewRequest(http.MethodGet, server.URL, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("User-Agent", "explicit/9.9")

			resp, err := client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Body.Close()).To(Succeed())

			Expect(lastRequest.Header.Get("User-Agent")).To(Equal("explicit/9.9"))
		})

		It("does not mutate the caller's request header map", func() {
			client := WithUserAgent(&http.Client{}, "1.2.3")

			req, err := http.NewRequest(http.MethodGet, server.URL, nil)
			Expect(err).NotTo(HaveOccurred())

			resp, err := client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Body.Close()).To(Succeed())

			Expect(req.Header.Get("User-Agent")).To(BeEmpty())
		})

		It("preserves the original client timeout", func() {
			original := &http.Client{Timeout: 7}
			wrapped := WithUserAgent(original, "1.2.3")
			Expect(wrapped.Timeout).To(Equal(original.Timeout))
		})
	})
})
