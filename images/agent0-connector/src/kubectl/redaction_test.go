// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

//nolint:lll
func TestTargetedDash0ResourceTypes(t *testing.T) {
	const (
		monitoringResource            = "dash0monitorings.operator.dash0.com"
		operatorConfigurationResource = "dash0operatorconfigurations.operator.dash0.com"
		notificationChannelResource   = "dash0notificationchannels.operator.dash0.com"
		syntheticCheckResource        = "dash0syntheticchecks.operator.dash0.com"
	)

	tests := []struct {
		name      string
		arguments []string
		expected  []string
	}{
		{name: "monitoring resources as yaml", arguments: []string{"get", "dash0monitorings", "-o", "yaml"}, expected: []string{monitoringResource}},
		{name: "singular form as json", arguments: []string{"get", "dash0monitoring", "-o", "json"}, expected: []string{monitoringResource}},
		{name: "kind form as yaml", arguments: []string{"get", "Dash0Monitoring", "-o", "yaml"}, expected: []string{monitoringResource}},
		{name: "fully qualified form as yaml", arguments: []string{"get", "dash0monitorings.v1beta1.operator.dash0.com", "-o", "yaml"}, expected: []string{monitoringResource}},
		{name: "type/name pair as yaml", arguments: []string{"get", "dash0monitoring/my-resource", "-o", "yaml"}, expected: []string{monitoringResource}},
		{name: "type/name pair in a later positional slot as yaml", arguments: []string{"get", "pod/a", "dash0monitoring/b", "-o", "yaml"}, expected: []string{monitoringResource}},
		{name: "operator configuration as yaml", arguments: []string{"get", "dash0operatorconfigurations", "-o", "yaml"}, expected: []string{operatorConfigurationResource}},
		{name: "multi-resource list", arguments: []string{"get", "pods,dash0monitorings,dash0operatorconfiguration", "-o", "yaml"}, expected: []string{monitoringResource, operatorConfigurationResource}},
		{name: "repeated references are deduplicated", arguments: []string{"get", "dash0monitorings", "dash0monitoring/a", "-o", "yaml"}, expected: []string{monitoringResource}},
		{name: "jsonpath output", arguments: []string{"get", "dash0monitorings", "-o", "jsonpath={.items[*].spec.export.dash0.authorization.token}"}, expected: []string{monitoringResource}},
		{name: "custom-columns output", arguments: []string{"get", "dash0monitorings", "-o", "custom-columns=T:.spec.export.dash0.authorization.token"}, expected: []string{monitoringResource}},
		{name: "go-template via --template", arguments: []string{"get", "dash0monitorings", "--template={{.items}}"}, expected: []string{monitoringResource}},
		{name: "attached output format", arguments: []string{"get", "dash0monitorings", "-oyaml"}, expected: []string{monitoringResource}},
		{name: "grouped shorthand output format", arguments: []string{"get", "dash0monitorings", "-Aoyaml"}, expected: []string{monitoringResource}},
		{name: "any occurrence of a repeated output flag counts", arguments: []string{"get", "dash0monitorings", "-o", "yaml", "--output=name"}, expected: []string{monitoringResource}},
		{name: "leading global flag before the subcommand", arguments: []string{"-n", "my-namespace", "get", "dash0monitorings", "-o", "yaml"}, expected: []string{monitoringResource}},
		{name: "describe", arguments: []string{"describe", "dash0monitorings"}, expected: []string{monitoringResource}},
		{name: "describe a single resource", arguments: []string{"describe", "dash0monitoring", "my-resource", "-n", "my-namespace"}, expected: []string{monitoringResource}},
		{name: "describe the operator configuration", arguments: []string{"describe", "dash0operatorconfiguration"}, expected: []string{operatorConfigurationResource}},
		{name: "notification channels as yaml", arguments: []string{"get", "dash0notificationchannels", "-o", "yaml"}, expected: []string{notificationChannelResource}},
		{name: "notification channel kind form", arguments: []string{"get", "Dash0NotificationChannel", "-o", "yaml"}, expected: []string{notificationChannelResource}},
		{name: "synthetic checks as yaml", arguments: []string{"get", "dash0syntheticchecks", "-o", "yaml"}, expected: []string{syntheticCheckResource}},
		{name: "describe a single synthetic check", arguments: []string{"describe", "dash0syntheticcheck", "my-check"}, expected: []string{syntheticCheckResource}},
		{name: "all resource types with secrets at once", arguments: []string{"get", "dash0notificationchannels,dash0syntheticchecks,dash0views", "-o", "yaml"},
			expected: []string{notificationChannelResource, syntheticCheckResource}},

		{name: "table output has no resource content", arguments: []string{"get", "dash0monitorings"}, expected: nil},
		{name: "wide output has no resource content", arguments: []string{"get", "dash0monitorings", "-A", "-o", "wide"}, expected: nil},
		{name: "name output has no resource content", arguments: []string{"get", "dash0monitorings", "-o", "name"}, expected: nil},
		{name: "labels in table output have no resource content", arguments: []string{"get", "dash0monitorings", "--show-labels", "-L", "app"}, expected: nil},
		{name: "explain only prints the schema", arguments: []string{"explain", "dash0monitorings", "--recursive"}, expected: nil},
		{name: "events do not reference the resource positionally", arguments: []string{"events", "--for", "dash0monitoring/my-resource"}, expected: nil},
		{name: "other resources are unaffected", arguments: []string{"get", "pods", "-o", "yaml"}, expected: nil},
		{name: "Dash0 resource types without secrets are unaffected", arguments: []string{"get", "dash0views,dash0teams,dash0samplingrules", "-o", "yaml"}, expected: nil},
		{name: "a resource named like a Dash0 resource is not a resource type", arguments: []string{"get", "pods", "dash0monitorings", "-o", "yaml"}, expected: nil},
		{name: "a namespace named like a Dash0 resource is not a resource type", arguments: []string{"get", "pods", "-n", "dash0monitorings", "-o", "yaml"}, expected: nil},
		{name: "no subcommand", arguments: []string{"--help"}, expected: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractResourceTypesThatRequireSecretRedaction(parseArguments(tt.arguments))
			if !slices.Equal(got, tt.expected) {
				t.Errorf("expected targeted resource types %q, got %q", tt.expected, got)
			}
		})
	}
}

