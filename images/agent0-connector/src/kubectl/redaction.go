// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Handles secret redaction for the output of kubectl commands.
//
// The stdout response of the kubectl invocation is parsed into a document, the credential values are replaced
// within that document, and the document is rendered again.
//
// This is only possible for formats the connector can parse, reliably interprete, and render itself. Requests that
// targets a Dash0 custom resource which can contain secrets are restricted to output formats that satisfy these
// constraints (see safeOrRedactableOutputFormats in validation.go):
//
//   - "-o json" and "-o yaml" render the resources as a document. kubectl serializes a custom resource from its
//     unstructured (map) form, so re-rendering the parsed document reproduces kubectl's output byte for byte, apart
//     from the redacted values.
//   - the content-free formats ("-o name", "-o wide", the default table) do not expose the content of a resource at
//     all and are passed through untouched.
//   - "-o go-template", "-o jsonpath", "-o custom-columns" and "kubectl describe" are rejected for these resource
//     types. The template formats can reshape the response and make it impossible to find the secret. kubectl describe
//     renders a text format that is not meant to be parsed; in neither case can the credentials be located in the
//     output. Furthermore, with go-template, individual secret characters could be exfiltrated via expresssions like
//     {{printf "%.6s" .token}}' and similar, enabling exfiltrating a secret over multiple requests, piece by piece.
//   - file-related output formats (go-template-file etc.) and --raw are disallowed outright for all resource types, so
//     they do not require specific treatment with respect to secret redaction
//
// A response that cannot be parsed after all - output truncated at maxOutputBytesPerStream, a multi-document YAML
// stream, or an error message on stderr with nothing on stdout - is withheld rather than handed out, see
// withholdResponse.
//
// stderr is scrubbed by replacing the values that were redacted from the document, since kubectl formats an error
// with Go's %v verbs rather than as a document.

package kubectl

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

const (
	// redactedValue replaces secrets in the response. Deliberately differs from api/operator/common.RedactedValue
	// ("<redacted>"), since angle brackets would render as "\u003credacted\u003e" in JSON.
	redactedValue = "(redacted)"

	// minCredentialValueLength is the length of the shortest redacted value that is also scrubbed from stderr (see
	// valuesToScrubFromStderr). The values are removed from the document by position, so their length does not matter
	// there; stderr is scrubbed by replacing the value itself, where a short one would frequently match unrelated
	// output and garble it.
	minCredentialValueLength = 4

	// outputFormatJson and outputFormatYaml are the output formats that render the targeted resources as a document the
	// connector can parse and render itself, see parseableOutputFormat and redactSecretsInResponse.
	outputFormatJson = "json"
	outputFormatYaml = "yaml"

	// jsonIndent is the indentation kubectl uses for "-o json", so that a re-rendered document matches the original
	// output.
	jsonIndent = "    "
)

// wellKnownNonSecretValues lists values which are very unlikely to be secrets/credentials. They are used as a
// best-effort to not remove innocuous values from header and query parameter values (see
// redactHeaderValuesIfPlausible). Values are matched case-insensitively. Fields that always hold secrets (e.g. an
// export token) are always redacted, independent of the value.
var wellKnownNonSecretValues = map[string]struct{}{
	"true":                   {},
	"false":                  {},
	"null":                   {},
	"none":                   {},
	"unset":                  {},
	"undefined":              {},
	"empty":                  {},
	"auto":                   {},
	"default":                {},
	"enabled":                {},
	"disabled":               {},
	"gzip":                   {},
	"deflate":                {},
	"identity":               {},
	"chunked":                {},
	"close":                  {},
	"keep-alive":             {},
	"no-cache":               {},
	"utf-8":                  {},
	"*/*":                    {},
	"text/plain":             {},
	"application/json":       {},
	"application/grpc":       {},
	"application/x-protobuf": {},
}

// dash0ResourceTypesWithSecrets lists the resource type names of the Dash0 custom resources whose content can contain
// secrets: the Dash0 auth token and the header values of non-Dash0 exports (Dash0OperatorConfiguration,
// Dash0Monitoring), the credentials of the third-party integrations of a notification channel
// (Dash0NotificationChannel), and the credentials a synthetic check sends with its request (Dash0SyntheticCheck).
// Singular and plural form are listed; none of these custom resources have short names, and kubectl also accepts the
// kind (e.g. "Dash0Monitoring"), which normalizes to the singular form.
var dash0ResourceTypesWithSecrets = map[string]struct{}{
	"dash0operatorconfiguration":  {},
	"dash0operatorconfigurations": {},
	"dash0monitoring":             {},
	"dash0monitorings":            {},
	"dash0notificationchannel":    {},
	"dash0notificationchannels":   {},
	"dash0syntheticcheck":         {},
	"dash0syntheticchecks":        {},
}

