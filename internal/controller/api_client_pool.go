// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"container/list"
	"fmt"
	"strings"
	"sync"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
)

// maxPooledApiClients is the number of clients the pool keeps before it evicts the least recently used one.
const maxPooledApiClients = 50

type apiClientKey struct {
	endpoint string
	token    string
}

type pooledApiClient struct {
	key       apiClientKey
	apiClient dash0apiclient.Client
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
// retries, rate limiting and the concurrency limit apply globally across all API endpoints and tokens. The pool holds at
// most maxPooledApiClients clients and evicts the least recently used one when it is full, so clients (and tokens) for
// endpoint/token combinations that are no longer in use do not pile up.
type ApiClientPool struct {
	transport *dash0apiclient.Transport
	userAgent string
	mutex     sync.Mutex
	clients   map[apiClientKey]*list.Element
	lru       *list.List
}

// NewApiClientPool creates a pool whose clients all use the given shared transport and user agent.
func NewApiClientPool(transport *dash0apiclient.Transport, userAgent string) *ApiClientPool {
	return &ApiClientPool{
		transport: transport,
		userAgent: userAgent,
		clients:   make(map[apiClientKey]*list.Element),
		lru:       list.New(),
	}
}

// Get returns the client for the given endpoint and token, creating it on first use. It returns an error if the
// client cannot be created, for example because the endpoint is not a valid URL or the token is malformed.
func (p *ApiClientPool) Get(endpoint string, token string) (dash0apiclient.Client, error) {
	key := apiClientKey{endpoint: endpoint, token: token}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if element, ok := p.clients[key]; ok {
		p.lru.MoveToFront(element)
		return element.Value.(*pooledApiClient).apiClient, nil
	}
	if !strings.HasPrefix(token, dash0apiclient.AuthTokenPrefixStatic) && !strings.HasPrefix(token, dash0apiclient.AuthTokenPrefixOAuth) {
		return nil, &invalidAuthTokenError{
			err: fmt.Errorf(
				"the Dash0 auth token is invalid, it must start with %q or %q",
				dash0apiclient.AuthTokenPrefixStatic,
				dash0apiclient.AuthTokenPrefixOAuth,
			),
		}
	}
	apiClient, err := dash0apiclient.NewClient(
		dash0apiclient.WithApiUrl(endpoint),
		dash0apiclient.WithAuthToken(token),
		dash0apiclient.WithTransport(p.transport),
		dash0apiclient.WithUserAgent(p.userAgent),
	)
	if err != nil {
		return nil, err
	}
	if p.lru.Len() >= maxPooledApiClients {
		oldest := p.lru.Back()
		p.lru.Remove(oldest)
		delete(p.clients, oldest.Value.(*pooledApiClient).key)
	}
	p.clients[key] = p.lru.PushFront(&pooledApiClient{key: key, apiClient: apiClient})
	return apiClient, nil
}