//nolint:lll
func TestExtractionArguments(t *testing.T) {
	const monitoringResource = "dash0monitorings.operator.dash0.com"

	tests := []struct {
		name      string
		arguments []string
		expected  []string
	}{
		{name: "no namespace flag reads the same default namespace", arguments: []string{"describe", "dash0monitorings"},
			expected: []string{"get", monitoringResource, "--output", "json"}},
		{name: "namespace shorthand", arguments: []string{"describe", "dash0monitorings", "-n", "my-namespace"},
			expected: []string{"get", monitoringResource, "--namespace", "my-namespace", "--output", "json"}},
		{name: "namespace long form", arguments: []string{"describe", "dash0monitorings", "--namespace=my-namespace"},
			expected: []string{"get", monitoringResource, "--namespace", "my-namespace", "--output", "json"}},
		{name: "attached namespace value", arguments: []string{"describe", "dash0monitorings", "-nmy-namespace"},
			expected: []string{"get", monitoringResource, "--namespace", "my-namespace", "--output", "json"}},
		{name: "the last namespace wins, as in kubectl", arguments: []string{"describe", "dash0monitorings", "-n", "first", "-n", "second"},
			expected: []string{"get", monitoringResource, "--namespace", "second", "--output", "json"}},
		{name: "all namespaces shorthand", arguments: []string{"describe", "dash0monitorings", "-A"},
			expected: []string{"get", monitoringResource, "--all-namespaces", "--output", "json"}},
		{name: "all namespaces long form", arguments: []string{"describe", "dash0monitorings", "--all-namespaces"},
			expected: []string{"get", monitoringResource, "--all-namespaces", "--output", "json"}},
		{name: "all namespaces in a shorthand group", arguments: []string{"get", "dash0monitorings", "-Aoyaml"},
			expected: []string{"get", monitoringResource, "--all-namespaces", "--output", "json"}},
		{name: "all namespaces wins over a namespace, as in kubectl", arguments: []string{"describe", "dash0monitorings", "-n", "my-namespace", "-A"},
			expected: []string{"get", monitoringResource, "--all-namespaces", "--output", "json"}},
		// Fail-safe: an explicitly disabled --all-namespaces widens the scope rather than narrowing it, and a namespace
		// that is not a resource type slot is not mistaken for one.
		{name: "explicitly disabled all namespaces", arguments: []string{"describe", "dash0monitorings", "--all-namespaces=false", "-n", "my-namespace"},
			expected: []string{"get", monitoringResource, "--all-namespaces", "--output", "json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := parseArguments(tt.arguments)
			got := extractionArguments(parsed, extractResourceTypesThatRequireSecretRedaction(parsed))
			if !slices.Equal(got, tt.expected) {
				t.Errorf("expected extraction arguments %q, got %q", tt.expected, got)
			}
		})
	}
}

