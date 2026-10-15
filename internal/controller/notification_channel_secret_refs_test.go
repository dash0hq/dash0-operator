// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const credentialsSecretName = "notification-channel-credentials"

var _ = Describe("Resolving the secret references of a notification channel", Ordered, func() {
	ctx := context.Background()
	logger := logd.FromContext(ctx)

	credentialFrom := func(key string) *dash0v1beta1.CredentialValueFrom {
		return &dash0v1beta1.CredentialValueFrom{
			SecretKeyRef: &dash0common.SecretKeySelector{Name: credentialsSecretName, Key: key},
		}
	}

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: TestNamespaceName, Name: credentialsSecretName},
			StringData: map[string]string{
				"slack":              "slack-value",
				"webhook-url":        "webhook-url-value",
				"webhook-header-a":   "webhook-header-a-value",
				"webhook-header-b":   "webhook-header-b-value",
				"incidentio-url":     "incidentio-url-value",
				"incidentio-headers": "incidentio-headers-value",
				"opsgenie":           "opsgenie-value",
				"pagerduty":          "pagerduty-value",
				"teams":              "teams-value",
				"discord":            "discord-value",
				"google-chat":        "google-chat-value",
				"ilert":              "ilert-value",
				"all-quiet":          "all-quiet-value",
			},
		})).To(Succeed())
	})

	AfterAll(func() {
		err := k8sClient.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: TestNamespaceName, Name: credentialsSecretName},
		})
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("replaces every secret reference with the value from the secret", func() {
		spec := &dash0v1beta1.Dash0NotificationChannelSpec{
			SlackConfig: &dash0v1beta1.SlackConfig{WebhookURLFrom: credentialFrom("slack")},
			WebhookConfig: &dash0v1beta1.WebhookConfig{
				URLFrom: credentialFrom("webhook-url"),
				HeadersFrom: map[string]dash0v1beta1.CredentialValueFrom{
					"X-Header-A": *credentialFrom("webhook-header-a"),
					"X-Header-B": *credentialFrom("webhook-header-b"),
				},
			},
			IncidentioConfig: &dash0v1beta1.IncidentioConfig{
				URLFrom:     credentialFrom("incidentio-url"),
				HeadersFrom: credentialFrom("incidentio-headers"),
			},
			OpsgenieConfig:          &dash0v1beta1.OpsgenieConfig{ApiKeyFrom: credentialFrom("opsgenie")},
			PagerdutyConfig:         &dash0v1beta1.PagerdutyConfig{KeyFrom: credentialFrom("pagerduty")},
			TeamsWebhookConfig:      &dash0v1beta1.TeamsWebhookConfig{URLFrom: credentialFrom("teams")},
			DiscordWebhookConfig:    &dash0v1beta1.DiscordWebhookConfig{URLFrom: credentialFrom("discord")},
			GoogleChatWebhookConfig: &dash0v1beta1.GoogleChatWebhookConfig{URLFrom: credentialFrom("google-chat")},
			IlertConfig:             &dash0v1beta1.IlertConfig{URLFrom: credentialFrom("ilert")},
			AllQuietConfig:          &dash0v1beta1.AllQuietConfig{URLFrom: credentialFrom("all-quiet")},
		}

		resolvedValues, err := resolveSecretKeyRefs(ctx, k8sClient, TestNamespaceName, spec, logger)
		Expect(err).ToNot(HaveOccurred())

		Expect(spec.SlackConfig.WebhookURL).To(Equal("slack-value"))
		Expect(spec.WebhookConfig.URL).To(Equal("webhook-url-value"))
		Expect(spec.WebhookConfig.Headers).To(Equal(map[string]string{
			"X-Header-A": "webhook-header-a-value",
			"X-Header-B": "webhook-header-b-value",
		}))
		Expect(spec.IncidentioConfig.URL).To(Equal("incidentio-url-value"))
		Expect(spec.IncidentioConfig.Headers).To(Equal("incidentio-headers-value"))
		Expect(spec.OpsgenieConfig.ApiKey).To(Equal("opsgenie-value"))
		Expect(spec.PagerdutyConfig.Key).To(Equal("pagerduty-value"))
		Expect(spec.TeamsWebhookConfig.URL).To(Equal("teams-value"))
		Expect(spec.DiscordWebhookConfig.URL).To(Equal("discord-value"))
		Expect(spec.GoogleChatWebhookConfig.URL).To(Equal("google-chat-value"))
		Expect(spec.IlertConfig.URL).To(Equal("ilert-value"))
		Expect(spec.AllQuietConfig.URL).To(Equal("all-quiet-value"))
		Expect(resolvedValues).To(HaveLen(13))

		serializedSpec, err := json.Marshal(spec)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(serializedSpec)).ToNot(ContainSubstring("From"))
	})

	It("adds headers from the secret to the existing headers", func() {
		spec := &dash0v1beta1.Dash0NotificationChannelSpec{
			WebhookConfig: &dash0v1beta1.WebhookConfig{
				URL:     "https://example.com/webhook",
				Headers: map[string]string{"X-Literal": "literal-value"},
				HeadersFrom: map[string]dash0v1beta1.CredentialValueFrom{
					"X-Header-A": *credentialFrom("webhook-header-a"),
				},
			},
		}

		_, err := resolveSecretKeyRefs(ctx, k8sClient, TestNamespaceName, spec, logger)
		Expect(err).ToNot(HaveOccurred())

		Expect(spec.WebhookConfig.URL).To(Equal("https://example.com/webhook"))
		Expect(spec.WebhookConfig.Headers).To(Equal(map[string]string{
			"X-Literal":  "literal-value",
			"X-Header-A": "webhook-header-a-value",
		}))
		Expect(spec.WebhookConfig.HeadersFrom).To(BeNil())
	})

	It("reports every reference that cannot be resolved", func() {
		spec := &dash0v1beta1.Dash0NotificationChannelSpec{
			IncidentioConfig: &dash0v1beta1.IncidentioConfig{
				URLFrom: credentialFrom("missing-key-1"),
				HeadersFrom: &dash0v1beta1.CredentialValueFrom{
					SecretKeyRef: &dash0common.SecretKeySelector{Name: "missing-secret", Key: "key"},
				},
			},
		}

		_, err := resolveSecretKeyRefs(ctx, k8sClient, TestNamespaceName, spec, logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("secret \"%s/%s\" does not contain key \"missing-key-1\"", TestNamespaceName, credentialsSecretName),
		))
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("failed to fetch secret with name missing-secret in namespace %s", TestNamespaceName),
		))
	})

	It("removes resolved values from Dash0 API errors", func() {
		apiErr := &dash0apiclient.APIError{
			StatusCode: 400,
			Body:       `{"error":"invalid url https://hooks.slack.com/services/secret"}`,
			Message:    "invalid url https://hooks.slack.com/services/secret",
		}

		redactResolvedValues(fmt.Errorf("wrapped: %w", apiErr), []string{"https://hooks.slack.com/services/secret"})

		Expect(apiErr.Body).To(Equal(`{"error":"invalid url <redacted>"}`))
		Expect(apiErr.Message).To(Equal("invalid url <redacted>"))
	})

	It("redacts a resolved value that contains another resolved value as a whole", func() {
		apiErr := &dash0apiclient.APIError{
			StatusCode: 400,
			Body:       `{"error":"invalid url https://hooks.example.com/tok123-abc"}`,
			Message:    "invalid url https://hooks.example.com/tok123-abc",
		}

		redactResolvedValues(apiErr, []string{"tok123", "https://hooks.example.com/tok123-abc"})

		Expect(apiErr.Body).To(Equal(`{"error":"invalid url <redacted>"}`))
		Expect(apiErr.Message).To(Equal("invalid url <redacted>"))
	})
})