// credentialFieldsPerConfigObject maps the name of a configuration object in a Dash0 custom resource to the fields
// within it that hold a credential. Keying on the enclosing object rather than on the field name alone keeps the
// generic field names ("url", "key") from matching unrelated values, e.g. the attribute keys of the notification
// routing filters or the URL of a synthetic check request. The webhook URLs are credentials themselves: they contain
// an unguessable token that grants the right to post to the channel.
//
// A field listed here is redacted unconditionally, unlike the generic header and query parameter values, which are only
// redacted when they do not look like a well-known non-secret value (see redactHeaderValueIfPlausible). That is why
// incidentioConfig.headers is listed even though "headers" is redacted everywhere anyway: it is not a position that
// happens to hold a credential, it is the Incident.io authorization header value and therefore always one.
var credentialFieldsPerConfigObject = map[string][]string{
	// Dash0NotificationChannel, spec.<type>Config
	"slackConfig":             {"webhookURL"},
	"webhookConfig":           {"url"},
	"incidentioConfig":        {"url", "headers"},
	"opsgenieConfig":          {"apiKey"},
	"pagerdutyConfig":         {"key"},
	"teamsWebhookConfig":      {"url"},
	"discordWebhookConfig":    {"url"},
	"googleChatWebhookConfig": {"url"},
	"ilertConfig":             {"url"},
	"allQuietConfig":          {"url"},
}

// parseableOutputFormats are the output formats whose response the connector can parse itself.
var parseableOutputFormats = map[string]struct{}{
	outputFormatJson: {},
	outputFormatYaml: {},
}

// redactSecretsInResponse redacts secrets in a command response, in place. The response is parsed into a document, the
// credential values (Dash0 auth tokens, third-party credentials) are replaced within that document - including in the
// copy of the spec that kubectl apply leaves behind in the "kubectl.kubernetes.io/last-applied-configuration"
// annotation - and the document is rendered again in the format the request asked for.
//
// Only responses that render a Dash0 custom resource which can contain secrets are redacted; for those, validation.go
// has already restricted the request to an output format the connector can parse and render (see
// safeOrRedactableOutputFormats).
//
// A non-nil error means the response could not be redacted and must not be sent to the backend, see withholdResponse.
func redactSecretsInResponse(parsed parsedArguments, resp *pb.CommandResponse, stdoutTruncated bool) error {
	// If there is no output at all, there is nothing to redact.
	if resp.GetStdout() == "" && resp.GetStderr() == "" {
		return nil
	}
	if !responseCanContainSecrets(parsed) {
		// The response either does not target a resource type that can contain secrets, or it uses an output format that
		// does not render details (e.g. default table, name, wide). No redaction is required.
		return nil
	}

	format, parseable := parsed.parseableOutputFormat()
	if !parseable {
		// Reached when the request sets the output format more than once ("-o yaml -o json"): each occurrence names a
		// redactable format, so validation accepts it, but parseableOutputFormat does not replicate kubectl's precedence
		// rules to decide which one was applied. A single format other than json/yaml cannot get here, since the only
		// ones left for these resource types are the content-free ones, which responseCanContainSecrets already ruled
		// out.
		return fmt.Errorf("the output format of this command cannot be parsed for redaction")
	}
	if stdoutTruncated {
		return fmt.Errorf(
			"the output exceeds the limit of %d bytes and the truncated response cannot be parsed for redaction",
			maxOutputBytesPerStream,
		)
	}
	if resp.GetStdout() == "" {
		// Only stderr carries content, which is not a document (kubectl formats an error message, not a resource).
		return fmt.Errorf("the command produced no output that could be parsed for redaction")
	}
	document, parsedSuccessfully := parseResponseDocument(format, resp.GetStdout())
	if !parsedSuccessfully {
		return fmt.Errorf("the response is not a single %s document that could be parsed for redaction", format)
	}

	redacted := &redactor{values: make(map[string]struct{})}
	if err := redactResourceList(document, redacted); err != nil {
		return err
	}

	stdout, err := renderResponseDocument(format, document)
	if err != nil {
		return fmt.Errorf("the redacted response could not be rendered as %s: %w", format, err)
	}
	resp.Stdout = stdout
	// kubectl does not print resource content to stderr, but scrubbing the redacted values from it as well is cheap.
	resp.Stderr = redactAllSecrets(resp.GetStderr(), redacted.valuesToScrubFromStderr())
	return nil
}