//nolint:lll
func TestSecretsFromResponse(t *testing.T) {
	tests := []struct {
		name            string
		arguments       []string
		stdout          string
		stdoutTruncated bool
		expected        []string
		expectedOk      bool
	}{
		{name: "json response", arguments: []string{"get", "dash0monitorings", "-A", "-o", "json"}, stdout: dash0ResourcesJson,
			expected:   []string{operatorConfigurationToken, httpHeaderValue, lastAppliedToken, monitoringToken, grpcHeaderValue},
			expectedOk: true},
		{name: "yaml response", arguments: []string{"get", "dash0monitorings", "-o", "yaml"}, stdout: monitoringResourceYaml,
			expected:   []string{httpHeaderValue, lastAppliedToken, monitoringToken, grpcHeaderValue},
			expectedOk: true},
		{name: "attached output format", arguments: []string{"get", "dash0monitorings", "-oyaml"}, stdout: monitoringResourceYaml,
			expected:   []string{httpHeaderValue, lastAppliedToken, monitoringToken, grpcHeaderValue},
			expectedOk: true},
		{name: "a response without secrets", arguments: []string{"get", "dash0monitorings", "-o", "json"}, stdout: `{"items":[]}`,
			expected: []string{}, expectedOk: true},

		// Everything that is not certainly a full resource document has to be re-read from the cluster instead.
		{name: "truncated response", arguments: []string{"get", "dash0monitorings", "-o", "json"}, stdout: dash0ResourcesJson, stdoutTruncated: true},
		{name: "unparseable response", arguments: []string{"get", "dash0monitorings", "-o", "json"}, stdout: "not json"},
		{name: "multi-document yaml response", arguments: []string{"get", "dash0monitorings", "-o", "yaml"},
			stdout: monitoringResourceYaml + "---\n" + monitoringResourceYaml},
		{name: "jsonpath output that happens to parse", arguments: []string{"get", "dash0monitorings", "-o", "jsonpath={.items[*].spec.exports[0].dash0.authorization.token}"},
			stdout: monitoringToken},
		{name: "custom-columns output", arguments: []string{"get", "dash0monitorings", "-o", "custom-columns=T:.spec.export.dash0.authorization.token"}, stdout: monitoringToken},
		{name: "go-template output", arguments: []string{"get", "dash0monitorings", "-o", "json", "--template={{.items}}"}, stdout: dash0ResourcesJson},
		{name: "repeated output flag", arguments: []string{"get", "dash0monitorings", "-o", "yaml", "--output=json"}, stdout: dash0ResourcesJson},
		{name: "describe output", arguments: []string{"describe", "dash0monitorings"}, stdout: monitoringResourceYaml},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := secretsFromResponse(parseArguments(tt.arguments), tt.stdout, tt.stdoutTruncated)
			if ok != tt.expectedOk {
				t.Fatalf("expected ok=%t, got %t (secrets: %q)", tt.expectedOk, ok, got)
			}
			if tt.expectedOk && !slices.Equal(got, tt.expected) {
				t.Errorf("expected secrets %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestCollectSecrets(t *testing.T) {
	t.Run("collects tokens and header values from all exports", func(t *testing.T) {
		var document any
		if err := json.Unmarshal([]byte(dash0ResourcesJson), &document); err != nil {
			t.Fatalf("cannot parse the test document: %v", err)
		}

		secrets := make(map[string]struct{})
		collectSecretsFromResourceList(document, secrets)

		// Ordered from longest to shortest, so that a secret containing another secret is replaced first.
		expected := []string{
			operatorConfigurationToken,
			httpHeaderValue,
			lastAppliedToken,
			// Secrets of equal length are ordered alphabetically.
			monitoringToken,
			grpcHeaderValue,
		}
		if got := sortedSecrets(secrets); !slices.Equal(got, expected) {
			t.Errorf("expected secrets %q, got %q", expected, got)
		}
	})

	t.Run("collects the third-party credentials of notification channels and synthetic checks", func(t *testing.T) {
		var document any
		if err := json.Unmarshal([]byte(dash0ApiResourcesJson), &document); err != nil {
			t.Fatalf("cannot parse the test document: %v", err)
		}

		secrets := make(map[string]struct{})
		collectSecretsFromResourceList(document, secrets)

		// The order is verified by the test above; here only the set of collected values matters.
		expected := []string{
			slackWebhookUrl,
			webhookUrl,
			webhookHeaderValue,
			incidentioUrl,
			incidentioHeaderValue,
			opsgenieApiKey,
			lastAppliedOpsgenieApiKey,
			pagerdutyIntegrationKey,
			syntheticCheckHeaderValue,
			syntheticCheckQueryParameterValue,
			syntheticCheckPassword,
		}
		got := sortedSecrets(secrets)
		slices.Sort(got)
		slices.Sort(expected)
		if !slices.Equal(got, expected) {
			t.Errorf("expected secrets %q, got %q", expected, got)
		}
	})

	t.Run("collects nothing from resources without secrets", func(t *testing.T) {
		var document any
		if err := json.Unmarshal([]byte(`{"items":[{"spec":{"exports":[{"dash0":{"endpoint":"ingress.dash0.com:4317",
			"authorization":{"secretRef":{"name":"dash0-authorization-secret","key":"token"}}}},
			{"http":{"headers":[{"name":"X-From-Secret","valueFrom":{"secretKeyRef":{"name":"s","key":"k"}}}]}}]}}]}`),
			&document); err != nil {
			t.Fatalf("cannot parse the test document: %v", err)
		}

		secrets := make(map[string]struct{})
		collectSecretsFromResourceList(document, secrets)

		if len(secrets) != 0 {
			t.Errorf("expected no secrets, got %q", sortedSecrets(secrets))
		}
	})
}

func TestRedactSecrets(t *testing.T) {
	secrets := []string{"auth_token-value", "Bearer header-secret"}

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{name: "yaml output",
			text:     "    authorization:\n      token: auth_token-value\n    dataset: default\n",
			expected: "    authorization:\n      token: <redacted>\n    dataset: default\n"},
		{name: "json output",
			text:     `{"authorization":{"token":"auth_token-value"}}`,
			expected: `{"authorization":{"token":"<redacted>"}}`},
		{name: "describe output",
			text:     "      Authorization:\n        Token:  auth_token-value\n",
			expected: "      Authorization:\n        Token:  <redacted>\n"},
		{name: "output without keys, e.g. jsonpath or custom-columns",
			text:     "auth_token-value\n",
			expected: "<redacted>\n"},
		{name: "the last-applied-configuration annotation",
			text:     `      {"spec":{"export":{"dash0":{"authorization":{"token":"auth_token-value"}}}}}`,
			expected: `      {"spec":{"export":{"dash0":{"authorization":{"token":"<redacted>"}}}}}`},
		{name: "header values",
			text:     "        headers:\n        - name: Authorization\n          value: Bearer header-secret\n",
			expected: "        headers:\n        - name: Authorization\n          value: <redacted>\n"},
		{name: "every occurrence in the response",
			text:     "auth_token-value auth_token-value",
			expected: "<redacted> <redacted>"},
		{name: "output without secrets is left untouched",
			text:     "  endpoint: ingress.dash0.com:4317\n",
			expected: "  endpoint: ingress.dash0.com:4317\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactAllSecrets(tt.text, secrets); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestTrimTruncatedSecretFragment(t *testing.T) {
	secrets := []string{"auth_a-rather-long-token-value"}

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{name: "removes a secret fragment at the end of the output",
			text:     "      token: auth_a-rather-long",
			expected: "      token: <redacted>"},
		{name: "keeps output that ends in a fragment shorter than the minimum length",
			text:     "      token: auth",
			expected: "      token: auth"},
		{name: "keeps output that does not end in a secret fragment",
			text:     "      dataset: default",
			expected: "      dataset: default"},
		{name: "keeps output that ends in a secret fragment somewhere in the middle",
			text:     "      token: auth_a-rather-long value: x",
			expected: "      token: auth_a-rather-long value: x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimTruncatedSecretFragment(tt.text, secrets); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestRedactDash0SecretsInCommandResponse(t *testing.T) {
	logger := discardLogger()

	t.Run("redacts all secrets from the response of a Dash0 resource request", func(t *testing.T) {
		fakeKubectlOnPath(t, fakeKubectlWithDash0Resources(dash0ResourcesJson, monitoringResourceYaml))

		// "describe" output cannot be parsed, so the values to redact are re-read from the cluster.
		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-redact",
			Command:   "kubectl",
			Arguments: []string{"describe", "dash0monitorings", "-A"},
		})

		if resp.GetExitCode() != 0 {
			t.Fatalf("expected exit code 0, got %d (stderr: %q)", resp.GetExitCode(), resp.GetStderr())
		}
		for _, secret := range []string{monitoringToken, lastAppliedToken, httpHeaderValue, grpcHeaderValue} {
			if strings.Contains(resp.GetStdout(), secret) {
				t.Errorf("expected the secret %q to be redacted, got %q", secret, resp.GetStdout())
			}
		}
		if count := strings.Count(resp.GetStdout(), redactedValue); count != 4 {
			t.Errorf("expected 4 redacted values, got %d in %q", count, resp.GetStdout())
		}
		// Everything that is not a secret is passed through unchanged.
		if !strings.Contains(resp.GetStdout(), "endpoint: ingress.dash0.com:4317") {
			t.Errorf("expected the non-secret content to be preserved, got %q", resp.GetStdout())
		}
	})

	t.Run("leaves the response of a request for other resources untouched", func(t *testing.T) {
		// The fake kubectl echoes a token-like value for any request; a request that does not target a Dash0 resource
		// must not be post-processed at all.
		fakeKubectlOnPath(t, "#!/bin/sh\necho \""+monitoringToken+"\"\n")

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-other-resource",
			Command:   "kubectl",
			Arguments: []string{"get", "pods", "-o", "yaml"},
		})

		if strings.TrimSpace(resp.GetStdout()) != monitoringToken {
			t.Errorf("expected the response to be passed through unchanged, got %q", resp.GetStdout())
		}
	})

	t.Run("leaves a response that cannot contain resource content untouched", func(t *testing.T) {
		fakeKubectlOnPath(t, "#!/bin/sh\necho \""+monitoringToken+"\"\n")

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-table-output",
			Command:   "kubectl",
			Arguments: []string{"get", "dash0monitorings", "-A"},
		})

		if strings.TrimSpace(resp.GetStdout()) != monitoringToken {
			t.Errorf("expected the response to be passed through unchanged, got %q", resp.GetStdout())
		}
	})

	t.Run("re-reads the Dash0 resources with the namespace scope of the request", func(t *testing.T) {
		// The fake fails the re-read unless it is scoped to the namespace of the request, which would withhold the
		// response.
		fakeKubectlOnPath(t, fakeKubectlWithDash0ResourcesForScope(
			dash0ResourcesJson,
			monitoringResourceYaml,
			"--namespace my-namespace",
		))

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-namespace-scope",
			Command:   "kubectl",
			Arguments: []string{"describe", "dash0monitoring", "my-resource", "-n", "my-namespace"},
		})

		if resp.GetExitCode() != 0 {
			t.Fatalf("expected exit code 0, got %d (stderr: %q)", resp.GetExitCode(), resp.GetStderr())
		}
		if strings.Contains(resp.GetStdout(), monitoringToken) {
			t.Errorf("expected the secret %q to be redacted, got %q", monitoringToken, resp.GetStdout())
		}
	})

	for _, tt := range []struct {
		name             string
		outputFormat     string
		response         string
		expectedSecrets  []string
		expectedRedacted int
	}{
		{
			name:         "json",
			outputFormat: "json",
			response:     dash0ResourcesJson,
			expectedSecrets: []string{
				operatorConfigurationToken,
				monitoringToken,
				lastAppliedToken,
				httpHeaderValue,
				grpcHeaderValue,
			},
			expectedRedacted: 5,
		},
		{
			name:             "yaml",
			outputFormat:     "yaml",
			response:         monitoringResourceYaml,
			expectedSecrets:  []string{monitoringToken, lastAppliedToken, httpHeaderValue, grpcHeaderValue},
			expectedRedacted: 4,
		},
	} {
		t.Run("redacts a "+tt.name+" response without re-reading the Dash0 resources", func(t *testing.T) {
			// The fake fails every invocation that re-reads the Dash0 resources, which would withhold the response, so a
			// redacted response proves that the values were taken from the response itself.
			fakeKubectlOnPath(t, fakeKubectlRejectingTheReRead("dash0monitorings.operator.dash0.com", tt.response))

			resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
				RequestId: "req-redact-from-response-" + tt.name,
				Command:   "kubectl",
				Arguments: []string{"get", "dash0monitorings", "-A", "-o", tt.outputFormat},
			})

			if resp.GetExitCode() != 0 {
				t.Fatalf("expected exit code 0, got %d (stderr: %q)", resp.GetExitCode(), resp.GetStderr())
			}
			for _, secret := range tt.expectedSecrets {
				if strings.Contains(resp.GetStdout(), secret) {
					t.Errorf("expected the secret %q to be redacted, got %q", secret, resp.GetStdout())
				}
			}
			if count := strings.Count(resp.GetStdout(), redactedValue); count != tt.expectedRedacted {
				t.Errorf("expected %d redacted values, got %d in %q", tt.expectedRedacted, count, resp.GetStdout())
			}
		})
	}

	t.Run("redacts the third-party credentials of notification channels and synthetic checks", func(t *testing.T) {
		fakeKubectlOnPath(t, fakeKubectlRejectingTheReRead(
			"dash0notificationchannels.operator.dash0.com",
			dash0ApiResourcesJson,
		))

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-redact-api-resources",
			Command:   "kubectl",
			Arguments: []string{"get", "dash0notificationchannels,dash0syntheticchecks", "-A", "-o", "json"},
		})

		if resp.GetExitCode() != 0 {
			t.Fatalf("expected exit code 0, got %d (stderr: %q)", resp.GetExitCode(), resp.GetStderr())
		}
		for _, secret := range []string{
			slackWebhookUrl,
			webhookUrl,
			webhookHeaderValue,
			incidentioUrl,
			incidentioHeaderValue,
			opsgenieApiKey,
			lastAppliedOpsgenieApiKey,
			pagerdutyIntegrationKey,
			syntheticCheckHeaderValue,
			syntheticCheckQueryParameterValue,
			syntheticCheckPassword,
		} {
			if strings.Contains(resp.GetStdout(), secret) {
				t.Errorf("expected the secret %q to be redacted, got %q", secret, resp.GetStdout())
			}
		}
		// Values that are no credentials are passed through unchanged.
		for _, preserved := range []string{
			pagerdutyEventsUrl,
			syntheticCheckUrl,
			syntheticCheckUsername,
			routingFilterAttributeKey,
			"application/json",
		} {
			if !strings.Contains(resp.GetStdout(), preserved) {
				t.Errorf("expected %q to be preserved, got %q", preserved, resp.GetStdout())
			}
		}
	})

	t.Run("withholds the response when the Dash0 resources cannot be read", func(t *testing.T) {
		// The request itself succeeds, but the invocation that re-reads the Dash0 resources to learn which values need
		// to be redacted fails, so the response must not be handed out.
		fakeKubectlOnPath(t, `#!/bin/sh
if echo "$*" | grep -q -- dash0monitorings.operator.dash0.com; then
  echo "error: the server doesn't have a resource type \"dash0monitorings\"" >&2
  exit 1
fi
echo "`+monitoringToken+`"
`)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-harvest-failure",
			Command:   "kubectl",
			Arguments: []string{"get", "dash0monitorings", "-o", "jsonpath={.items[*].spec.export.dash0.authorization.token}"},
		})

		if resp.GetStdout() != "" {
			t.Errorf("expected the response to be withheld, got %q", resp.GetStdout())
		}
		if !strings.Contains(resp.GetStderr(), "withheld the response") {
			t.Errorf("expected an explanation on stderr, got %q", resp.GetStderr())
		}
		if strings.Contains(resp.GetStderr(), monitoringToken) {
			t.Errorf("expected the explanation to not contain the token, got %q", resp.GetStderr())
		}
		if resp.GetExitCode() != exitCodeRejected {
			t.Errorf("expected the rejected exit code %d, got %d", exitCodeRejected, resp.GetExitCode())
		}
	})

	t.Run("withholds the response when the Dash0 resources cannot be parsed", func(t *testing.T) {
		fakeKubectlOnPath(t, fakeKubectlWithDash0Resources("not json", monitoringResourceYaml))

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-unparsable-harvest",
			Command:   "kubectl",
			Arguments: []string{"describe", "dash0monitoring", "my-resource"},
		})

		if resp.GetStdout() != "" {
			t.Errorf("expected the response to be withheld, got %q", resp.GetStdout())
		}
		if !strings.Contains(resp.GetStderr(), "withheld the response") {
			t.Errorf("expected an explanation on stderr, got %q", resp.GetStderr())
		}
	})
}

