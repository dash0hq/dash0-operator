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
func TestResponseCanContainSecrets(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		expected  bool
	}{
		{name: "monitoring resources as yaml", arguments: []string{"get", "dash0monitorings", "-o", "yaml"}, expected: true},
		{name: "singular form as json", arguments: []string{"get", "dash0monitoring", "-o", "json"}, expected: true},
		{name: "kind form as yaml", arguments: []string{"get", "Dash0Monitoring", "-o", "yaml"}, expected: true},
		{name: "fully qualified form as yaml", arguments: []string{"get", "dash0monitorings.v1beta1.operator.dash0.com", "-o", "yaml"}, expected: true},
		{name: "type/name pair as yaml", arguments: []string{"get", "dash0monitoring/my-resource", "-o", "yaml"}, expected: true},
		{name: "type/name pair in a later positional slot as yaml", arguments: []string{"get", "pod/a", "dash0monitoring/b", "-o", "yaml"}, expected: true},
		{name: "operator configuration as yaml", arguments: []string{"get", "dash0operatorconfigurations", "-o", "yaml"}, expected: true},
		{name: "notification channels as yaml", arguments: []string{"get", "dash0notificationchannels", "-o", "yaml"}, expected: true},
		{name: "synthetic checks as json", arguments: []string{"get", "dash0syntheticchecks", "-o", "json"}, expected: true},
		{name: "multi-resource list", arguments: []string{"get", "pods,dash0monitorings,dash0operatorconfiguration", "-o", "yaml"}, expected: true},
		{name: "attached output format", arguments: []string{"get", "dash0monitorings", "-oyaml"}, expected: true},
		{name: "grouped shorthand output format", arguments: []string{"get", "dash0monitorings", "-Aoyaml"}, expected: true},
		{name: "leading global flag before the subcommand", arguments: []string{"-n", "my-namespace", "get", "dash0monitorings", "-o", "yaml"}, expected: true},

		{name: "table output has no resource content", arguments: []string{"get", "dash0monitorings"}, expected: false},
		{name: "wide output has no resource content", arguments: []string{"get", "dash0monitorings", "-A", "-o", "wide"}, expected: false},
		{name: "name output has no resource content", arguments: []string{"get", "dash0monitorings", "-o", "name"}, expected: false},
		{name: "labels in table output have no resource content", arguments: []string{"get", "dash0monitorings", "--show-labels", "-L", "app"}, expected: false},
		// describe is rejected for these resource types in validation.go, so its response is never redacted here.
		{name: "describe is not redacted", arguments: []string{"describe", "dash0monitorings"}, expected: false},
		{name: "explain only prints the schema", arguments: []string{"explain", "dash0monitorings", "--recursive"}, expected: false},
		{name: "events do not reference the resource positionally", arguments: []string{"events", "--for", "dash0monitoring/my-resource"}, expected: false},
		{name: "other resources are unaffected", arguments: []string{"get", "pods", "-o", "yaml"}, expected: false},
		{name: "Dash0 resource types without secrets are unaffected", arguments: []string{"get", "dash0views,dash0teams,dash0samplingrules", "-o", "yaml"}, expected: false},
		{name: "a resource named like a Dash0 resource is not a resource type", arguments: []string{"get", "pods", "dash0monitorings", "-o", "yaml"}, expected: false},
		{name: "a namespace named like a Dash0 resource is not a resource type", arguments: []string{"get", "pods", "-n", "dash0monitorings", "-o", "yaml"}, expected: false},
		{name: "no subcommand", arguments: []string{"--help"}, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := responseCanContainSecrets(parseArguments(tt.arguments)); got != tt.expected {
				t.Errorf("expected responseCanContainSecrets=%t, got %t", tt.expected, got)
			}
		})
	}
}

// redactDocument parses a document, redacts it and renders it again as JSON, mirroring what redactSecretsInResponse
// does, and returns the rendered document together with the values that were replaced.
func redactDocument(t *testing.T, document string) (string, []string) {
	t.Helper()
	return redactDocumentAs(t, document, outputFormatJson)
}

