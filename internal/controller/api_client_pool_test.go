// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"errors"
	"fmt"
	"net/http"

	dash0 "github.com/dash0hq/dash0-api-client-go"
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

var _ = Describe("Converting API client errors", func() {
	const actionLabel = "synchronize the view \"v\": PUT https://api.dash0.com/api/views/o?dataset=d"

	It("treats 404 for a delete as success", func() {
		err := convertApiClientError(&dash0.APIError{StatusCode: http.StatusNotFound}, actionLabel, true)
		Expect(err).ToNot(HaveOccurred())
	})

	DescribeTable("keeps the status code and renders the operator's error message",
		func(statusCode int, isDelete bool) {
			err := convertApiClientError(
				&dash0.APIError{StatusCode: statusCode, Body: `{"error":"x"}`},
				actionLabel,
				isDelete,
			)
			Expect(err).To(HaveOccurred())
			Expect(httpStatusCodeFromError(err)).To(Equal(statusCode))
			Expect(err.Error()).To(Equal(fmt.Sprintf(
				`unexpected status code %d when trying to %s, response body is {"error":"x"}`,
				statusCode,
				actionLabel,
			)))
		},
		Entry("404 for a put", http.StatusNotFound, false),
		Entry("400", http.StatusBadRequest, false),
		Entry("429", http.StatusTooManyRequests, false),
		Entry("503", http.StatusServiceUnavailable, false),
		Entry("500 for a delete", http.StatusInternalServerError, true),
	)

	It("maps errors without an HTTP response to status code 0", func() {
		transportErr := errors.New("connection refused")
		err := convertApiClientError(transportErr, actionLabel, false)
		Expect(err).To(HaveOccurred())
		Expect(httpStatusCodeFromError(err)).To(Equal(0))
		Expect(errors.Is(err, transportErr)).To(BeTrue())
		Expect(isRetryableHttpStatusCode(httpStatusCodeFromError(err))).To(BeTrue())
	})
})