// responseCanContainSecrets reports whether the response of the given invocation renders the content of a Dash0 custom
// resource that can contain secrets, and therefore has to be redacted.
func responseCanContainSecrets(parsed parsedArguments) bool {
	if parsed.subcommand != "get" {
		// No other allowed subcommand renders the content of a custom resource: "describe" is rejected for these
		// resource types (see describeOfResourceTypeWithSecrets), and "explain" only prints the schema.
		return false
	}
	if parsed.outputIsContentFree() {
		// kubectl get -o name or similar, no actual resource content in the response.
		return false
	}
	return targetsResourceTypeWithSecrets(parsed)
}

// parseResponseDocument parses a kubectl response that renders resources in the given output format. It reports false
// when the response cannot be parsed, or when it cannot be guaranteed that the parsed result covers the whole response.
// YAML is converted to JSON before it is unmarshalled, so that both formats yield the same types and are walked by
// exactly the same code (see redactDocumentNodeRecursively).
func parseResponseDocument(format string, stdout string) (any, bool) {
	var document any
	switch format {
	case outputFormatJson:
		if err := json.Unmarshal([]byte(stdout), &document); err != nil {
			return nil, false
		}
	case outputFormatYaml:
		if hasYamlDocumentSeparator(stdout) {
			// "kubectl get -o yaml" can render a list of requested resources as one multi-document stream separated by ---.
			// Unmarshalling only covers the first document, so anything that looks like a multi-document response is not
			// parsed here. Removing that guard would not leak secrets, but return a partial response (only the first doc
			// would be returned), without any indication what has gone wrong. This is defense in depth, it is practically
			// unreachable for validated requests. kubectl's prints multiple objects into one v1.List for -o yaml/-o json.
			return nil, false
		}
		if err := yaml.Unmarshal([]byte(stdout), &document); err != nil {
			return nil, false
		}
	default:
		return nil, false
	}
	return document, true
}

// hasYamlDocumentSeparator reports whether the output contains a line that starts a new YAML document. A separator
// within a block scalar is reported as well; that is a false positive in the safe direction.
func hasYamlDocumentSeparator(stdout string) bool {
	for line := range strings.SplitSeq(stdout, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "---" || strings.HasPrefix(trimmed, "--- ") {
			return true
		}
	}
	return false
}

// redactor replaces the credential values of a document with redactedValue and collects the values it replaced. The
// count is tracked separately from the set of values, so that a caller can tell whether a node changed even when it
// held a value that had already been replaced elsewhere (see redactAnnotations).
type redactor struct {
	values map[string]struct{}
	count  int
}

func (r *redactor) add(value string) {
	r.values[value] = struct{}{}
	r.count++
}

// valuesToScrubFromStderr returns the redacted values that are worth replacing in stderr as well, ordered from longest
// to shortest so that a longer value is replaced before a shorter one contained in it. Values shorter than
// minCredentialValueLength are left out: they reveal little, but would frequently match unrelated output and garble
// the error message. The value itself is matched here, which is reliable because kubectl formats an error message with
// Go's %v verbs rather than as a document, so no escaping is applied to it.
func (r *redactor) valuesToScrubFromStderr() []string {
	sorted := make([]string, 0, len(r.values))
	for value := range r.values {
		if len(value) >= minCredentialValueLength {
			sorted = append(sorted, value)
		}
	}
	slices.SortFunc(sorted, func(first, second string) int {
		if diff := len(second) - len(first); diff != 0 {
			return diff
		}
		return strings.Compare(first, second)
	})
	return sorted
}

// targetsResourceTypeWithSecrets reports whether the kubectl arguments reference a Dash0 custom resource type whose
// content can contain secrets (see dash0ResourceTypesWithSecrets). Unlike responseCanContainSecrets it does not look at
// the subcommand or the output format, since it answers whether a response could contain a secret at all, not whether
// the response has to be redacted. It is the basis for rejecting the output formats whose rendering of a secret cannot
// be redacted reliably, see unredactableOutputRequested in validation.go.
func targetsResourceTypeWithSecrets(parsed parsedArguments) bool {
	for _, resourceType := range parsed.resourceTypes {
		if _, hasSecrets := dash0ResourceTypesWithSecrets[resourceType]; hasSecrets {
			return true
		}
	}
	return false
}

