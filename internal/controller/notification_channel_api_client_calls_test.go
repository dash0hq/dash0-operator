// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"net/url"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The notification channel API client calls", func() {
	DescribeTable("address the same notification channel in Dash0 as the hand-built URL",
		func(namespace string, name string) {
			ctx := context.Background()
			logger := logd.FromContext(ctx)
			recorder := &urlRecordingRoundTripper{}
			notificationChannelReconciler := &NotificationChannelReconciler{
				pseudoClusterUid: "cluster-uid",
				apiClientPool: NewApiClientPool(
					dash0apiclient.NewTransport(
						dash0apiclient.WithBaseTransport(recorder),
						dash0apiclient.WithTransportMaxRetries(0),
					),
					"test",
				),
			}
			preconditionChecksResult := &preconditionValidationResult{k8sNamespace: namespace, k8sName: name}
			apiConfig := ApiConfig{
				Endpoint: ApiEndpointStandardizedTest,
				Dataset:  DatasetCustomTest,
				Token:    AuthorizationTokenTest,
			}
			handBuiltUrl := fmt.Sprintf(
				"%sapi/notification-channels/dash0-operator_cluster-uid_%s_%s",
				apiConfig.Endpoint,
				namespace,
				name,
			)
			expected, err := url.Parse(handBuiltUrl)
			Expect(err).ToNot(HaveOccurred())

			for _, action := range []apiAction{upsertAction, deleteAction} {
				preconditionChecksResult.resource = map[string]any{
					"metadata": map[string]any{"name": name},
					"spec": map[string]any{
						"display":     map[string]any{"name": "Notification Channel"},
						"type":        "slack",
						"slackConfig": map[string]any{"webhookURL": "https://hooks.slack.com/x", "channel": "#alerts"},
					},
				}
				result := notificationChannelReconciler.MapResourceToHttpRequests(
					preconditionChecksResult,
					apiConfig,
					action,
					logger,
				)
				Expect(result.ApiRequests).To(HaveLen(1))
				_, _ = result.ApiRequests[0].ApiClientCall.Execute(ctx)
			}

			Expect(recorder.urls).To(HaveLen(2))
			for _, actual := range recorder.urls {
				Expect(actual.Host).To(Equal(expected.Host))
				Expect(actual.Path).To(Equal(expected.Path))
				Expect(actual.RawQuery).To(BeEmpty())
			}
		},
		Entry("plain namespace and name", "namespace", "name"),
		Entry("namespace and name with dots", "my.namespace", "my.notification.channel"),
	)
})