// fakeKubectlWithDash0Resources returns a fake kubectl that answers the invocation which re-reads the Dash0 custom
// resources for redaction with dash0Resources, and every other invocation with the given output. The re-reading
// invocation is recognized by the fully qualified resource name, which only it uses.
func fakeKubectlWithDash0Resources(dash0Resources string, output string) string {
	return fakeKubectlWithDash0ResourcesForScope(dash0Resources, output, "")
}

// fakeKubectlWithDash0ResourcesForScope is fakeKubectlWithDash0Resources, but the invocation that re-reads the Dash0
// custom resources has to contain expectedScope in its arguments; otherwise the fake fails, which makes the connector
// withhold the response and thereby lets a test verify the namespace scope of that invocation.
func fakeKubectlWithDash0ResourcesForScope(dash0Resources string, output string, expectedScope string) string {
	scopeCheck := ""
	if expectedScope != "" {
		scopeCheck = `if ! echo "$*" | grep -q -- "` + expectedScope + `"; then
  echo "unexpected namespace scope: $*" >&2
  exit 1
fi
`
	}
	return `#!/bin/sh
if echo "$*" | grep -q -- dash0monitorings.operator.dash0.com; then
` + scopeCheck + `cat <<'DASH0_RESOURCES'
` + dash0Resources + `
DASH0_RESOURCES
else
cat <<'OUTPUT'
` + output + `
OUTPUT
fi
`
}

