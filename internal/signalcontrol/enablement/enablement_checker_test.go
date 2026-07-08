// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package enablement

import (
	"context"
	"net/http"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	testApiEndpoint           = "https://api.example.dash0.com"
	testAuthToken             = "test-auth-token"
	signalControlSettingsPath = "/api/signal-control/edge/settings"
)

var _ = Describe("the Signal Control enablement checker", func() {
	var ctx context.Context
	var httpClient *http.Client
	var checker *EnablementChecker
	logger := logd.Discard()

	BeforeEach(func() {
		ctx = context.Background()
		// A client with a nil transport uses http.DefaultTransport at request time, which gock replaces when a mock is
		// registered via gock.New, so requests made through this client are intercepted.
		httpClient = &http.Client{}
		// The k8s client is only needed to resolve secret-ref-based auth tokens; these tests use a token literal.
		checker = NewEnablementChecker(httpClient, nil, "dash0-operator-namespace")
	})

	AfterEach(func() {
		gock.Off()
	})

	Describe("Check", func() {
		It("returns Allowed and caches it when the API reports enabled:true", func() {
			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				MatchHeader("Authorization", "Bearer "+testAuthToken).
				Reply(http.StatusOK).
				JSON(map[string]bool{"enabled": true})

			result, err := checker.Check(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(ResultAllowed))
			Expect(checker.Result()).To(Equal(ResultAllowed))
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("returns NotAllowed and caches it when the API reports enabled:false", func() {
			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				Reply(http.StatusOK).
				JSON(map[string]bool{"enabled": false})

			result, err := checker.Check(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(ResultNotAllowed))
			Expect(checker.Result()).To(Equal(ResultNotAllowed))
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("returns an error and leaves the cached result unchanged on a non-2xx response", func() {
			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				Reply(http.StatusServiceUnavailable)

			result, err := checker.Check(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)

			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(ResultUnknown))
			Expect(checker.Result()).To(Equal(ResultUnknown))
		})

		It("returns an error on an unparseable response body", func() {
			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				Reply(http.StatusOK).
				BodyString("this is not json")

			result, err := checker.Check(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)

			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(ResultUnknown))
		})

		It("keeps a previously confirmed Allowed result when a later check fails transiently", func() {
			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				Reply(http.StatusOK).
				JSON(map[string]bool{"enabled": true})
			_, err := checker.Check(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(checker.Result()).To(Equal(ResultAllowed))

			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				Reply(http.StatusServiceUnavailable)
			result, err := checker.Check(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)

			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(ResultAllowed))
			Expect(checker.Result()).To(Equal(ResultAllowed))
		})

		It("returns ErrNotConfigured when there is no operator configuration resource", func() {
			result, err := checker.Check(ctx, nil, logger)

			Expect(err).To(MatchError(ErrNotConfigured))
			Expect(result).To(Equal(ResultUnknown))
		})

		It("returns ErrNotConfigured when no Dash0 export has an API endpoint", func() {
			result, err := checker.Check(ctx, operatorConfigWithApiEndpoint(""), logger)

			Expect(err).To(MatchError(ErrNotConfigured))
			Expect(result).To(Equal(ResultUnknown))
		})
	})

	Describe("EnsureAllowed", func() {
		It("performs a check when the result is Unknown and returns the result", func() {
			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				Reply(http.StatusOK).
				JSON(map[string]bool{"enabled": true})

			Expect(checker.EnsureAllowed(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)).To(BeTrue())
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("returns the cached Allowed result without performing another HTTP request", func() {
			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				Reply(http.StatusOK).
				JSON(map[string]bool{"enabled": true})
			_, err := checker.Check(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)
			Expect(err).ToNot(HaveOccurred())

			// No further gock mock is registered; a second HTTP request would fail. EnsureAllowed must use the cache.
			Expect(checker.EnsureAllowed(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)).To(BeTrue())
		})

		It("returns false without an HTTP request when the cached result is NotAllowed", func() {
			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				Reply(http.StatusOK).
				JSON(map[string]bool{"enabled": false})
			_, err := checker.Check(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(checker.EnsureAllowed(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)).To(BeFalse())
		})

		It("returns false when the check cannot be completed", func() {
			gock.New(testApiEndpoint).
				Get(signalControlSettingsPath).
				Reply(http.StatusServiceUnavailable)

			Expect(checker.EnsureAllowed(ctx, operatorConfigWithApiEndpoint(testApiEndpoint), logger)).To(BeFalse())
			Expect(checker.Result()).To(Equal(ResultUnknown))
		})
	})
})

func operatorConfigWithApiEndpoint(apiEndpoint string) *dash0v1alpha1.Dash0OperatorConfiguration {
	return &dash0v1alpha1.Dash0OperatorConfiguration{
		Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
			Exports: []dash0common.Export{
				{
					Dash0: &dash0common.Dash0Configuration{
						Endpoint:    "ingress.example.dash0.com:4317",
						ApiEndpoint: apiEndpoint,
						Authorization: dash0common.Authorization{
							Token: ptr.To(testAuthToken),
						},
					},
				},
			},
		},
	}
}