// redactDocumentAs is redactDocument for a given output format, which also selects the placeholder.
func redactDocumentAs(t *testing.T, document string, format string) (string, []string) {
	t.Helper()

	var parsed any
	if err := json.Unmarshal([]byte(document), &parsed); err != nil {
		t.Fatalf("cannot parse the test document: %v", err)
	}
	redacted := &redactor{values: make(map[string]struct{})}
	if err := redactResourceList(parsed, redacted); err != nil {
		t.Fatalf("cannot redact the test document: %v", err)
	}
	rendered, err := renderResponseDocument(format, parsed)
	if err != nil {
		t.Fatalf("cannot render the redacted test document: %v", err)
	}
	return rendered, redacted.valuesToScrubFromStderr()
}

func TestRedactDocument(t *testing.T) {
	t.Run("redacts tokens and header values of all exports", func(t *testing.T) {
		rendered, replaced := redactDocument(t, dash0ResourcesJson)

		for _, secret := range []string{
			operatorConfigurationToken,
			monitoringToken,
			lastAppliedToken,
			httpHeaderValue,
			grpcHeaderValue,
		} {
			if strings.Contains(rendered, secret) {
				t.Errorf("expected the secret %q to be redacted, got %q", secret, rendered)
			}
			if !slices.Contains(replaced, secret) {
				t.Errorf("expected the secret %q to be reported as replaced, got %q", secret, replaced)
			}
		}
		// Everything that is not a credential stays in place.
		if !strings.Contains(rendered, "ingress.dash0.com:4317") {
			t.Errorf("expected the non-secret content to be preserved, got %q", rendered)
		}
	})

	t.Run("redacts the third-party credentials of notification channels and synthetic checks", func(t *testing.T) {
		rendered, _ := redactDocument(t, dash0ApiResourcesJson)

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
			if strings.Contains(rendered, secret) {
				t.Errorf("expected the credential %q to be redacted, got %q", secret, rendered)
			}
		}
		// Values that are not credentials keep their place, even where they sit next to one.
		for _, preserved := range []string{
			pagerdutyEventsUrl,
			syntheticCheckUsername,
			syntheticCheckUrl,
			routingFilterAttributeKey,
		} {
			if !strings.Contains(rendered, preserved) {
				t.Errorf("expected %q to be preserved, got %q", preserved, rendered)
			}
		}
	})

	t.Run("keeps the redacted last-applied-configuration annotation parseable", func(t *testing.T) {
		rendered, _ := redactDocument(t, dash0ResourcesJson)

		var document map[string]any
		if err := json.Unmarshal([]byte(rendered), &document); err != nil {
			t.Fatalf("the redacted document does not parse: %v", err)
		}
		items, _ := document["items"].([]any)
		for _, item := range items {
			annotations, hasAnnotations := annotationsOf(item)
			if !hasAnnotations {
				continue
			}
			for name, value := range annotations {
				annotation, isString := value.(string)
				if !isString || !strings.HasPrefix(strings.TrimSpace(annotation), "{") {
					continue
				}
				var embedded any
				if err := json.Unmarshal([]byte(annotation), &embedded); err != nil {
					t.Errorf("the redacted %q annotation is not valid JSON: %v", name, err)
				}
			}
		}
	})

	t.Run("redacts nothing when the values are sourced from a secret", func(t *testing.T) {
		rendered, replaced := redactDocument(t, `{"items":[{"spec":{"exports":[{"dash0":{"endpoint":"ingress.dash0.com:4317",
			"authorization":{"secretRef":{"name":"dash0-authorization-secret","key":"token"}}}},
			{"http":{"headers":[{"name":"X-From-Secret","valueFrom":{"secretKeyRef":{"name":"s","key":"k"}}}]}}]}}]}`)

		if len(replaced) != 0 {
			t.Errorf("expected no replaced values, got %q", replaced)
		}
		if strings.Contains(rendered, redactedValue) {
			t.Errorf("expected nothing to be redacted, got %q", rendered)
		}
	})

	t.Run("redacts a short credential without touching anything else", func(t *testing.T) {
		// A credential is replaced where it lives, so even a value that occurs all over the response cannot garble it.
		// Matching this value would have rewritten the resource name and every other occurrence of "ku" as well.
		rendered, replaced := redactDocument(t,
			`{"items":[{"metadata":{"name":"ku"},"spec":{"exports":[{"dash0":{"authorization":{"token":"ku"}}}]}}]}`)

		if !strings.Contains(rendered, `"name": "ku"`) {
			t.Errorf("expected the unrelated occurrence to be preserved, got %q", rendered)
		}
		if got := tokenOfFirstExport(t, rendered); got != redactedValue {
			t.Errorf("expected the token to be %q, got %q", redactedValue, got)
		}
		// Too short to be scrubbed from stderr, where it would match unrelated output.
		if len(replaced) != 0 {
			t.Errorf("expected no value to be scrubbed from stderr, got %q", replaced)
		}
	})

	t.Run("keeps well-known non-secret header values", func(t *testing.T) {
		// A header is the one credential-bearing position that also carries values which are not credentials.
		rendered, _ := redactDocument(t, `{"items":[{"spec":{"exports":[{"http":{"headers":[
			{"name":"Content-Type","value":"application/json"},
			{"name":"Accept-Encoding","value":"GZIP"},
			{"name":"Authorization","value":"Bearer my-secret-header"}]}}]}}]}`)

		for _, preserved := range []string{"application/json", "GZIP"} {
			if !strings.Contains(rendered, preserved) {
				t.Errorf("expected the well-known header value %q to be preserved, got %q", preserved, rendered)
			}
		}
		if strings.Contains(rendered, "Bearer my-secret-header") {
			t.Errorf("expected the credential header value to be redacted, got %q", rendered)
		}
	})

	// A placeholder is escaped and quoted like any other value, so it must be one that both formats render verbatim:
	// angle brackets, for instance, would show up as "\u003credacted\u003e" in a JSON response.
	for _, format := range []string{outputFormatJson, outputFormatYaml} {
		t.Run("renders the placeholder verbatim as "+format, func(t *testing.T) {
			rendered, _ := redactDocumentAs(t,
				`{"items":[{"spec":{"exports":[{"dash0":{"authorization":{"token":"`+monitoringToken+`"}}}]}}]}`,
				format)

			if !strings.Contains(rendered, redactedValue) {
				t.Errorf("expected the placeholder %q to be rendered verbatim, got %q", redactedValue, rendered)
			}
		})
	}
}

