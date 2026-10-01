// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	. "github.com/dash0hq/dash0-operator/test/util"
)

type urlRecordingRoundTripper struct {
	urls []*url.URL
}

func (r *urlRecordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL)
	return nil, errors.New("request recorded")
}

var _ = Describe("The view API client calls", func() {
	DescribeTable("address the same view in Dash0 as the hand-built URL",
		func(dataset string) {
			ctx := context.Background()
			logger := logd.FromContext(ctx)
			recorder := &urlRecordingRoundTripper{}
			viewReconciler := &ViewReconciler{
				pseudoClusterUid: "cluster-uid",
				apiClientPool: NewApiClientPool(
					dash0.NewTransport(dash0.WithBaseTransport(recorder), dash0.WithTransportMaxRetries(0)),
					"test",
				),
			}
			preconditionChecksResult := &preconditionValidationResult{k8sNamespace: "namespace", k8sName: "name"}
			apiConfig := ApiConfig{Endpoint: ApiEndpointStandardizedTest, Dataset: dataset, Token: AuthorizationTokenTest}
			handBuiltUrl, _ := viewReconciler.renderViewUrl(preconditionChecksResult, apiConfig.Endpoint, dataset)
			expected, err := url.Parse(handBuiltUrl)
			Expect(err).ToNot(HaveOccurred())

			for _, action := range []apiAction{upsertAction, deleteAction} {
				preconditionChecksResult.resource = map[string]any{"metadata": map[string]any{"name": "name"}}
				result := viewReconciler.MapResourceToHttpRequests(preconditionChecksResult, apiConfig, action, logger)
				Expect(result.ApiRequests).To(HaveLen(1))
				_, _ = result.ApiRequests[0].ApiClientCall.Execute(ctx)
			}

			Expect(recorder.urls).To(HaveLen(2))
			for _, actual := range recorder.urls {
				Expect(actual.Host).To(Equal(expected.Host))
				Expect(actual.Path).To(Equal(expected.Path))
				Expect(actual.Query()).To(Equal(expected.Query()))
			}
		},
		Entry("plain dataset", "default"),
		Entry("dataset with a space", "my dataset"),
		Entry("dataset with a slash", "a/b"),
		Entry("dataset with a percent sign", "100%"),
		Entry("dataset with non-ASCII and reserved characters", "ä+ö&x=y"),
	)
})
