// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"net/http"
)

const (
	UserAgentHeaderName = "User-Agent"

	userAgentProduct        = "Dash0 Operator"
	userAgentUnknownVersion = "unknown"
)

// RenderUserAgent returns the value the operator uses for the User-Agent header
// when calling the Dash0 API. If version is empty (for example when the operator
// image carries neither a tag nor a digest) the version segment is reported as
// "unknown" so that the header is always well-formed.
func RenderUserAgent(version string) string {
	if version == "" {
		version = userAgentUnknownVersion
	}
	return userAgentProduct + "/" + version
}

type userAgentRoundTripper struct {
	base      http.RoundTripper
	userAgent string
}

func (rt *userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get(UserAgentHeaderName) != "" {
		return rt.base.RoundTrip(req)
	}
	clone := req.Clone(req.Context())
	clone.Header.Set(UserAgentHeaderName, rt.userAgent)
	return rt.base.RoundTrip(clone)
}

// WithUserAgent returns a copy of the given http.Client whose transport adds
// "User-Agent: Dash0 Operator/<version>" to every outgoing request that does
// not already carry a User-Agent header. Timeout, redirect policy and cookie
// jar are preserved.
func WithUserAgent(client *http.Client, version string) *http.Client {
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Transport: &userAgentRoundTripper{
			base:      base,
			userAgent: RenderUserAgent(version),
		},
		CheckRedirect: client.CheckRedirect,
		Jar:           client.Jar,
		Timeout:       client.Timeout,
	}
}