// redactResourceList redacts the secrets of all resources in a parsed resource document, in place. Such a document
// usually is a list, but a document that is not a list is treated as a single resource. The whole document is walked,
// including resources of other types that the same command rendered (as in
// "kubectl get pods,dash0monitorings -o json"): a credential-like field of such a resource is redacted as well,
// which errs towards redacting too much rather than too little.
func redactResourceList(document any, redacted *redactor) error {
	documentMap, isMap := document.(map[string]any)
	if !isMap {
		// "kubectl get -o json/yaml" renders a single resource as an object and several as a v1.List, so the root of the
		// document is always a map. Anything else is a shape this code was not written for, and a shape it cannot redact:
		// returning without an error here would hand the document out unredacted, so it fails closed instead.
		return fmt.Errorf("the response is not a resource document that could be parsed for redaction")
	}
	items, isList := documentMap["items"].([]any)
	if !isList {
		return redactResourceItem(document, redacted)
	}
	for _, item := range items {
		if err := redactResourceItem(item, redacted); err != nil {
			return err
		}
	}
	return nil
}

// redactResourceItem redacts the secrets of a single resource, both in its own content and in the copies of it that
// tools embed in its annotations.
func redactResourceItem(resource any, redacted *redactor) error {
	redactDocumentNodeRecursively(resource, redacted)
	return redactAnnotations(resource, redacted)
}

// redactDocumentNodeRecursively recursively walks the content of a resource and replaces every credential value it
// finds with redactedValue:
//   - the Dash0 auth token (all spec.exports.dash0.authorization.token and its legacy counterpart
//     spec.export.dash0.authorization.token) and the basic authentication password of a synthetic check,
//   - the literal header values of the gRPC and HTTP exports, of the webhook notification channels, and of the request
//     of a synthetic check, as well as its query parameter values,
//   - the credentials of the third-party integration of a notification channel (see credentialFieldsPerConfigObject).
//
// The user name of the basic authentication of a synthetic check is not a credential and is left in place. Values
// sourced via valueFrom are ignored. Apart from the fields that are only credentials within a particular
// configuration object, the walk is not bound to specific paths, so it covers any future location of these fields as
// well. It is applied to the resources themselves as well as to the copies of them that are embedded in annotations
// (see redactResourceItem).
func redactDocumentNodeRecursively(node any, redacted *redactor) {
	switch typedNode := node.(type) {
	case map[string]any:
		// Only the values of existing keys are replaced, never new ones added, so the map may be modified while it is
		// ranged over.
		for key, value := range typedNode {
			switch key {
			case "token", "password":
				redactValueOf(typedNode, key, redacted)
			case "headers", "queryParameters":
				redactHeaderValues(typedNode, key, redacted)
			default:
				if credentialFields, hasCredentials := credentialFieldsPerConfigObject[key]; hasCredentials {
					redactCredentialFields(value, credentialFields, redacted)
				}
			}
			redactDocumentNodeRecursively(value, redacted)
		}
	case []any:
		// recursively redact nested objects
		for _, item := range typedNode {
			redactDocumentNodeRecursively(item, redacted)
		}
	}
}

// redactValueOf replaces the value the given key holds in node with redactedValue and records the
// replaced value, so that it can be scrubbed from stderr as well. Non-string and empty values are left alone; a
// value sourced via valueFrom is an object rather than a string and is therefore not a credential the response
// exposes. No length or plausibility check is applied: the value is replaced where it lives, so an unusually short
// credential cannot affect anything else in the response.
//
// A value that already is the placeholder is left alone as well. Two rules can cover the same field - a credential
// field of a configuration object that is also a header, or a header whose name happens to be "token" - and replacing
// it twice would add the placeholder itself to the recorded values and count as another replacement, which
// redactAnnotations reads as "this annotation held a credential".
func redactValueOf(node map[string]any, key string, redacted *redactor) {
	value, isString := node[key].(string)
	if !isString || value == "" || value == redactedValue {
		return
	}
	node[key] = redactedValue
	redacted.add(value)
}

// redactHeaderValues redacts the literal header or query parameter values held by the given key of node. Three shapes
// occur in the Dash0 custom resources: the list of name/value pairs of the exports and of the request of a synthetic
// check, the map of the generic webhook notification channel, and the single header value of the Incident.io
// notification channel, which is why the enclosing node is passed rather than the value itself.
func redactHeaderValues(node map[string]any, key string, redacted *redactor) {
	switch typedValue := node[key].(type) {
	case []any:
		for _, header := range typedValue {
			headerMap, isMap := header.(map[string]any)
			if !isMap {
				continue
			}
			redactHeaderValueIfPlausible(headerMap, "value", redacted)
		}
	case map[string]any:
		for name := range typedValue {
			redactHeaderValueIfPlausible(typedValue, name, redacted)
		}
	case string:
		redactHeaderValueIfPlausible(node, key, redacted)
	}
}

