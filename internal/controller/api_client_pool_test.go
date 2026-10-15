// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	. "github.com/dash0hq/dash0-operator/test/util"
)

func testApiClientPool() *ApiClientPool {
	return NewApiClientPool(TestTransport(), "dash0-operator-test")
}

type urlRecordingRoundTripper struct {
	urls []*url.URL
}

func (r *urlRecordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL)
	return nil, errors.New("request recorded")
}

func urlRecordingApiClientPool() (*ApiClientPool, *urlRecordingRoundTripper) {
	recorder := &urlRecordingRoundTripper{}
	return NewApiClientPool(
		dash0apiclient.NewTransport(dash0apiclient.WithBaseTransport(recorder), dash0apiclient.WithTransportMaxRetries(0)),
		"test",
	), recorder
}

/*
expectApiClientCallsToAddressUrl maps the resource to an upsert and a delete API client call, executes both against
the given recorder (which must back the reconciler's API client pool), and expects that both requests address the
same host, path and query as the hand-built URL.
*/
func expectApiClientCallsToAddressUrl(
	reconciler ApiSyncReconciler,
	recorder *urlRecordingRoundTripper,
	preconditionChecksResult *preconditionValidationResult,
	resource func() map[string]any,
	apiConfig ApiConfig,
	handBuiltUrl string,
) {
	GinkgoHelper()
	expected, err := url.Parse(handBuiltUrl)
	Expect(err).ToNot(HaveOccurred())
	ctx := context.Background()
	logger := logd.FromContext(ctx)
	for _, action := range []apiAction{upsertAction, deleteAction} {
		preconditionChecksResult.resource = resource()
		result := reconciler.MapResourceToHttpRequests(preconditionChecksResult, apiConfig, action, logger)
		Expect(result.ApiRequests).To(HaveLen(1))
		_, _ = result.ApiRequests[0].ApiClientCall.Execute(ctx)
	}
	Expect(recorder.urls).To(HaveLen(2))
	for _, actual := range recorder.urls {
		Expect(actual.Host).To(Equal(expected.Host))
		Expect(actual.Path).To(Equal(expected.Path))
		Expect(actual.Query()).To(Equal(expected.Query()))
	}
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
