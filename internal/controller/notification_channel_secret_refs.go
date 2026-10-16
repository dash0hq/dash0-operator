// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// resolveSecretKeyRefs replaces each secret reference in the spec with the value read from the secret, and returns the
// values that have been read.
func resolveSecretKeyRefs(
	ctx context.Context,
	secretReader client.Reader,
	namespace string,
	spec *dash0v1beta1.Dash0NotificationChannelSpec,
	logger logd.Logger,
) ([]string, error) {
	var resolvedValues []string
	var errs []error
	readSecretValue := func(valueFrom *dash0v1beta1.CredentialValueFrom) (string, bool) {
		value, err := selfmonitoringapiaccess.GetSecretValue(
			ctx,
			secretReader,
			namespace,
			valueFrom.SecretKeyRef,
			"for a notification channel credential",
			logger,
		)
		if err != nil {
			errs = append(errs, err)
			return "", false
		}
		resolvedValues = append(resolvedValues, value)
		return value, true
	}
	resolve := func(value *string, valueFrom **dash0v1beta1.CredentialValueFrom) {
		if *valueFrom == nil {
			return
		}
		if resolvedValue, ok := readSecretValue(*valueFrom); ok {
			*value = resolvedValue
		}
		*valueFrom = nil
	}

	if config := spec.SlackConfig; config != nil {
		resolve(&config.WebhookURL, &config.WebhookURLFrom)
	}
	if config := spec.WebhookConfig; config != nil {
		resolve(&config.URL, &config.URLFrom)
		for _, headerName := range slices.Sorted(maps.Keys(config.HeadersFrom)) {
			valueFrom := config.HeadersFrom[headerName]
			if headerValue, ok := readSecretValue(&valueFrom); ok {
				if config.Headers == nil {
					config.Headers = make(map[string]string, len(config.HeadersFrom))
				}
				config.Headers[headerName] = headerValue
			}
		}
		config.HeadersFrom = nil
	}
	if config := spec.IncidentioConfig; config != nil {
		resolve(&config.URL, &config.URLFrom)
		resolve(&config.Headers, &config.HeadersFrom)
	}
	if config := spec.OpsgenieConfig; config != nil {
		resolve(&config.ApiKey, &config.ApiKeyFrom)
	}
	if config := spec.PagerdutyConfig; config != nil {
		resolve(&config.Key, &config.KeyFrom)
	}
	if config := spec.TeamsWebhookConfig; config != nil {
		resolve(&config.URL, &config.URLFrom)
	}
	if config := spec.DiscordWebhookConfig; config != nil {
		resolve(&config.URL, &config.URLFrom)
	}
	if config := spec.GoogleChatWebhookConfig; config != nil {
		resolve(&config.URL, &config.URLFrom)
	}
	if config := spec.IlertConfig; config != nil {
		resolve(&config.URL, &config.URLFrom)
	}
	if config := spec.AllQuietConfig; config != nil {
		resolve(&config.URL, &config.URLFrom)
	}
	return resolvedValues, errors.Join(errs...)
}

// redactResolvedValues removes the given secret values from a Dash0 API error. The API might echo parts of the request
// in its error response, which ends up in the logs and in the status of the resource.
func redactResolvedValues(err error, resolvedValues []string) {
	var apiErr *dash0apiclient.APIError
	if !errors.As(err, &apiErr) {
		return
	}
	valuesLongestFirst := slices.SortedFunc(slices.Values(resolvedValues), func(a, b string) int {
		return cmp.Compare(len(b), len(a))
	})
	for _, value := range valuesLongestFirst {
		apiErr.Body = strings.ReplaceAll(apiErr.Body, value, "<redacted>")
		apiErr.Message = strings.ReplaceAll(apiErr.Message, value, "<redacted>")
	}
}