// tokenOfFirstExport parses a rendered document and returns
// items[0].spec.exports[0].dash0.authorization.token, so that a test can assert on the value rather than on its
// rendering.
func tokenOfFirstExport(t *testing.T, rendered string) string {
	t.Helper()

	var document map[string]any
	if err := json.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatalf("the rendered document does not parse: %v", err)
	}
	items, _ := document["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("the rendered document has no items: %q", rendered)
	}
	item, _ := items[0].(map[string]any)
	spec, _ := item["spec"].(map[string]any)
	exports, _ := spec["exports"].([]any)
	if len(exports) == 0 {
		t.Fatalf("the rendered document has no exports: %q", rendered)
	}
	export, _ := exports[0].(map[string]any)
	dash0, _ := export["dash0"].(map[string]any)
	authorization, _ := dash0["authorization"].(map[string]any)
	token, _ := authorization["token"].(string)
	return token
}

// annotationsOf returns the metadata.annotations of a parsed resource.
func annotationsOf(resource any) (map[string]any, bool) {
	resourceMap, isMap := resource.(map[string]any)
	if !isMap {
		return nil, false
	}
	metadata, isMap := resourceMap["metadata"].(map[string]any)
	if !isMap {
		return nil, false
	}
	annotations, isMap := metadata["annotations"].(map[string]any)
	return annotations, isMap
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
			expected: "    authorization:\n      token: (redacted)\n    dataset: default\n"},
		{name: "json output",
			text:     `{"authorization":{"token":"auth_token-value"}}`,
			expected: `{"authorization":{"token":"(redacted)"}}`},
		{name: "describe output",
			text:     "      Authorization:\n        Token:  auth_token-value\n",
			expected: "      Authorization:\n        Token:  (redacted)\n"},
		{name: "output without keys, e.g. jsonpath or custom-columns",
			text:     "auth_token-value\n",
			expected: "(redacted)\n"},
		{name: "the last-applied-configuration annotation",
			text:     `      {"spec":{"export":{"dash0":{"authorization":{"token":"auth_token-value"}}}}}`,
			expected: `      {"spec":{"export":{"dash0":{"authorization":{"token":"(redacted)"}}}}}`},
		{name: "header values",
			text:     "        headers:\n        - name: Authorization\n          value: Bearer header-secret\n",
			expected: "        headers:\n        - name: Authorization\n          value: (redacted)\n"},
		{name: "every occurrence in the response",
			text:     "auth_token-value auth_token-value",
			expected: "(redacted) (redacted)"},
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

