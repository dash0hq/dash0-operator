// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"strings"
	"sync"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

type apiClientKey struct {
	endpoint string
	token    string
}

// invalidAuthTokenError is returned for a token that the Dash0 API client refuses to use. The Dash0 API would reject
// such a token anyway, so it is treated like an HTTP 401 response.
type invalidAuthTokenError struct {
	err error
}

func (e *invalidAuthTokenError) Error() string {
	return e.err.Error()
}

func (e *invalidAuthTokenError) Unwrap() error {
	return e.err
}

// ApiClientPool hands out Dash0 API clients per (endpoint, token) combination. All clients share one transport, so
// retries, rate limiting and the concurrency limit apply globally across all API endpoints and tokens.
type ApiClientPool struct {
	transport *dash0.Transport
	userAgent string
	mutex     sync.Mutex
	clients   map[apiClientKey]dash0.Client
}

// NewApiClientPool creates a pool whose clients all use the given shared transport and user agent.
func NewApiClientPool(transport *dash0.Transport, userAgent string) *ApiClientPool {
	return &ApiClientPool{
		transport: transport,
		userAgent: userAgent,
		clients:   make(map[apiClientKey]dash0.Client),
	}
}

// Get returns the client for the given endpoint and token, creating it on first use. It returns an error if the
// client cannot be created, for example because the endpoint is not a valid URL or the token is malformed.
func (p *ApiClientPool) Get(endpoint string, token string) (dash0.Client, error) {
	key := apiClientKey{endpoint: endpoint, token: token}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if apiClient, ok := p.clients[key]; ok {
		return apiClient, nil
	}
	if !strings.HasPrefix(token, dash0.AuthTokenPrefixStatic) && !strings.HasPrefix(token, dash0.AuthTokenPrefixOAuth) {
		return nil, &invalidAuthTokenError{
			err: fmt.Errorf(
				"the Dash0 auth token is invalid, it must start with %q or %q",
				dash0.AuthTokenPrefixStatic,
				dash0.AuthTokenPrefixOAuth,
			),
		}
	}
	apiClient, err := dash0.NewClient(
		dash0.WithApiUrl(endpoint),
		dash0.WithAuthToken(token),
		dash0.WithTransport(p.transport),
		dash0.WithUserAgent(p.userAgent),
	)
	if err != nil {
		return nil, err
	}
	p.clients[key] = apiClient
	return apiClient, nil
}