var _ = Describe("Validating the secret references of a notification channel", func() {
	ctx := context.Background()
	credentialFrom := &dash0v1beta1.CredentialValueFrom{
		SecretKeyRef: &dash0common.SecretKeySelector{Name: credentialsSecretName, Key: "key"},
	}

	BeforeEach(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
	})

	DescribeTable("validates that exactly one of the value or the secret reference is provided",
		func(spec dash0v1beta1.Dash0NotificationChannelSpec, expectedError string) {
			spec.Display = dash0v1beta1.Dash0NotificationChannelDisplay{Name: "Test Channel"}
			notificationChannel := &dash0v1beta1.Dash0NotificationChannel{
				ObjectMeta: metav1.ObjectMeta{Namespace: TestNamespaceName, Name: "validation-test"},
				Spec:       spec,
			}
			err := k8sClient.Create(ctx, notificationChannel)
			if expectedError == "" {
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Delete(ctx, notificationChannel)).To(Succeed())
			} else {
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(expectedError))
			}
		},
		Entry("accepts a secret reference", dash0v1beta1.Dash0NotificationChannelSpec{
			Type:            "pagerduty",
			PagerdutyConfig: &dash0v1beta1.PagerdutyConfig{KeyFrom: credentialFrom, URL: "https://events.pagerduty.com"},
		}, ""),
		Entry("rejects a value together with a secret reference", dash0v1beta1.Dash0NotificationChannelSpec{
			Type: "pagerduty",
			PagerdutyConfig: &dash0v1beta1.PagerdutyConfig{
				Key:     "integration-key",
				KeyFrom: credentialFrom,
				URL:     "https://events.pagerduty.com",
			},
		}, "exactly one of key or keyFrom has to be provided"),
		Entry("rejects neither a value nor a secret reference", dash0v1beta1.Dash0NotificationChannelSpec{
			Type:        "slack",
			SlackConfig: &dash0v1beta1.SlackConfig{Channel: "#alerts"},
		}, "exactly one of webhookURL or webhookURLFrom has to be provided"),
		Entry("rejects a header with both a value and a secret reference", dash0v1beta1.Dash0NotificationChannelSpec{
			Type: "webhook",
			WebhookConfig: &dash0v1beta1.WebhookConfig{
				URL:         "https://example.com/webhook",
				Headers:     map[string]string{"Authorization": "Bearer token"},
				HeadersFrom: map[string]dash0v1beta1.CredentialValueFrom{"Authorization": *credentialFrom},
			},
		}, "a header must not be provided in both headers and headersFrom"),
		Entry("rejects a header with both a value and a secret reference in different case",
			dash0v1beta1.Dash0NotificationChannelSpec{
				Type: "webhook",
				WebhookConfig: &dash0v1beta1.WebhookConfig{
					URL:         "https://example.com/webhook",
					Headers:     map[string]string{"authorization": "Bearer token"},
					HeadersFrom: map[string]dash0v1beta1.CredentialValueFrom{"Authorization": *credentialFrom},
				},
			}, "a header must not be provided in both headers and headersFrom"),
		Entry("accepts different headers in headers and headersFrom", dash0v1beta1.Dash0NotificationChannelSpec{
			Type: "webhook",
			WebhookConfig: &dash0v1beta1.WebhookConfig{
				URL:         "https://example.com/webhook",
				Headers:     map[string]string{"X-Team": "platform"},
				HeadersFrom: map[string]dash0v1beta1.CredentialValueFrom{"Authorization": *credentialFrom},
			},
		}, ""),
	)
})