// redactHeaderValueIfPlausible redacts a header or query parameter value, unless it is a well-known non-secret value.
// Unlike the fields that are credentials by definition (a token, a password, the credential of a notification
// channel), a header is a position that also carries values which are not credentials at all - a content type, an
// encoding - and rendering those as the placeholder would hide harmless information from the reader without protecting
// anything.
func redactHeaderValueIfPlausible(node map[string]any, key string, redacted *redactor) {
	value, isString := node[key].(string)
	if !isString {
		return
	}
	if _, isWellKnown := wellKnownNonSecretValues[strings.ToLower(value)]; isWellKnown {
		return
	}
	redactValueOf(node, key, redacted)
}

// redactCredentialFields redacts the given fields of a configuration object, see credentialFieldsPerConfigObject.
func redactCredentialFields(node any, credentialFields []string, redacted *redactor) {
	configObject, isMap := node.(map[string]any)
	if !isMap {
		return
	}
	for _, field := range credentialFields {
		redactValueOf(configObject, field, redacted)
	}
}

// redactAnnotations redacts the secrets in the resource copies that tools embed in the metadata.annotations of a
// resource. kubectl apply stores a verbatim copy of the applied manifest - including the plaintext auth token,
// potentially an older one than the one in the current spec - in the
// "kubectl.kubernetes.io/last-applied-configuration" annotation. Every annotation value that parses as JSON is walked,
// so that equivalent annotations of other tools are covered as well, and is rendered again only when the walk actually
// replaced something, so that an unrelated annotation is handed out exactly as kubectl rendered it.
func redactAnnotations(resource any, redacted *redactor) error {
	resourceMap, isMap := resource.(map[string]any)
	if !isMap {
		return nil
	}
	metadata, isMap := resourceMap["metadata"].(map[string]any)
	if !isMap {
		return nil
	}
	annotations, isMap := metadata["annotations"].(map[string]any)
	if !isMap {
		return nil
	}
	for name, annotationValue := range annotations {
		annotationString, isString := annotationValue.(string)
		if !isString {
			continue
		}
		var embeddedResource any
		if err := json.Unmarshal([]byte(annotationString), &embeddedResource); err != nil {
			continue
		}
		countBefore := redacted.count
		redactDocumentNodeRecursively(embeddedResource, redacted)
		if redacted.count == countBefore {
			continue
		}
		rendered, err := json.Marshal(embeddedResource)
		if err != nil {
			// The annotation still holds the credential in plaintext, so the response must not be handed out.
			return fmt.Errorf("the redacted %q annotation could not be rendered: %w", name, err)
		}
		annotations[name] = string(rendered)
	}
	return nil
}

// redactAllSecrets replaces every occurrence of every secret in text with redactedValue.
func redactAllSecrets(text string, secrets []string) string {
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, redactedValue)
	}
	return text
}

// renderResponseDocument renders the redacted document in the output format the request asked for. It tries to emulate
// the output format of kubectl as best as possible. kubectl serializes a custom resource from its unstructured map
// form, with alphabetically ordered keys, four-space indentation and a trailing newline for JSON - exactly what
// encoding/json and sigs.k8s.io/yaml produce for the same document. So the rendered response should match kubectl's
// output byte for byte, apart from the redacted values. Built-in resource types that the same command rendered
// (as in "kubectl get pods,dash0monitorings -o json") are serialized from a Go struct
// by kubectl and therefore come back with their fields ordered alphabetically rather than in the order kubectl used;
// the document is equivalent, only its field order differs.
func renderResponseDocument(format string, document any) (string, error) {
	switch format {
	case outputFormatJson:
		rendered, err := json.MarshalIndent(document, "", jsonIndent)
		if err != nil {
			return "", err
		}
		return string(rendered) + "\n", nil
	case outputFormatYaml:
		rendered, err := yaml.Marshal(document)
		if err != nil {
			return "", err
		}
		return string(rendered), nil
	default:
		return "", fmt.Errorf("unsupported output format %q", format)
	}
}

// withholdResponse discards the output of a command whose response could not be redacted and replaces it with the
// reason, so that a response that might contain a Dash0 auth token or a third-party credential is never sent to the
// backend.
func withholdResponse(resp *pb.CommandResponse, reason error) {
	resp.Stdout = ""
	resp.Stderr = fmt.Sprintf(
		"dash0 agent0-connector withheld the response of this command because the secrets it might contain "+
			"could not be redacted: %s",
		reason,
	)
	resp.ExitCode = exitCodeRejected
}
