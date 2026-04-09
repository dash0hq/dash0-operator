// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"net/http"
	"time"

	"github.com/dash0hq/dash0-api-client-go"
)

// defaultTransportProxy is an http.RoundTripper that delegates to http.DefaultTransport at request time, rather than
// capturing the value at creation time. This is required for gock compatibility: gock replaces http.DefaultTransport
// with its own intercepting transport, and a proxy ensures the library's retry/rate-limit transport chain picks up
// gock's replacement regardless of creation order.
type defaultTransportProxy struct{}

func (defaultTransportProxy) RoundTrip(req *http.Request) (*http.Response, error) {
	return http.DefaultTransport.RoundTrip(req)
}

// TestHTTPClient returns an *http.Client with the dash0-api-client-go transport configured for fast retries, suitable
// for unit tests. Retry delays are set to milliseconds to avoid slowing down the test suite. The base transport is a
// proxy to http.DefaultTransport so that gock interception works.
func TestHTTPClient() *http.Client {
	return dash0.NewTransport(
		dash0.WithBaseTransport(defaultTransportProxy{}),
		dash0.WithTransportMaxRetries(2),
		dash0.WithTransportRetryWaitMin(1*time.Millisecond),
		dash0.WithTransportRetryWaitMax(5*time.Millisecond),
		dash0.WithTransportTimeout(10*time.Second),
	).HTTPClient()
}