// fakeKubectlRejectingTheReRead returns a fake kubectl that answers every invocation with the given output, except for
// the invocation that re-reads the given Dash0 custom resource type for redaction, which it fails.
func fakeKubectlRejectingTheReRead(qualifiedResourceType string, output string) string {
	return `#!/bin/sh
if echo "$*" | grep -q -- ` + qualifiedResourceType + `; then
  echo "the Dash0 resources must not be re-read for this request" >&2
  exit 1
fi
cat <<'OUTPUT'
` + output + `
OUTPUT
`
}

const (
	operatorConfigurationToken = "auth_operator-configuration-token"
	monitoringToken            = "auth_monitoring-token"
	lastAppliedToken           = "auth_last-applied-token"
	httpHeaderValue            = "Bearer my-http-header-secret"
	grpcHeaderValue            = "my-grpc-header-secret"

	slackWebhookUrl           = "https://hooks.slack.com/services/T0000/B0000/my-slack-webhook-secret"
	webhookUrl                = "https://webhook.example.com/hook?token=my-webhook-url-secret"
	webhookHeaderValue        = "Bearer my-webhook-header-secret"
	incidentioUrl             = "https://api.incident.io/v2/alert_events/http/my-incidentio-url-secret"
	incidentioHeaderValue     = "Bearer my-incidentio-header-secret"
	opsgenieApiKey            = "my-opsgenie-api-key"
	lastAppliedOpsgenieApiKey = "my-previous-opsgenie-api-key"
	pagerdutyIntegrationKey   = "my-pagerduty-integration-key"
	pagerdutyEventsUrl        = "https://events.pagerduty.com/v2/enqueue"

	syntheticCheckHeaderValue         = "Bearer my-synthetic-check-header-secret"
	syntheticCheckQueryParameterValue = "my-synthetic-check-api-key"
	syntheticCheckUsername            = "my-synthetic-check-user"
	syntheticCheckPassword            = "my-synthetic-check-password"
	syntheticCheckUrl                 = "https://api.example.com/health"
	routingFilterAttributeKey         = "service.namespace"
)

