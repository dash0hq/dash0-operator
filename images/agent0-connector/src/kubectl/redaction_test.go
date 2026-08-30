// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
		{name: "workload resources as yaml", arguments: []string{"get", "deployments", "-o", "yaml"}, expected: true},
		{name: "singular workload resource as json", arguments: []string{"get", "daemonset", "my-daemonset", "-o", "json"}, expected: true},
		{name: "workload short name as yaml", arguments: []string{"get", "sts", "-o", "yaml"}, expected: true},
		{name: "workload kind form as yaml", arguments: []string{"get", "CronJob", "-o", "yaml"}, expected: true},
		{name: "fully qualified workload as yaml", arguments: []string{"get", "deployments.v1.apps", "-o", "yaml"}, expected: true},
		{name: "workload type/name pair as json", arguments: []string{"get", "pod/my-pod", "-o", "json"}, expected: true},
		{name: "the all shorthand renders pod specs", arguments: []string{"get", "all", "-o", "yaml"}, expected: true},
		{name: "controller revisions embed a pod template", arguments: []string{"get", "controllerrevisions", "-o", "yaml"}, expected: true},
		{name: "config maps as yaml", arguments: []string{"get", "configmaps", "-o", "yaml"}, expected: true},
		{name: "singular config map as json", arguments: []string{"get", "configmap", "my-cm", "-o", "json"}, expected: true},
		{name: "config map short name as yaml", arguments: []string{"get", "cm", "-o", "yaml"}, expected: true},
		{name: "config map kind form as yaml", arguments: []string{"get", "ConfigMap", "-o", "yaml"}, expected: true},
		{name: "fully qualified config map as yaml", arguments: []string{"get", "configmaps.v1.", "-o", "yaml"}, expected: true},
		{name: "config map type/name pair as json", arguments: []string{"get", "cm/my-cm", "-o", "json"}, expected: true},
		{name: "attached output format", arguments: []string{"get", "dash0monitorings", "-oyaml"}, expected: true},
		{name: "grouped shorthand output format", arguments: []string{"get", "dash0monitorings", "-Aoyaml"}, expected: true},
		{name: "leading global flag before the kubectl command", arguments: []string{"-n", "my-namespace", "get", "dash0monitorings", "-o", "yaml"}, expected: true},

		{name: "table output has no resource content", arguments: []string{"get", "dash0monitorings"}, expected: false},
		{name: "wide output has no resource content", arguments: []string{"get", "dash0monitorings", "-A", "-o", "wide"}, expected: false},
		{name: "name output has no resource content", arguments: []string{"get", "dash0monitorings", "-o", "name"}, expected: false},
		{name: "labels in table output have no resource content", arguments: []string{"get", "dash0monitorings", "--show-labels", "-L", "app"}, expected: false},
		// describe is rejected for these resource types in validation.go, so its response is never redacted here.
		{name: "describe is not redacted", arguments: []string{"describe", "dash0monitorings"}, expected: false},
		{name: "explain only prints the schema", arguments: []string{"explain", "dash0monitorings", "--recursive"}, expected: false},
		{name: "events do not reference the resource positionally", arguments: []string{"events", "--for", "dash0monitoring/my-resource"}, expected: false},
		{name: "workload table output has no resource content", arguments: []string{"get", "deployments", "-o", "wide"}, expected: false},
		{name: "config map table output has no resource content", arguments: []string{"get", "configmaps", "-o", "wide"}, expected: false},
		{name: "a resource named like a config map is not a resource type", arguments: []string{"get", "services", "cm", "-o", "yaml"}, expected: false},
		{name: "other resources are unaffected", arguments: []string{"get", "services", "-o", "yaml"}, expected: false},
		{name: "Dash0 resource types without secrets are unaffected", arguments: []string{"get", "dash0views,dash0teams,dash0samplingrules", "-o", "yaml"}, expected: false},
		{name: "a resource named like a Dash0 resource is not a resource type", arguments: []string{"get", "services", "dash0monitorings", "-o", "yaml"}, expected: false},
		{name: "a namespace named like a Dash0 resource is not a resource type", arguments: []string{"get", "services", "-n", "dash0monitorings", "-o", "yaml"}, expected: false},
		{name: "a resource named like a workload type is not a resource type", arguments: []string{"get", "services", "deployments", "-o", "yaml"}, expected: false},
		{name: "no kubectl command", arguments: []string{"--help"}, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := responseCanContainSecrets(parseKubectlArguments(tt.arguments)); got != tt.expected {
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

// decodeBinaryDataValue returns the decoded content of one binaryData value of a redacted config map response, so that
// a test can assert on what the value holds rather than on its base64.
func decodeBinaryDataValue(t *testing.T, response string, key string) string {
	t.Helper()

	var configMap struct {
		BinaryData map[string]string `json:"binaryData"`
	}
	if err := json.Unmarshal([]byte(response), &configMap); err != nil {
		t.Fatalf("cannot parse the redacted response: %v", err)
	}
	value, hasKey := configMap.BinaryData[key]
	if !hasKey {
		t.Fatalf("the redacted response has no binaryData value %q: %s", key, response)
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("the redacted binaryData value %q is not base64: %v", key, err)
	}
	return string(decoded)
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
			teamsWebhookUrl,
			discordWebhookUrl,
			googleChatWebhookUrl,
			ilertUrl,
			allQuietUrl,
			syntheticCheckHeaderValue,
			syntheticCheckQueryParameterValue,
			syntheticCheckPassword,
			syntheticCheckUrlPassword,
			syntheticCheckUrlApiKey,
			syntheticCheckBodyContent,
		} {
			if strings.Contains(rendered, secret) {
				t.Errorf("expected the credential %q to be redacted, got %q", secret, rendered)
			}
		}
		// Values that are not credentials keep their place, even where they sit next to one.
		for _, preserved := range []string{
			pagerdutyEventsUrl,
			syntheticCheckUsername,
			syntheticCheckUrlEndpoint,
			routingFilterAttributeKey,
		} {
			if !strings.Contains(rendered, preserved) {
				t.Errorf("expected %q to be preserved, got %q", preserved, rendered)
			}
		}
	})

	t.Run("redacts an always-credential field whose value looks like a non-secret", func(t *testing.T) {
		// The Incident.io authorization header value is a credential by definition, so it must not be subject to the
		// plausibility check that keeps a content type or an encoding in place for a generic header. Every value in
		// wellKnownNonSecretValues would be waved through by that check.
		for _, value := range []string{"none", "true", "gzip", "application/json"} {
			document := `{
  "apiVersion": "operator.dash0.com/v1beta1",
  "kind": "Dash0NotificationChannel",
  "metadata": { "name": "incidentio-channel" },
  "spec": {
    "incidentioConfig": { "url": "` + incidentioUrl + `", "headers": "` + value + `" }
  }
}`
			rendered, replaced := redactDocument(t, document)

			if strings.Contains(rendered, `"headers": "`+value+`"`) {
				t.Errorf("expected the Incident.io header value %q to be redacted, got %q", value, rendered)
			}
			if !slices.Contains(replaced, value) {
				t.Errorf("expected the header value %q to be reported as replaced, got %q", value, replaced)
			}
		}
	})

	t.Run("does not report the placeholder itself as a replaced value", func(t *testing.T) {
		// A header named "token" is covered both by redactHeaderValues and by the "token" case, and
		// incidentioConfig.headers both by credentialFieldsPerConfigObject and by the "headers" case. Replacing an
		// already-redacted value again would record the placeholder as a credential and scrub it from stderr.
		document := `{
  "apiVersion": "operator.dash0.com/v1beta1",
  "kind": "Dash0NotificationChannel",
  "metadata": { "name": "webhook-channel" },
  "spec": {
    "incidentioConfig": { "url": "` + incidentioUrl + `", "headers": "` + incidentioHeaderValue + `" },
    "webhookConfig": { "url": "` + webhookUrl + `", "headers": { "token": "` + webhookHeaderValue + `" } }
  }
}`
		_, replaced := redactDocument(t, document)

		if slices.Contains(replaced, redactedValue) {
			t.Errorf("expected the placeholder to not be recorded as a replaced value, got %q", replaced)
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

// nestedObject walks the given path of keys through a parsed document and returns the object it points to.
func nestedObject(t *testing.T, node map[string]any, path ...string) map[string]any {
	t.Helper()

	for _, key := range path {
		child, isObject := node[key].(map[string]any)
		if !isObject {
			t.Fatalf("expected an object at %q, got %v", key, node[key])
		}
		node = child
	}
	return node
}

// TestRedactWorkloadEnvVars covers the environment variables of a pod spec, wherever the pod spec sits.
func TestRedactWorkloadEnvVars(t *testing.T) {
	t.Run("redacts the literal value of every environment variable", func(t *testing.T) {
		rendered, replaced := redactDocument(t, workloadResourcesJson)

		for _, value := range []string{
			deploymentEnvValue,
			initContainerEnvValue,
			ephemeralContainerEnvValue,
			daemonSetEnvValue,
			lastAppliedEnvValue,
			cronJobEnvValue,
			controllerRevisionEnvValue,
		} {
			if strings.Contains(rendered, value) {
				t.Errorf("expected the environment variable value %q to be redacted, got %q", value, rendered)
			}
		}
		// The values are replaced in the document only. Scrubbing them from stderr as well would replace ordinary words
		// in unrelated output, see redactEnvVarValue.
		if len(replaced) > 0 {
			t.Errorf("expected no environment variable value to be scrubbed from stderr, got %q", replaced)
		}
	})

	t.Run("leaves everything that is not a literal value in place", func(t *testing.T) {
		rendered, _ := redactDocument(t, workloadResourcesJson)

		for _, preserved := range []string{
			envVarName,
			envVarSecretKeyName,
			"DATABASE_URL",
			"INIT_SECRET",
			"metadata.name",
			"secretKeyRef",
			"envFrom",
			"app:1.0.0",
		} {
			if !strings.Contains(rendered, preserved) {
				t.Errorf("expected %q to be preserved, got %q", preserved, rendered)
			}
		}
	})

	t.Run("redacts the environment variables of a container in every shape", func(t *testing.T) {
		tests := []struct {
			name     string
			document string
			expected string
		}{
			{
				name:     "a literal value",
				document: `{"env":[{"name":"A","value":"secret"}]}`,
				expected: `{"env":[{"name":"A","value":"(redacted)"}]}`,
			},
			{
				name:     "several variables of one container",
				document: `{"env":[{"name":"A","value":"a"},{"name":"B","value":"b"}]}`,
				expected: `{"env":[{"name":"A","value":"(redacted)"},{"name":"B","value":"(redacted)"}]}`,
			},
			{
				// A well-known non-secret value keeps its place in a header, but not in an environment variable: any
				// workload can hold a credential there, and there is no way to tell one from an innocuous value.
				name:     "a value that would be well-known for a header",
				document: `{"env":[{"name":"A","value":"application/json"}]}`,
				expected: `{"env":[{"name":"A","value":"(redacted)"}]}`,
			},
			{
				name:     "a value sourced via valueFrom",
				document: `{"env":[{"name":"A","valueFrom":{"secretKeyRef":{"name":"s","key":"k"}}}]}`,
				expected: `{"env":[{"name":"A","valueFrom":{"secretKeyRef":{"key":"k","name":"s"}}}]}`,
			},
			{
				name:     "an empty value",
				document: `{"env":[{"name":"A","value":""}]}`,
				expected: `{"env":[{"name":"A","value":""}]}`,
			},
			{
				// Not the env var list of a pod spec: the walk only replaces a string held by the "value" key of a list
				// entry, so a field that happens to be called "env" is left alone.
				name:     "an env field that is not a list",
				document: `{"env":{"A":"a"}}`,
				expected: `{"env":{"A":"a"}}`,
			},
			{
				name:     "an env list of strings",
				document: `{"env":["A=a"]}`,
				expected: `{"env":["A=a"]}`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var parsed any
				if err := json.Unmarshal([]byte(tt.document), &parsed); err != nil {
					t.Fatalf("cannot parse the test document: %v", err)
				}
				redactDocumentNodeRecursively(parsed, &redactor{values: make(map[string]struct{})})
				rendered, err := json.Marshal(parsed)
				if err != nil {
					t.Fatalf("cannot render the redacted test document: %v", err)
				}
				if string(rendered) != tt.expected {
					t.Errorf("expected %s, got %s", tt.expected, rendered)
				}
			})
		}
	})

	t.Run("redacts the header values of the probes and lifecycle hooks of a pod spec", func(t *testing.T) {
		tests := []struct {
			name     string
			document string
			expected string
		}{
			{
				name:     "a liveness probe header",
				document: `{"livenessProbe":{"httpGet":{"httpHeaders":[{"name":"Authorization","value":"Bearer t0ken"}]}}}`,
				expected: `{"livenessProbe":{"httpGet":{"httpHeaders":[{"name":"Authorization","value":"(redacted)"}]}}}`,
			},
			{
				name:     "a lifecycle hook header",
				document: `{"lifecycle":{"preStop":{"httpGet":{"httpHeaders":[{"name":"X-Api-Key","value":"k3y"}]}}}}`,
				expected: `{"lifecycle":{"preStop":{"httpGet":{"httpHeaders":[{"name":"X-Api-Key","value":"(redacted)"}]}}}}`,
			},
			{
				// The same plausibility check as for the header values of an export: a well-known non-secret value
				// keeps its place, see redactHeaderValueIfPlausible.
				name:     "a well-known non-secret header value",
				document: `{"readinessProbe":{"httpGet":{"httpHeaders":[{"name":"Accept","value":"application/json"}]}}}`,
				expected: `{"readinessProbe":{"httpGet":{"httpHeaders":[{"name":"Accept","value":"application/json"}]}}}`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var parsed any
				if err := json.Unmarshal([]byte(tt.document), &parsed); err != nil {
					t.Fatalf("cannot parse the test document: %v", err)
				}
				redactDocumentNodeRecursively(parsed, &redactor{values: make(map[string]struct{})})
				rendered, err := json.Marshal(parsed)
				if err != nil {
					t.Fatalf("cannot render the redacted test document: %v", err)
				}
				if string(rendered) != tt.expected {
					t.Errorf("expected %s, got %s", tt.expected, rendered)
				}
			})
		}
	})
}

// The "\n" sequences in these documents are two characters in the Go source and become newlines when the surrounding
// JSON document is parsed, so the value of a data key holds a multi-line configuration file, as it does in a real
// config map.
const (
	collectorConfigMapJson = `{
		"apiVersion": "v1",
		"kind": "ConfigMap",
		"metadata": {"name": "dash0-operator-collector", "namespace": "dash0-system"},
		"data": {
			"config.yaml": "exporters:\n  otlp/dash0:\n    endpoint: ingress.dash0.com:4317\n` +
		`    headers:\n      Authorization: Bearer collector-cm-token\n` +
		`service:\n  pipelines:\n    traces:\n      exporters:\n      - otlp/dash0\n"
		}
	}`

	rootCaConfigMapJson = `{
		"apiVersion": "v1",
		"kind": "ConfigMap",
		"metadata": {"name": "kube-root-ca.crt", "namespace": "kube-system"},
		"data": {
			"ca.crt": "-----BEGIN CERTIFICATE-----\nMIIDBTCCAe2gAwIBAgIIfjlZk27R4Lgw\n-----END CERTIFICATE-----\n"
		}
	}`

	// A config map that was created with "kubectl apply" carries a verbatim copy of itself, its data included, in the
	// kubectl.kubernetes.io/last-applied-configuration annotation. The credential therefore sits in the response twice
	// and has to be redacted in both places.
	appliedConfigMapJson = `{
		"apiVersion": "v1",
		"kind": "ConfigMap",
		"metadata": {
			"name": "applied-cm",
			"namespace": "default",
			"annotations": {
				"kubectl.kubernetes.io/last-applied-configuration": ` +
		`"{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":\"applied-cm\"},\"data\":` +
		`{\"config.yaml\":\"exporters:\\n  otlp/example:\\n    headers:\\n` +
		`      Authorization: Bearer applied-cm-token\\n\"}}"
			}
		},
		"data": {
			"config.yaml": "exporters:\n  otlp/example:\n    headers:\n` +
		`      Authorization: Bearer applied-cm-token\n"
		}
	}`
)

func TestRedactConfigMapData(t *testing.T) {
	t.Run("redacts a credential inside the yaml of a data value", func(t *testing.T) {
		rendered, replaced := redactDocument(t, collectorConfigMapJson)

		if strings.Contains(rendered, "collector-cm-token") {
			t.Errorf("expected the export header value to be redacted, got %q", rendered)
		}
		if !strings.Contains(rendered, redactedValue) {
			t.Errorf("expected the redaction placeholder in the response, got %q", rendered)
		}
		// Only the credential is replaced; the rest of the collector configuration stays readable.
		for _, preserved := range []string{"ingress.dash0.com:4317", "otlp/dash0", "pipelines", "Authorization"} {
			if !strings.Contains(rendered, preserved) {
				t.Errorf("expected %q to be preserved, got %q", preserved, rendered)
			}
		}
		if !slices.Contains(replaced, "Bearer collector-cm-token") {
			t.Errorf("expected the header value to be scrubbed from stderr as well, got %q", replaced)
		}
	})

	t.Run("leaves a data value the walk finds no credential in exactly as kubectl rendered it", func(t *testing.T) {
		rendered, replaced := redactDocument(t, rootCaConfigMapJson)

		// A PEM block is not an object or a list, so there is nothing to walk and nothing to render again.
		if !strings.Contains(rendered, "-----BEGIN CERTIFICATE-----\\nMIIDBTCCAe2gAwIBAgIIfjlZk27R4Lgw") {
			t.Errorf("expected the certificate to be preserved verbatim, got %q", rendered)
		}
		if strings.Contains(rendered, redactedValue) {
			t.Errorf("expected nothing to be redacted, got %q", rendered)
		}
		if len(replaced) > 0 {
			t.Errorf("expected no value to be scrubbed from stderr, got %q", replaced)
		}
	})

	t.Run("redacts the credential in the copy kubectl apply embeds in an annotation", func(t *testing.T) {
		rendered, replaced := redactDocument(t, appliedConfigMapJson)

		if strings.Contains(rendered, "applied-cm-token") {
			t.Errorf("expected the credential to be redacted in the data and in the annotation, got %q", rendered)
		}
		// Both the data value and the copy of it in the annotation must carry the placeholder, otherwise only one of
		// the two was redacted.
		if placeholders := strings.Count(rendered, redactedValue); placeholders != 2 {
			t.Errorf("expected 2 redaction placeholders, one per copy of the credential, got %d in %q",
				placeholders, rendered)
		}
		if !slices.Contains(replaced, "Bearer applied-cm-token") {
			t.Errorf("expected the header value to be scrubbed from stderr as well, got %q", replaced)
		}
	})

	t.Run("decodes a binaryData value, redacts it, and hands it back base64", func(t *testing.T) {
		const plaintext = "headers:\n  Authorization: Bearer binary-cm-token\nport: 8080\n"
		encoded := base64.StdEncoding.EncodeToString([]byte(plaintext))
		document := fmt.Sprintf(`{"kind":"ConfigMap","binaryData":{"app.yaml":%q}}`, encoded)

		rendered, replaced := redactDocument(t, document)

		if strings.Contains(rendered, "binary-cm-token") {
			t.Errorf("expected the credential not to appear in plaintext, got %q", rendered)
		}
		if strings.Contains(rendered, encoded) {
			t.Errorf("expected the original base64 value to be replaced, got %q", rendered)
		}
		// The response must stay a valid config map, so the redacted value has to be base64 again rather than the
		// plaintext the walk worked on.
		decoded := decodeBinaryDataValue(t, rendered, "app.yaml")
		if strings.Contains(decoded, "binary-cm-token") {
			t.Errorf("expected the credential to be redacted, got %q", decoded)
		}
		if !strings.Contains(decoded, redactedValue) {
			t.Errorf("expected the redaction placeholder in the decoded value, got %q", decoded)
		}
		if !strings.Contains(decoded, "port: 8080") {
			t.Errorf("expected the rest of the value to stay readable, got %q", decoded)
		}
		// stderr carries the encoded form, in which the plaintext credential does not occur, so the encoded value has
		// to be scrubbed in its own right.
		if !slices.Contains(replaced, encoded) {
			t.Errorf("expected the encoded value to be scrubbed from stderr as well, got %q", replaced)
		}
	})

	t.Run("leaves a binaryData value holding no credential exactly as kubectl rendered it", func(t *testing.T) {
		encoded := base64.StdEncoding.EncodeToString([]byte("port: 8080\n# a comment\n"))
		document := fmt.Sprintf(`{"kind":"ConfigMap","binaryData":{"app.yaml":%q}}`, encoded)

		rendered, replaced := redactDocument(t, document)

		if !strings.Contains(rendered, encoded) {
			t.Errorf("expected the value to be preserved verbatim, got %q", rendered)
		}
		if len(replaced) > 0 {
			t.Errorf("expected no value to be scrubbed from stderr, got %q", replaced)
		}
	})

	t.Run("redacts a data value in every shape", func(t *testing.T) {
		tests := []struct {
			name     string
			document string
			expected string
		}{
			{
				name:     "a json value stays json",
				document: `{"kind":"ConfigMap","data":{"app.json":"{\"headers\":{\"Authorization\":\"tok-json\"}}"}}`,
				expected: `{"data":{"app.json":"{\n    \"headers\": {\n` +
					`        \"Authorization\": \"(redacted)\"\n    }\n}\n"},"kind":"ConfigMap"}`,
			},
			{
				name:     "a token at the top level of a yaml value",
				document: `{"kind":"ConfigMap","data":{"app.yaml":"token: tok-yaml\nport: 8080\n"}}`,
				expected: `{"data":{"app.yaml":"port: 8080\ntoken: (redacted)\n"},"kind":"ConfigMap"}`,
			},
			{
				// kubectl renders binaryData as base64, so a value that is not base64 cannot be one and there is
				// nothing in it to parse.
				name:     "a binaryData value that is not base64 is left alone",
				document: `{"kind":"ConfigMap","binaryData":{"app.yaml":"password: pw-binary\n"}}`,
				expected: `{"binaryData":{"app.yaml":"password: pw-binary\n"},"kind":"ConfigMap"}`,
			},
			{
				name:     "a value without a credential keeps its formatting",
				document: `{"kind":"ConfigMap","data":{"app.yaml":"port:   8080\n# a comment\n"}}`,
				expected: `{"data":{"app.yaml":"port:   8080\n# a comment\n"},"kind":"ConfigMap"}`,
			},
			{
				name:     "a scalar value is left alone",
				document: `{"kind":"ConfigMap","data":{"greeting":"hello"}}`,
				expected: `{"data":{"greeting":"hello"},"kind":"ConfigMap"}`,
			},
			{
				name:     "an empty value is left alone",
				document: `{"kind":"ConfigMap","data":{"empty":""}}`,
				expected: `{"data":{"empty":""},"kind":"ConfigMap"}`,
			},
			{
				name:     "every document of a multi-document yaml value is redacted",
				document: `{"kind":"ConfigMap","data":{"app.yaml":"token: first\n---\ntoken: second\n"}}`,
				expected: `{"data":{"app.yaml":"token: (redacted)\n---\ntoken: (redacted)\n"},"kind":"ConfigMap"}`,
			},
			{
				// A document start marker before the first content line opens the first document rather than a second
				// one, and starting a YAML file with it is a widespread convention.
				name:     "a value opening with the document start marker is redacted",
				document: `{"kind":"ConfigMap","data":{"app.yaml":"---\ntoken: tok-marker\n"}}`,
				expected: `{"data":{"app.yaml":"token: (redacted)\n"},"kind":"ConfigMap"}`,
			},
			{
				name:     "a value opening with a comment and the document start marker is redacted",
				document: `{"kind":"ConfigMap","data":{"app.yaml":"# a comment\n---\ntoken: tok-comment\n"}}`,
				expected: `{"data":{"app.yaml":"token: (redacted)\n"},"kind":"ConfigMap"}`,
			},
			{
				// A trailing separator does not open a document, so the value must not gain a "null" document.
				name:     "a value ending with the document separator is redacted",
				document: `{"kind":"ConfigMap","data":{"app.yaml":"token: tok-trailing\n---\n"}}`,
				expected: `{"data":{"app.yaml":"token: (redacted)\n"},"kind":"ConfigMap"}`,
			},
			{
				// The separator only starts a document at the top level, which is why the documents are split by a
				// parser rather than by searching the text for the separator.
				name:     "a separator inside a block scalar does not split the value",
				document: `{"kind":"ConfigMap","data":{"app.yaml":"token: tok-block\nnotes: |\n  ---\n  not a document\n"}}`,
				expected: `{"data":{"app.yaml":"notes: |\n  ---\n  not a document\ntoken: (redacted)\n"},"kind":"ConfigMap"}`,
			},
			{
				// The walk reaches a config map wherever it occurs, so a config map nested in the data of another one
				// is redacted as well.
				name: "a config map nested in the data of a config map is redacted",
				document: `{"kind":"ConfigMap","data":{"nested.yaml":` +
					`"kind: ConfigMap\ndata:\n  inner.yaml: |\n    token: tok-nested\n"}}`,
				expected: `{"data":{"nested.yaml":"data:\n  inner.yaml: |\n    token: (redacted)\nkind: ConfigMap\n"},` +
					`"kind":"ConfigMap"}`,
			},
			{
				// "data" is a generic field name, so the walk is bound to the kind rather than to the field.
				name:     "the data of another kind is not walked",
				document: `{"kind":"SomeCustomResource","data":{"app.yaml":"token: keep-me\n"}}`,
				expected: `{"data":{"app.yaml":"token: keep-me\n"},"kind":"SomeCustomResource"}`,
			},
			{
				name:     "a resource without a kind is not walked",
				document: `{"data":{"app.yaml":"token: keep-me\n"}}`,
				expected: `{"data":{"app.yaml":"token: keep-me\n"}}`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var parsed any
				if err := json.Unmarshal([]byte(tt.document), &parsed); err != nil {
					t.Fatalf("cannot parse the test document: %v", err)
				}
				redacted := &redactor{values: make(map[string]struct{})}
				if err := redactResourceList(parsed, redacted); err != nil {
					t.Fatalf("cannot redact the test document: %v", err)
				}
				rendered, err := json.Marshal(parsed)
				if err != nil {
					t.Fatalf("cannot render the redacted test document: %v", err)
				}
				if string(rendered) != tt.expected {
					t.Errorf("expected %s, got %s", tt.expected, rendered)
				}
			})
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

// TestRedactSyntheticCheckRequest covers the two parts of a synthetic check request whose redaction is not driven by
// a field name alone: the request body, which is nested in a generically named object, and the URL, of which only the
// credential-bearing parts are replaced.
func TestRedactSyntheticCheckRequest(t *testing.T) {
	t.Run("redacts the credentials in the URL and the whole request body of a synthetic check", func(t *testing.T) {
		document := `{
  "apiVersion": "operator.dash0.com/v1alpha1",
  "kind": "Dash0SyntheticCheck",
  "metadata": { "name": "synthetic-check" },
  "spec": {
    "plugin": {
      "kind": "http",
      "spec": {
        "request": {
          "method": "post",
          "url": "` + syntheticCheckUrl + `",
          "body": { "kind": "form", "spec": { "content": "` + syntheticCheckBodyContent + `" } }
        }
      }
    }
  }
}`
		rendered, replaced := redactDocument(t, document)

		var parsed map[string]any
		if err := json.Unmarshal([]byte(rendered), &parsed); err != nil {
			t.Fatalf("the redacted document does not parse: %v", err)
		}
		request := nestedObject(t, parsed, "spec", "plugin", "spec", "request")
		expectedUrl := "https://" + syntheticCheckUsername + ":" + redactedValue + "@" + syntheticCheckUrlEndpoint +
			"?apiKey=" + redactedValue + "&format=application/json"
		if request["url"] != expectedUrl {
			t.Errorf("expected the URL to be redacted to %q, got %q", expectedUrl, request["url"])
		}
		if content := nestedObject(t, request, "body", "spec")["content"]; content != redactedValue {
			t.Errorf("expected the request body to be redacted, got %q", content)
		}
		for _, secret := range []string{syntheticCheckUrlPassword, syntheticCheckUrlApiKey, syntheticCheckBodyContent} {
			if !slices.Contains(replaced, secret) {
				t.Errorf("expected the secret %q to be reported as replaced, got %q", secret, replaced)
			}
		}
	})

	t.Run("leaves a request whose URL and body hold no credential untouched", func(t *testing.T) {
		// The body is a container in the custom resource, but a response can render anything: a shape the redaction
		// does not expect must be passed through rather than crash the walk.
		document := `{
  "apiVersion": "operator.dash0.com/v1alpha1",
  "kind": "Dash0SyntheticCheck",
  "metadata": { "name": "synthetic-check" },
  "spec": {
    "plugin": {
      "kind": "http",
      "spec": {
        "request": { "method": "get", "url": "https://api.example.com/health", "body": "not an object" }
      }
    }
  }
}`
		rendered, replaced := redactDocument(t, document)

		if !strings.Contains(rendered, `"url": "https://api.example.com/health"`) {
			t.Errorf("expected the URL to be preserved, got %q", rendered)
		}
		if !strings.Contains(rendered, `"body": "not an object"`) {
			t.Errorf("expected the unexpected body shape to be preserved, got %q", rendered)
		}
		if len(replaced) > 0 {
			t.Errorf("expected nothing to be replaced, got %q", replaced)
		}
	})
}

func TestRedactCredentialsInUrl(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
		replaced []string
	}{
		{
			name: "leaves a URL that carries no credential untouched, including its escaping",
			// A space in the path and a redundant escape would both be normalized by rendering the URL through
			// net/url, which is why the redaction works on the original string.
			url:      "https://api.example.com/health%20check/%61?format=application/json&debug",
			expected: "https://api.example.com/health%20check/%61?format=application/json&debug",
		},
		{
			name:     "replaces the password of the user information and keeps the user name",
			url:      "https://my-user:my-url-password@api.example.com:8443/health",
			expected: "https://my-user:" + redactedValue + "@api.example.com:8443/health",
			replaced: []string{"my-url-password"},
		},
		{
			name:     "replaces user information that has no password",
			url:      "https://my-url-token@api.example.com/health",
			expected: "https://" + redactedValue + "@api.example.com/health",
			replaced: []string{"my-url-token"},
		},
		{
			name:     "leaves an empty password alone",
			url:      "https://my-user:@api.example.com/health",
			expected: "https://my-user:@api.example.com/health",
		},
		{
			name:     "replaces the query parameter values that are not well-known non-secrets",
			url:      "https://api.example.com/health?apiKey=my-url-api-key&format=application/json&debug",
			expected: "https://api.example.com/health?apiKey=" + redactedValue + "&format=application/json&debug",
			replaced: []string{"my-url-api-key"},
		},
		{
			name:     "records the encoded and the decoded form of a query parameter value",
			url:      "https://api.example.com/health?apiKey=my%2Durl%2Dapi%2Dkey",
			expected: "https://api.example.com/health?apiKey=" + redactedValue,
			replaced: []string{"my%2Durl%2Dapi%2Dkey", "my-url-api-key"},
		},
		{
			name:     "keeps the fragment and redacts the query of the same URL",
			url:      "https://api.example.com/health?apiKey=my-url-api-key#my-fragment",
			expected: "https://api.example.com/health?apiKey=" + redactedValue + "#my-fragment",
			replaced: []string{"my-url-api-key"},
		},
		{
			name:     "does not mistake an at sign in the path for user information",
			url:      "https://api.example.com/users/me@example.com?format=application/json",
			expected: "https://api.example.com/users/me@example.com?format=application/json",
		},
		{
			name:     "redacts the query of a URL without a scheme",
			url:      "api.example.com/health?apiKey=my-url-api-key",
			expected: "api.example.com/health?apiKey=" + redactedValue,
			replaced: []string{"my-url-api-key"},
		},
		{
			name: "replaces a URL that cannot be parsed as a whole",
			// The parts of a URL that net/url rejects cannot be located, so it fails closed rather than handing out
			// something it did not understand.
			url:      "https://my-user:my-url-password@api.example.com/%zz",
			expected: redactedValue,
			replaced: []string{"https://my-user:my-url-password@api.example.com/%zz"},
		},
		{
			name:     "leaves a value that already is the placeholder alone",
			url:      redactedValue,
			expected: redactedValue,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := map[string]any{"url": test.url}
			redacted := &redactor{values: make(map[string]struct{})}

			redactUrlParts(node, "url", redacted)

			if node["url"] != test.expected {
				t.Errorf("expected %q, got %q", test.expected, node["url"])
			}
			for _, value := range test.replaced {
				if _, wasReplaced := redacted.values[value]; !wasReplaced {
					t.Errorf("expected %q to be recorded as replaced, got %v", value, redacted.values)
				}
			}
			if len(redacted.values) != len(test.replaced) {
				t.Errorf("expected exactly %d replaced value(s), got %v", len(test.replaced), redacted.values)
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
			teamsWebhookUrl,
			discordWebhookUrl,
			googleChatWebhookUrl,
			ilertUrl,
			allQuietUrl,
			syntheticCheckHeaderValue,
			syntheticCheckQueryParameterValue,
			syntheticCheckPassword,
			syntheticCheckUrlPassword,
			syntheticCheckUrlApiKey,
			syntheticCheckBodyContent,
		} {
			if strings.Contains(resp.GetStdout(), secret) {
				t.Errorf("expected the credential %q to be redacted, got %q", secret, resp.GetStdout())
			}
		}
	})

	t.Run("redacts the environment variable values of a workload response", func(t *testing.T) {
		fakeKubectlEchoing(t, workloadResourcesJson)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-redact-workloads",
			Command:   "kubectl",
			Arguments: []string{"get", "deployments,daemonsets,cronjobs,controllerrevisions,pods", "-A", "-o", "json"},
		})

		if resp.GetExitCode() != 0 {
			t.Fatalf("expected exit code 0, got %d (stderr: %q)", resp.GetExitCode(), resp.GetStderr())
		}
		for _, value := range []string{
			deploymentEnvValue,
			initContainerEnvValue,
			ephemeralContainerEnvValue,
			daemonSetEnvValue,
			lastAppliedEnvValue,
			cronJobEnvValue,
			controllerRevisionEnvValue,
		} {
			if strings.Contains(resp.GetStdout(), value) {
				t.Errorf("expected the environment variable value %q to be redacted, got %q", value, resp.GetStdout())
			}
		}
		// The rest of the workload stays readable, which is what makes the response useful for diagnosing it.
		for _, preserved := range []string{envVarName, "app:1.0.0", "my-cron-job"} {
			if !strings.Contains(resp.GetStdout(), preserved) {
				t.Errorf("expected %q to be preserved, got %q", preserved, resp.GetStdout())
			}
		}
	})

	t.Run("leaves the response of a request for other resources untouched", func(t *testing.T) {
		// The fake kubectl echoes a token-like value for any request; a request that targets neither a Dash0 resource nor
		// a workload resource must not be post-processed at all.
		fakeKubectlEchoing(t, monitoringToken)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-other-resource",
			Command:   "kubectl",
			Arguments: []string{"get", "services", "-o", "yaml"},
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
		{
			// Both formats are redactable on their own, so validation accepts the request, but which one kubectl actually
			// applied is not replicated here (see parseableOutputFormat).
			name:      "the output format is set more than once",
			arguments: []string{"get", "dash0monitorings", "-o", "yaml", "-o", "json"},
			response:  monitoringResourceYaml,
		},
		// "kubectl get -o json/yaml" always renders an object or a v1.List, so a document whose root is not a map is a
		// shape the redaction was not written for. It is handed out unredacted unless it fails closed.
		{
			name:      "the response root is a json array",
			arguments: []string{"get", "dash0monitorings", "-o", "json"},
			response:  `[ { "kind": "Dash0Monitoring" } ]`,
		},
		{
			name:      "the response root is a json scalar",
			arguments: []string{"get", "dash0monitorings", "-o", "json"},
			response:  `"just a string"`,
		},
		{
			name:      "the response root is a yaml scalar",
			arguments: []string{"get", "dash0monitorings", "-o", "yaml"},
			response:  `null`,
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

func TestRedactDash0SecretsWithTruncatedStdout(t *testing.T) {
	logger := discardLogger()

	t.Run("withholds a response whose stdout was truncated", func(t *testing.T) {
		// Output beyond maxStdoutBytes is dropped, so the captured stdout is a prefix of the document kubectl
		// rendered. That prefix cannot be parsed, and a document that cannot be parsed cannot be redacted, so the
		// response has to be withheld even though its beginning holds the token in plaintext.
		//
		// The fake kubectl emits the token first and then pads past the limit, in 4 KiB chunks built by doubling a
		// literal, so that no external tool is needed.
		fakeKubectlOnPath(t, `#!/bin/sh
printf '{ "token": "`+monitoringToken+`", "padding": "'
s=0123456789012345678901234567890123456789012345678901234567890123
s=$s$s$s$s
s=$s$s$s$s
s=$s$s$s$s
s=$s$s$s$s
i=0
while [ $i -lt 200 ]; do
  printf '%s' "$s"
  i=$((i+1))
done
printf '" }\n'
`)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-truncated-stdout",
			Command:   "kubectl",
			Arguments: []string{"get", "dash0monitorings", "-o", "json"},
		})

		if resp.GetStdout() != "" {
			t.Errorf("expected the response to be withheld, got %d bytes of stdout", len(resp.GetStdout()))
		}
		if strings.Contains(resp.GetStderr(), monitoringToken) {
			t.Errorf("expected the token to be withheld, got %q", resp.GetStderr())
		}
		if !strings.Contains(resp.GetStderr(), "withheld the response") {
			t.Errorf("expected an explanation on stderr, got %q", resp.GetStderr())
		}
		// Pins that the truncation is what withheld the response, rather than the truncated prefix failing to parse
		// afterwards: redactSecretsInResponse checks stdoutTruncated before it attempts to parse.
		if !strings.Contains(resp.GetStderr(), "exceeds the limit") {
			t.Errorf("expected the truncation to be given as the reason, got %q", resp.GetStderr())
		}
		if resp.GetExitCode() != exitCodeRejected {
			t.Errorf("expected the rejected exit code %d, got %d", exitCodeRejected, resp.GetExitCode())
		}
	})

	t.Run("withholds a truncated response for every redactable output format", func(t *testing.T) {
		for _, format := range []string{outputFormatJson, outputFormatYaml} {
			fakeKubectlOnPath(t, `#!/bin/sh
s=0123456789012345678901234567890123456789012345678901234567890123
s=$s$s$s$s
s=$s$s$s$s
s=$s$s$s$s
s=$s$s$s$s
i=0
while [ $i -lt 200 ]; do
  printf '%s' "$s"
  i=$((i+1))
done
`)

			resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
				RequestId: "req-truncated-" + format,
				Command:   "kubectl",
				Arguments: []string{"get", "dash0monitorings", "-o", format},
			})

			if resp.GetStdout() != "" {
				t.Errorf("expected the -o %s response to be withheld, got %d bytes", format, len(resp.GetStdout()))
			}
			if !strings.Contains(resp.GetStderr(), "exceeds the limit") {
				t.Errorf("expected the -o %s response to be withheld for truncation, got %q", format, resp.GetStderr())
			}
		}
	})

	t.Run("hands out a truncated response for a resource type without secrets", func(t *testing.T) {
		// Truncation only withholds where redaction is required. A resource type that cannot contain a credential keeps
		// the existing behaviour: the truncated output is returned with a notice.
		fakeKubectlOnPath(t, `#!/bin/sh
s=0123456789012345678901234567890123456789012345678901234567890123
s=$s$s$s$s
s=$s$s$s$s
s=$s$s$s$s
s=$s$s$s$s
i=0
while [ $i -lt 200 ]; do
  printf '%s' "$s"
  i=$((i+1))
done
`)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-truncated-services",
			Command:   "kubectl",
			Arguments: []string{"get", "services", "-o", "json"},
		})

		if resp.GetStdout() == "" {
			t.Error("expected the truncated response to be handed out")
		}
		if !strings.Contains(resp.GetStdout(), "truncated the output") {
			t.Error("expected a truncation notice on stdout")
		}
		if strings.Contains(resp.GetStderr(), "withheld the response") {
			t.Errorf("expected the response to not be withheld, got %q", resp.GetStderr())
		}
	})
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
	teamsWebhookUrl           = "https://example.webhook.office.com/webhookb2/my-teams-webhook-secret"
	discordWebhookUrl         = "https://discord.com/api/webhooks/1234567890/my-discord-webhook-secret"
	ilertUrl                  = "https://api.ilert.com/api/v1/events/dash0/my-ilert-secret"
	allQuietUrl               = "https://events.allquiet.app/api/webhook/my-all-quiet-secret"

	// The canonical shape of a Google Chat webhook URL: its credentials are query parameters, so the value contains an
	// ampersand, which the JSON serializer escapes. Redacting the parsed document rather than the rendered text is what
	// makes this work.
	googleChatWebhookUrl = "https://chat.googleapis.com/v1/spaces/S/messages?key=my-chat-key&token=my-chat-token"

	syntheticCheckHeaderValue         = "Bearer my-synthetic-check-header-secret"
	syntheticCheckQueryParameterValue = "my-synthetic-check-api-key"
	syntheticCheckUsername            = "my-synthetic-check-user"
	syntheticCheckPassword            = "my-synthetic-check-password"
	syntheticCheckBodyContent         = "client_secret=my-synthetic-check-body-secret"
	routingFilterAttributeKey         = "service.namespace"

	// The URL a synthetic check requests is not a credential itself, but it carries two: the password of its user
	// information and the value of its "apiKey" query parameter. Everything else keeps its place, including the
	// user name and the query parameter whose value is a well-known non-secret.
	syntheticCheckUrlPassword = "my-synthetic-check-url-password"
	syntheticCheckUrlApiKey   = "my-synthetic-check-url-api-key"
	syntheticCheckUrlEndpoint = "api.example.com/health"
	syntheticCheckUrl         = "https://" + syntheticCheckUsername + ":" + syntheticCheckUrlPassword + "@" +
		syntheticCheckUrlEndpoint + "?apiKey=" + syntheticCheckUrlApiKey + "&format=application/json"
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
// a routing filter, the PagerDuty events API URL, the endpoint and the user name of the synthetic check request, its
// assertions, and header and query parameter values that are too short or too well-known to be secrets.
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
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": { "name": "teams-channel", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "Teams" },
        "type": "teams_webhook",
        "teamsWebhookConfig": { "url": "` + teamsWebhookUrl + `" }
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": { "name": "discord-channel", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "Discord" },
        "type": "discord_webhook",
        "discordWebhookConfig": { "url": "` + discordWebhookUrl + `" }
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": { "name": "google-chat-channel", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "Google Chat" },
        "type": "google_chat_webhook",
        "googleChatWebhookConfig": { "url": "` + googleChatWebhookUrl + `" }
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": { "name": "ilert-channel", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "iLert" },
        "type": "ilert",
        "ilertConfig": { "url": "` + ilertUrl + `" }
      }
    },
    {
      "apiVersion": "operator.dash0.com/v1beta1",
      "kind": "Dash0NotificationChannel",
      "metadata": { "name": "all-quiet-channel", "namespace": "my-namespace" },
      "spec": {
        "display": { "name": "All Quiet" },
        "type": "all_quiet",
        "allQuietConfig": { "url": "` + allQuietUrl + `" }
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
              },
              "body": {
                "kind": "form",
                "spec": { "content": "` + syntheticCheckBodyContent + `" }
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

const (
	deploymentEnvValue         = "postgres://app:my-deployment-db-password@db:5432/app"
	initContainerEnvValue      = "my-init-container-secret"
	ephemeralContainerEnvValue = "my-ephemeral-container-secret"
	daemonSetEnvValue          = "auth_daemonset-token"
	lastAppliedEnvValue        = "auth_previous-daemonset-token"
	cronJobEnvValue            = "my-cron-job-secret"
	controllerRevisionEnvValue = "my-controller-revision-secret"

	// The values that must survive the walk: the name of a variable is not a credential, and a variable that sources
	// its value via valueFrom holds a reference rather than a value.
	envVarName          = "DASH0_AUTHORIZATION_TOKEN"
	envVarSecretKeyName = "my-env-var-secret"
)

// workloadResourcesJson is a "kubectl get deployments,daemonsets,cronjobs,controllerrevisions,pods -o json" response.
// It covers every place a pod spec can hold the literal value of an environment variable: the containers and the init
// containers of a deployment, the ephemeral containers of a pod, the pod template a cron job nests two levels deep,
// the copy of a pod template that a controller revision keeps in its "data" field, and the copy of the manifest that
// kubectl apply leaves behind in the last-applied-configuration annotation. It also contains what must not be
// redacted: the names of the variables, a value sourced via valueFrom, and an envFrom reference.
const workloadResourcesJson = `{
  "apiVersion": "v1",
  "kind": "List",
  "items": [
    {
      "apiVersion": "apps/v1",
      "kind": "Deployment",
      "metadata": { "name": "my-deployment", "namespace": "my-namespace" },
      "spec": {
        "template": {
          "spec": {
            "initContainers": [
              {
                "name": "init",
                "image": "init:1.0.0",
                "env": [{ "name": "INIT_SECRET", "value": "` + initContainerEnvValue + `" }]
              }
            ],
            "containers": [
              {
                "name": "app",
                "image": "app:1.0.0",
                "env": [
                  { "name": "DATABASE_URL", "value": "` + deploymentEnvValue + `" },
                  { "name": "POD_NAME", "valueFrom": { "fieldRef": { "fieldPath": "metadata.name" } } },
                  {
                    "name": "API_KEY",
                    "valueFrom": { "secretKeyRef": { "name": "` + envVarSecretKeyName + `", "key": "api-key" } }
                  }
                ],
                "envFrom": [{ "secretRef": { "name": "` + envVarSecretKeyName + `" } }]
              }
            ]
          }
        }
      }
    },
    {
      "apiVersion": "apps/v1",
      "kind": "DaemonSet",
      "metadata": {
        "name": "dash0-operator-agent",
        "namespace": "dash0-system",
        "annotations": {
          "kubectl.kubernetes.io/last-applied-configuration":
            "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"agent\",\"env\":[{\"name\":\"` +
	envVarName + `\",\"value\":\"` + lastAppliedEnvValue + `\"}]}]}}}}"
        }
      },
      "spec": {
        "template": {
          "spec": {
            "containers": [
              { "name": "agent", "env": [{ "name": "` + envVarName + `", "value": "` + daemonSetEnvValue + `" }] }
            ]
          }
        }
      }
    },
    {
      "apiVersion": "batch/v1",
      "kind": "CronJob",
      "metadata": { "name": "my-cron-job", "namespace": "my-namespace" },
      "spec": {
        "jobTemplate": {
          "spec": {
            "template": {
              "spec": {
                "containers": [
                  { "name": "job", "env": [{ "name": "JOB_SECRET", "value": "` + cronJobEnvValue + `" }] }
                ]
              }
            }
          }
        }
      }
    },
    {
      "apiVersion": "apps/v1",
      "kind": "ControllerRevision",
      "metadata": { "name": "dash0-operator-agent-6cdb9d7c8f", "namespace": "dash0-system" },
      "revision": 3,
      "data": {
        "spec": {
          "template": {
            "spec": {
              "containers": [
                {
                  "name": "agent",
                  "env": [{ "name": "` + envVarName + `", "value": "` + controllerRevisionEnvValue + `" }]
                }
              ]
            }
          }
        }
      }
    },
    {
      "apiVersion": "v1",
      "kind": "Pod",
      "metadata": { "name": "my-pod", "namespace": "my-namespace" },
      "spec": {
        "ephemeralContainers": [
          { "name": "debugger", "env": [{ "name": "DEBUG_TOKEN", "value": "` + ephemeralContainerEnvValue + `" }] }
        ]
      }
    }
  ]
}`