func TestRedactDash0SecretsInCommandResponse(t *testing.T) {
	logger := discardLogger()

	for _, tt := range []struct {
		name         string
		outputFormat string
		response     string
	}{
		{name: "json", outputFormat: "json", response: dash0ResourcesJson},
		{name: "yaml", outputFormat: "yaml", response: monitoringResourceYaml},
	} {
		t.Run("redacts the secrets of a "+tt.name+" response", func(t *testing.T) {
			fakeKubectlEchoing(t, tt.response)

			resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
				RequestId: "req-redact-" + tt.name,
				Command:   "kubectl",
				Arguments: []string{"get", "dash0monitorings", "-A", "-o", tt.outputFormat},
			})

			if resp.GetExitCode() != 0 {
				t.Fatalf("expected exit code 0, got %d (stderr: %q)", resp.GetExitCode(), resp.GetStderr())
			}
			for _, secret := range []string{monitoringToken, lastAppliedToken, httpHeaderValue, grpcHeaderValue} {
				if strings.Contains(resp.GetStdout(), secret) {
					t.Errorf("expected the secret %q to be redacted, got %q", secret, resp.GetStdout())
				}
			}
			// Everything that is not a secret is passed through unchanged.
			if !strings.Contains(resp.GetStdout(), "ingress.dash0.com:4317") {
				t.Errorf("expected the non-secret content to be preserved, got %q", resp.GetStdout())
			}
		})
	}

	t.Run("renders a response that holds no secret exactly as kubectl did", func(t *testing.T) {
		// The response is parsed and rendered again, so a document without credentials has to come back byte for byte:
		// kubectl serializes a custom resource from its unstructured form, which is what the connector renders too.
		response := `{
    "apiVersion": "v1",
    "items": [
        {
            "apiVersion": "operator.dash0.com/v1beta1",
            "kind": "Dash0Monitoring",
            "metadata": {
                "name": "my-resource",
                "namespace": "my-namespace"
            },
            "spec": {
                "logCollection": {
                    "enabled": true
                }
            }
        }
    ],
    "kind": "List"
}`
		fakeKubectlEchoing(t, response)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-render-fidelity",
			Command:   "kubectl",
			Arguments: []string{"get", "dash0monitorings", "-o", "json"},
		})

		if resp.GetStdout() != response+"\n" {
			t.Errorf("expected the response to be rendered unchanged, got %q", resp.GetStdout())
		}
	})

	t.Run("redacts the third-party credentials of notification channels and synthetic checks", func(t *testing.T) {
		fakeKubectlEchoing(t, dash0ApiResourcesJson)

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
				t.Errorf("expected the credential %q to be redacted, got %q", secret, resp.GetStdout())
			}
		}
	})

	t.Run("leaves the response of a request for other resources untouched", func(t *testing.T) {
		// The fake kubectl echoes a token-like value for any request; a request that does not target a Dash0 resource
		// must not be post-processed at all.
		fakeKubectlEchoing(t, monitoringToken)

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
		fakeKubectlEchoing(t, monitoringToken)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-content-free",
			Command:   "kubectl",
			Arguments: []string{"get", "dash0monitorings", "-o", "name"},
		})

		if strings.TrimSpace(resp.GetStdout()) != monitoringToken {
			t.Errorf("expected the response to be passed through unchanged, got %q", resp.GetStdout())
		}
	})

	// A response that cannot be parsed cannot be redacted, and must not be handed out.
	for _, tt := range []struct {
		name      string
		arguments []string
		response  string
	}{
		{
			name:      "the response does not parse",
			arguments: []string{"get", "dash0monitorings", "-o", "json"},
			response:  "not json",
		},
		{
			name:      "the response is a multi-document yaml stream",
			arguments: []string{"get", "dash0monitorings", "-o", "yaml"},
			response:  monitoringResourceYaml + "\n---\n" + monitoringResourceYaml,
		},
	} {
		t.Run("withholds the response when "+tt.name, func(t *testing.T) {
			fakeKubectlEchoing(t, tt.response)

			resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
				RequestId: "req-unparseable",
				Command:   "kubectl",
				Arguments: tt.arguments,
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
	}
}

func TestRedactDash0SecretsWithEmptyStdout(t *testing.T) {
	logger := discardLogger()

	t.Run("withholds a response that only has content on stderr", func(t *testing.T) {
		// kubectl reports some errors by formatting the offending value - for a template or jsonpath error even the
		// whole object - into a message on stderr while stdout stays empty. There is no document to redact then, so the
		// response has to be withheld rather than handed out.
		fakeKubectlOnPath(t, `#!/bin/sh
echo "error: the object given to the engine was map[token:`+monitoringToken+`]" >&2
exit 1
`)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-secret-on-stderr",
			Command:   "kubectl",
			Arguments: []string{"get", "dash0monitorings", "-o", "yaml"},
		})

		if strings.Contains(resp.GetStderr(), monitoringToken) {
			t.Errorf("expected the token to be withheld, got %q", resp.GetStderr())
		}
		if !strings.Contains(resp.GetStderr(), "withheld the response") {
			t.Errorf("expected an explanation on stderr, got %q", resp.GetStderr())
		}
	})

	t.Run("passes a response through that has no content at all", func(t *testing.T) {
		fakeKubectlOnPath(t, "#!/bin/sh\nexit 0\n")

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-empty-response",
			Command:   "kubectl",
			Arguments: []string{"get", "dash0monitorings", "-o", "yaml"},
		})

		if strings.Contains(resp.GetStderr(), "withheld the response") {
			t.Errorf("expected the empty response to be handed out unchanged, got %q", resp.GetStderr())
		}
		if resp.GetExitCode() != 0 {
			t.Errorf("expected exit code 0, got %d (stderr: %q)", resp.GetExitCode(), resp.GetStderr())
		}
	})

	t.Run("scrubs the redacted values from stderr", func(t *testing.T) {
		// The document is redacted structurally; stderr is not a document, so the values that were replaced in the
		// document are removed from it by matching them.
		fakeKubectlOnPath(t, `#!/bin/sh
cat <<'OUTPUT'
`+monitoringResourceYaml+`
OUTPUT
echo "warning: could not reach the endpoint with `+grpcHeaderValue+`" >&2
`)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-stderr-scrub",
			Command:   "kubectl",
			Arguments: []string{"get", "dash0monitorings", "-o", "yaml"},
		})

		if strings.Contains(resp.GetStderr(), grpcHeaderValue) {
			t.Errorf("expected the header value to be scrubbed from stderr, got %q", resp.GetStderr())
		}
		if !strings.Contains(resp.GetStderr(), redactedValue) {
			t.Errorf("expected the redaction placeholder on stderr, got %q", resp.GetStderr())
		}
	})
}

// fakeKubectlEchoing installs a fake kubectl that answers every invocation with the given output.
func fakeKubectlEchoing(t *testing.T, output string) {
	t.Helper()
	fakeKubectlOnPath(t, `#!/bin/sh
cat <<'OUTPUT'
`+output+`
OUTPUT
`)
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

// dash0ResourcesJson is a "kubectl get dash0operatorconfigurations,dash0monitorings -o json" response: an
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