// dash0ResourcesJson is the output of the kubectl invocation that re-reads the Dash0 custom resources for redaction: an
// operator configuration resource and a monitoring resource, covering the deprecated single export as well as the
// exports list, a token sourced from a secret, literal and secret-sourced header values, header values that are too
// short or too well-known to be secrets, and the copy of the spec that kubectl apply leaves behind in the
// last-applied-configuration annotation (with an older token than the current spec).
const dash0ResourcesJson = `{
  "apiVersion": "v1",
  "kind": "List",
  "items": [
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0Monitoring",
      "metadata": { "name": "dash0-monitoring-resource", "namespace": "my-namespace" },
      "spec": {
        "exports": [
          {
            "dash0": {
              "endpoint": "ingress.dash0.com:4317",
              "authorization": { "token": "` + monitoringToken + `" }
            }
          },
          {
            "http": {
              "endpoint": "https://otlp.example.com",
              "headers": [
                { "name": "Authorization", "value": "` + httpHeaderValue + `" },
                { "name": "X-From-Secret", "valueFrom": { "secretKeyRef": { "name": "my-secret", "key": "my-key" } } }
              ]
            }
          },
          {
            "grpc": {
              "endpoint": "otlp.example.com:4317",
              "headers": [
                { "name": "authorization", "value": "` + grpcHeaderValue + `" },
                { "name": "short", "value": "abc" },
                { "name": "x-well-known-upper-case", "value": "False" },
                { "name": "x-well-known-lower-case", "value": "false" },
                { "name": "x-content-type", "value": "application/json" }
              ]
            }
          }
        ]
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1alpha1",
      "kind": "Dash0OperatorConfiguration",
      "metadata": {
        "name": "dash0-operator-configuration-resource",
        "annotations": {
          "kubectl.kubernetes.io/last-applied-configuration":
            "{\"spec\":{\"export\":{\"dash0\":{\"authorization\":{\"token\":\"` + lastAppliedToken + `\"}}}}}",
          "some-other-annotation": "not json"
        }
      },
      "spec": {
        "export": {
          "dash0": {
            "endpoint": "ingress.dash0.com:4317",
            "dataset": "default",
            "authorization": { "token": "` + operatorConfigurationToken + `" }
          }
        }
      }
    }
  ]
}`

