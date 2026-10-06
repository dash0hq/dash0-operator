// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

func testApiClientPool() *ApiClientPool {
	return NewApiClientPool(TestTransport(), "dash0-operator-test")
}

var _ = Describe("The API client pool", func() {
	It("returns the same client for the same endpoint and token", func() {
		pool := testApiClientPool()
		first, err := pool.Get(ApiEndpointStandardizedTest, AuthorizationTokenTest)
		Expect(err).ToNot(HaveOccurred())
		second, err := pool.Get(ApiEndpointStandardizedTest, AuthorizationTokenTest)
		Expect(err).ToNot(HaveOccurred())
		Expect(second).To(BeIdenticalTo(first))
	})

	It("returns different clients for different endpoints or tokens", func() {
		pool := testApiClientPool()
		base, err := pool.Get(ApiEndpointStandardizedTest, AuthorizationTokenTest)
		Expect(err).ToNot(HaveOccurred())
		otherEndpoint, err := pool.Get(ApiEndpointStandardizedTestAlternative, AuthorizationTokenTest)
		Expect(err).ToNot(HaveOccurred())
		otherToken, err := pool.Get(ApiEndpointStandardizedTest, AuthorizationTokenTestAlternative)
		Expect(err).ToNot(HaveOccurred())
		Expect(otherEndpoint).ToNot(BeIdenticalTo(base))
		Expect(otherToken).ToNot(BeIdenticalTo(base))
	})

	It("returns an error for an invalid token and does not cache it", func() {
		pool := testApiClientPool()
		_, err := pool.Get(ApiEndpointStandardizedTest, "not-a-dash0-token")
		Expect(err).To(HaveOccurred())
		Expect(pool.clients).To(BeEmpty())
	})
})