// dash0ApiResourcesJson is a "kubectl get dash0notificationchannels,dash0syntheticchecks -o json" response, covering
// the credentials of the third-party integrations of the notification channels (webhook URLs, API keys, and headers in
// all three shapes: a list of name/value pairs, a map, and a single value), the credentials that a synthetic check
// sends with its request, and the copy of the spec that kubectl apply leaves behind in the last-applied-configuration
// annotation. It also contains values that are no credentials and must not be redacted: the attribute key and value of
// a routing filter, the PagerDuty events API URL, the URL, the user name and the assertions of the synthetic check
// request, and header and query parameter values that are too short or too well-known to be secrets.
const dash0ApiResourcesJson = `{
  "apiVersion": "v1",
  "kind": "List",
  "items": [
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": { "name": "slack-channel", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "Slack" },
        "type": "slack",
        "slackConfig": { "webhookURL": "` + slackWebhookUrl + `", "channel": "#alerts" },
        "routing": {
          "assets": [{ "kind": "check_rule", "id": "check-rule-id", "name": "my-check-rule", "dataset": "default" }],
          "filters": [[{ "key": "` + routingFilterAttributeKey + `", "operator": "is", "value": "my-namespace" }]]
        }
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": { "name": "webhook-channel", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "Webhook" },
        "type": "webhook",
        "webhookConfig": {
          "url": "` + webhookUrl + `",
          "headers": { "Authorization": "` + webhookHeaderValue + `", "Content-Type": "application/json" }
        }
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": { "name": "incidentio-channel", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "Incident.io" },
        "type": "incidentio",
        "incidentioConfig": { "url": "` + incidentioUrl + `", "headers": "` + incidentioHeaderValue + `" }
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": {
        "name": "opsgenie-channel",
        "namespace": "my-namespace",
        "annotations": {
          "kubectl.kubernetes.io/last-applied-configuration":
            "{\"spec\":{\"opsgenieConfig\":{\"instance\":\"eu\",\"apiKey\":\"` + lastAppliedOpsgenieApiKey + `\"}}}"
        }
      },
      "spec": {
        "display": { "name": "OpsGenie" },
        "type": "opsgenie",
        "opsgenieConfig": { "instance": "eu", "apiKey": "` + opsgenieApiKey + `" }
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": { "name": "pagerduty-channel", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "PagerDuty" },
        "type": "pagerduty",
        "pagerdutyConfig": { "key": "` + pagerdutyIntegrationKey + `", "url": "` + pagerdutyEventsUrl + `" }
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1alpha1",
      "kind": "Dash0SyntheticCheck",
      "metadata": { "name": "synthetic-check", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "My Check" },
        "enabled": true,
        "plugin": {
          "kind": "http",
          "spec": {
            "request": {
              "method": "get",
              "url": "` + syntheticCheckUrl + `",
              "redirects": "follow",
              "tls": { "allowInsecure": false },
              "tracing": { "addTracingHeaders": true },
              "headers": [
                { "name": "Authorization", "value": "` + syntheticCheckHeaderValue + `" },
                { "name": "Accept", "value": "application/json" }
              ],
              "queryParameters": [
                { "name": "apiKey", "value": "` + syntheticCheckQueryParameterValue + `" },
                { "name": "v", "value": "2" }
              ],
              "basicAuthentication": {
                "username": "` + syntheticCheckUsername + `",
                "password": "` + syntheticCheckPassword + `"
              }
            },
            "assertions": {
              "criticalAssertions": [{ "kind": "status_code", "spec": { "operator": "is", "value": "200" } }],
              "degradedAssertions": []
            }
          }
        }
      }
    }
  ]
}`

// monitoringResourceYaml is a "kubectl get dash0monitorings -o yaml" response containing all secrets of the monitoring
// resource in dash0ResourcesJson, plus the older token from the last-applied-configuration annotation.
const monitoringResourceYaml = `apiVersion: v1
items:
- apiVersion: operator.dash0.com/v1beta1
  kind: Dash0Monitoring
  metadata:
    annotations:
      kubectl.kubernetes.io/last-applied-configuration: |
        {"spec":{"export":{"dash0":{"authorization":{"token":"` + lastAppliedToken + `"}}}}}
    name: dash0-monitoring-resource
    namespace: my-namespace
  spec:
    exports:
    - dash0:
        authorization:
          token: ` + monitoringToken + `
        endpoint: ingress.dash0.com:4317
    - http:
        endpoint: https://otlp.example.com
        headers:
        - name: Authorization
          value: ` + httpHeaderValue + `
    - grpc:
        endpoint: otlp.example.com:4317
        headers:
        - name: authorization
          value: ` + grpcHeaderValue + `
kind: List
`
