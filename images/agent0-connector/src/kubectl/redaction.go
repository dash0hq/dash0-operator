// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Handles secret redaction for the output of kubectl commands
//
// Redacting secrets from kubectl responses is not exactly trivial for a couple of reasons:
//
// - "kubectl get" supports a variety of output formats (-o xxx), not all of them can be reliably parsed and
//   interpreted. "go-template"/"go-template-file", "jsonpath"/"jsonpath-as-json"/"jsonpath-file" as well as
//   "custom-columns"/"custom-columns-file" can basically reshape the response arbitrarily; this makes reliably finding
//   the export tokens or export headers in such a response is basically impossible (or would require parsing and
//   interpreting the template/jsonpath/colum definitions as well)
// - "kubectl describe" output is somewhat structured, but not meant to be parsed, and there seem to no parsers
//   available for its format. Hand-rolling a parser might be possible, but potentially error-prone.
//
// Taking all of this into account, the approach to secret redaction is as follows:
// When a kubectl command involves CRDs for which we want to redact secrets, and if the command uses an output format
// that requires secret redaction (e.g. not a simple "kubectl get name" etc.):
// 1. gather the relevant secret values as strings, either
//     - from the response itself, when the command rendered the resources as a JSON or YAML document
//       (e.g. -ojson/-oyaml), or
//     - from the response of an additional kubectl command for the same resource types and with the same namespace
//       scope, using an output format that we can easily parse,
// 2. do a find-and-replace for these strings in the actual kubectl response that is sent back to the server

package kubectl

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

const (
	// maxExtractionOutputBytes caps how many bytes are captured from the kubectl invocation that re-reads the Dash0
	// custom resources of the requested namespace scope to learn which values have to be redacted (see
	// gatherRedactableSecretsViaAdditionalKubectlCall). The limit is higher than maxOutputBytesPerStream because this
	// output is never sent to the backend, it is only parsed to collect secret values; truncating it would silently drop
	// values that then would not be redacted, so a truncated extraction is treated as a failure.
	maxExtractionOutputBytes = 8 * 1024 * 1024 // 8 MiB

	// redactedValue replaces secrets in the response. It is the same placeholder the operator uses when it logs a Dash0
	// custom resource (see api/operator/common.RedactedValue).
	redactedValue = "<redacted>"

	// minCredentialValueLength is the length of the shortest value that is treated as a potential secret (see
	// addSecretIfPlausible). Shorter values are no plausible credentials, but would frequently match unrelated
	// parts of the response and garble it.
	minCredentialValueLength = 4

	// minTruncatedSecretFragmentLength is the length of the shortest fragment of a secret value that is stripped from
	// the end of truncated output (see trimTruncatedSecretFragment). Shorter fragments are left alone: they reveal
	// nothing, but would frequently match the last characters of unrelated output.
	minTruncatedSecretFragmentLength = 5

	// outputFormatJson and outputFormatYaml are the output formats that render the targeted resources as a document the
	// connector can parse itself, see parseableOutputFormat and secretsFromResponse.
	outputFormatJson = "json"
	outputFormatYaml = "yaml"
)

// contentFreeOutputFormats are the output formats that do not require secret redaction, i.e. that allow listing
// resources or checking for the presence of a particular one without exposing their content. Any other output format
// (yaml, json, jsonpath, go-template, custom-columns, ...) can serialize the content.
// Such a request
// * is outright rejected in validation.go when it targets a sensitive resource (e.g. a Kubernetes secret)
// * might get parts of its response (secrets, auth tokens, credentials etc.) redacted
var contentFreeOutputFormats = map[string]struct{}{
	"":     {}, // the default, human-readable table output
	"name": {},
	"wide": {},
}

// wellKnownNonSecretValues lists values which are very unlikely to be secrets/credentials. Values are matched
// case-insensitively (see addSecretIfPlausible).
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

// dash0ResourceTypesWithSecrets maps the resource type names of the Dash0 custom resources whose content can contain
// secrets to the fully qualified name under which they are re-read for redaction: the Dash0 auth token and the header
// values of non-Dash0 exports (Dash0OperatorConfiguration, Dash0Monitoring), the credentials of the third-party
// integrations of a notification channel (Dash0NotificationChannel), and the credentials a synthetic check sends with
// its request (Dash0SyntheticCheck). Singular and plural form are listed; none of these custom resources have short
// names, and kubectl also accepts the kind (e.g. "Dash0Monitoring"), which normalizes to the singular form.
var dash0ResourceTypesWithSecrets = map[string]string{
	"dash0operatorconfiguration":  "dash0operatorconfigurations.operator.dash0.com",
	"dash0operatorconfigurations": "dash0operatorconfigurations.operator.dash0.com",
	"dash0monitoring":             "dash0monitorings.operator.dash0.com",
	"dash0monitorings":            "dash0monitorings.operator.dash0.com",
	"dash0notificationchannel":    "dash0notificationchannels.operator.dash0.com",
	"dash0notificationchannels":   "dash0notificationchannels.operator.dash0.com",
	"dash0syntheticcheck":         "dash0syntheticchecks.operator.dash0.com",
	"dash0syntheticchecks":        "dash0syntheticchecks.operator.dash0.com",
}

// credentialFieldsPerConfigObject maps the name of a configuration object in a Dash0 custom resource to the fields
// within it that hold a credential. Keying on the enclosing object rather than on the field name alone keeps the
// generic field names ("url", "key") from matching unrelated values, e.g. the attribute keys of the notification
// routing filters or the URL of a synthetic check request. The webhook URLs are credentials themselves: they contain
// an unguessable token that grants the right to post to the channel.
var credentialFieldsPerConfigObject = map[string][]string{
	// Dash0NotificationChannel, spec.<type>Config
	"slackConfig":             {"webhookURL"},
	"webhookConfig":           {"url"},
	"incidentioConfig":        {"url"},
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

// redactSecretsInResponse redacs secrets in a command response, in place. The values (Dash0 auth tokens,
// third-party credentials) are redacted wherever they occur, no matter how kubectl rendered the resource (`describe`,
// `-o yaml`, `-o json`, `-o jsonpath`, `-o go-template`, ..., including the copy of the spec that kubectl apply leaves
// behind in the "kubectl.kubernetes.io/last-applied-configuration" annotation).
//
// redactSecretsInResponse only redacts secrets from Dash0-related resource types, for which we know the structure
// and format, and in which places they might contain secrets.
//
// The values that need to be redacted are determined from the response itself (see secretsFromResponse) or, when it
// uses a format that cannot be parsed and interpreted reliably (e.g. kubectl describe, kubectl get -o jsonpath), by
// re-reading the targeted resource types from the cluster (see gatherRedactableSecretsViaAdditionalKubectlCall), so
// responses are matched on the actual secret values rather than on a particular serialization format. A non-nil error
// means the response could not be redacted and must not be sent to the backend, see withholdResponse.
func redactSecretsInResponse(
	ctx context.Context,
	kubectlTmpDir string,
	parsed parsedArguments,
	resp *pb.CommandResponse,
	stdoutTruncated bool,
) error {
	// If there is no output at all, there is nothing to redact.
	if resp.GetStdout() == "" && resp.GetStderr() == "" {
		return nil
	}
	resourceTypes := extractResourceTypesThatRequireSecretRedaction(parsed)
	if len(resourceTypes) == 0 {
		// None of the resource types are subject to secret redaction.
		return nil
	}

	secrets, fromResponse := secretsFromResponse(parsed, resp.GetStdout(), stdoutTruncated)
	if !fromResponse {
		var err error
		if secrets, err = gatherRedactableSecretsViaAdditionalKubectlCall(
			ctx,
			kubectlTmpDir,
			parsed,
			resourceTypes,
		); err != nil {
			return err
		}
	}
	if len(secrets) == 0 {
		return nil
	}

	resp.Stdout = redactAllSecrets(resp.GetStdout(), secrets)
	if stdoutTruncated {
		resp.Stdout = trimTruncatedSecretFragment(resp.GetStdout(), secrets)
	}
	// kubectl does not print resource content to stderr, but redacting it as well is free at this point.
	resp.Stderr = redactAllSecrets(resp.GetStderr(), secrets)
	return nil
}

// extractResourceTypesThatRequireSecretRedaction inspects the kubectl arguments and returns the fully qualified names
// of the custom resource types that satisfy the following conditions:
//   - it is a resource type which can potentially include sensitive information (a Dash0 auth token in an export,
//     third party credentials in non-Dash0 export configurations, in the integration of a notification channel, or in
//     the request of a synthetic check),
//   - the kubectl command asks for the actual content of the resource (e.g. kubectl describe, or
//     kubectl get with -oyaml etc.),
//
// The resource names are returned without duplicates. The method returns an empty slice for requests that do not match
// the conditions above.
func extractResourceTypesThatRequireSecretRedaction(parsed parsedArguments) []string {
	switch parsed.subcommand {
	case "describe":
		// The describer always prints the whole resource, regardless of the output format.
	case "get":
		if parsed.outputIsContentFree() {
			// kubectl get -o name or similar, no actual resource content in the response, hence no need to redact secrets
			return nil
		}
		// All other "kubectl get" formats require secret redaction.
	default:
		// No other allowed subcommand renders the content of a custom resource ("explain" only prints its schema).
		return nil
	}

	var resourceTypes []string
	for _, resourceType := range parsed.resourceTypes {
		qualifiedName, hasSecrets := dash0ResourceTypesWithSecrets[resourceType]
		if hasSecrets && !slices.Contains(resourceTypes, qualifiedName) {
			resourceTypes = append(resourceTypes, qualifiedName)
		}
	}
	return resourceTypes
}

// targetsResourceTypeWithSecrets reports whether the kubectl arguments reference a Dash0 custom resource type whose
// content can contain secrets (see dash0ResourceTypesWithSecrets). Unlike
// extractResourceTypesThatRequireSecretRedaction it does not look at the subcommand or the output format, since it
// answers whether a response could contain a secret at all, not whether the response has to be redacted. It is the
// basis for rejecting the output formats whose rendering of a secret cannot be redacted reliably, see
// unredactableOutputRequested in validation.go.
func targetsResourceTypeWithSecrets(parsed parsedArguments) bool {
	for _, resourceType := range parsed.resourceTypes {
		if _, hasSecrets := dash0ResourceTypesWithSecrets[resourceType]; hasSecrets {
			return true
		}
	}
	return false
}

// secretsFromResponse returns the secret values that occur in the response itself, sorted from longest to shortest (see
// redactAllSecrets). This is the fast-path to be used when the original response can be used to identify secrets
// reliably.
//
// The second return value is false when the response is not a document that can be parsed with confidence
// (e.g. for kubectl get -o jsonpath) the caller then has to fall back to re-reading the resources via
// gatherRedactableSecretsViaAdditionalKubectlCall.
//
// The whole document is walked, including resources of other types that the same command rendered (as in
// "kubectl get pods,dash0monitorings -o json"): a secret value of such a resource is redacted as well, which errs
// towards redacting too much rather than too little.
func secretsFromResponse(parsed parsedArguments, stdout string, stdoutTruncated bool) ([]string, bool) {
	if stdout == "" {
		// Nothing can be learned from an empty stdout, and it must not be mistaken for a document that was parsed
		// successfully: an empty string is a valid (empty) YAML document, which would yield an empty secret list and
		// thereby skip the redaction of a stderr that does carry resource content.
		return nil, false
	}
	if stdoutTruncated {
		// A truncated document does not parse, and trimming the fragment of a secret at the very end of the output
		// requires the full value anyway (see trimTruncatedSecretFragment).
		return nil, false
	}
	format, parseable := parsed.parseableOutputFormat()
	if !parseable {
		return nil, false
	}
	document, parsedSuccessfully := parseResponseDocument(format, stdout)
	if !parsedSuccessfully {
		return nil, false
	}

	secrets := make(map[string]struct{})
	collectSecretsFromResourceList(document, secrets)
	return sortedSecrets(secrets), true
}

// parseResponseDocument parses a kubectl response that renders resources in the given output format. It reports false
// when the response cannot be parsed, or when it cannot be guaranteed that the parsed result covers the whole response.
// YAML is converted to JSON before it is unmarshalled, so that both formats yield the same types and are walked by
// exactly the same code (see collectSecretsFromDocumentNodeRecursively).
func parseResponseDocument(format string, stdout string) (any, bool) {
	var document any
	switch format {
	case outputFormatJson:
		if err := json.Unmarshal([]byte(stdout), &document); err != nil {
			return nil, false
		}
	case outputFormatYaml:
		if hasYamlDocumentSeparator(stdout) {
			// "kubectl get -o yaml" renders even a list of resources as a single document, and unmarshalling only covers
			// the first document of a stream, so anything that looks like a multi-document response is not parsed here.
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
// within a block scalar is reported as well; that is a false positive in the safe direction, since it only means that
// the values are read from the cluster instead of from the response.
func hasYamlDocumentSeparator(stdout string) bool {
	for line := range strings.SplitSeq(stdout, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "---" || strings.HasPrefix(trimmed, "--- ") {
			return true
		}
	}
	return false
}

// gatherRedactableSecretsViaAdditionalKubectlCall reads the custom resources with the given CRD type and collects
// secret values they contain, sorted from longest to shortest (see redactAllSecrets). It returns an error if the
// resources cannot be read or parsed; the caller must then reject the kubectl command in that case.
func gatherRedactableSecretsViaAdditionalKubectlCall(
	ctx context.Context,
	kubectlTmpDir string,
	parsed parsedArguments,
	resourceTypes []string,
) ([]string, error) {
	extractionCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	arguments := extractionArguments(parsed, resourceTypes)
	stdout, stderr, err := runKubectl(extractionCtx, kubectlTmpDir, arguments, maxExtractionOutputBytes)
	if err != nil {
		return nil, fmt.Errorf(
			"reading the resources via \"%s %s\" failed: %w: %s",
			kubectlCommand,
			strings.Join(arguments, " "),
			err,
			strings.TrimSpace(stderr.String()),
		)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("the resources exceed the limit of %d bytes", maxExtractionOutputBytes)
	}

	var document any
	if err = json.Unmarshal([]byte(stdout.String()), &document); err != nil {
		return nil, fmt.Errorf("cannot parse the Dash0 resources: %w", err)
	}

	secrets := make(map[string]struct{})
	collectSecretsFromResourceList(document, secrets)
	return sortedSecrets(secrets), nil
}

// extractionArguments returns the arguments of the kubectl invocation that re-reads the given resource types to learn
// which values have to be redacted. The invocation is given the same namespace scope as the command whose response is
// redacted (see namespaceScopeArguments).
func extractionArguments(parsed parsedArguments, resourceTypes []string) []string {
	namespaceScope := parsed.namespaceScopeArguments()
	arguments := make([]string, 0, 4+len(namespaceScope))
	arguments = append(arguments, "get", strings.Join(resourceTypes, ","))
	arguments = append(arguments, namespaceScope...)
	return append(arguments, "--output", outputFormatJson)
}

// collectSecretsFromResourceList adds the secrets of all resources in a parsed resource document - the response of the
// command itself or the one of the invocation that re-reads the resources - to secrets. Such a document usually is a
// list, but a document that is not a list is treated as a single resource.
func collectSecretsFromResourceList(document any, secrets map[string]struct{}) {
	documentMap, isMap := document.(map[string]any)
	if !isMap {
		return
	}
	items, isList := documentMap["items"].([]any)
	if !isList {
		collectSecretsFromResourceItem(document, secrets)
		return
	}
	for _, item := range items {
		collectSecretsFromResourceItem(item, secrets)
	}
}

// collectSecretsFromResourceItem adds the secrets of a single Dash0 custom resource to secrets, both from its own
// content and from the copies of it that tools embed in its annotations.
func collectSecretsFromResourceItem(resource any, secrets map[string]struct{}) {
	collectSecretsFromDocumentNodeRecursively(resource, secrets)
	collectSecretsFromAnnotations(resource, secrets)
}

// collectSecretsFromAnnotations adds the secrets of the resource copies that tools embed in the metadata.annotations of
// a resource to secrets. kubectl apply stores a verbatim copy of the applied manifest - including the plaintext auth
// token, potentially an older one than the one in the current spec - in the
// "kubectl.kubernetes.io/last-applied-configuration" annotation. Every annotation value that parses as JSON is walked,
// so that equivalent annotations of other tools are covered as well.
func collectSecretsFromAnnotations(resource any, secrets map[string]struct{}) {
	resourceMap, isMap := resource.(map[string]any)
	if !isMap {
		return
	}
	metadata, isMap := resourceMap["metadata"].(map[string]any)
	if !isMap {
		return
	}
	annotations, isMap := metadata["annotations"].(map[string]any)
	if !isMap {
		return
	}
	for _, annotationValue := range annotations {
		annotationString, isString := annotationValue.(string)
		if !isString {
			continue
		}
		var embeddedResource any
		if err := json.Unmarshal([]byte(annotationString), &embeddedResource); err != nil {
			continue
		}
		collectSecretsFromDocumentNodeRecursively(embeddedResource, secrets)
	}
}

// collectSecretsFromDocumentNodeRecursively recursively walks the content of a Dash0 custom resource and adds every
// secret value it finds to secrets:
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
// (see collectSecretsFromResourceItem).
func collectSecretsFromDocumentNodeRecursively(node any, secrets map[string]struct{}) {
	switch typedNode := node.(type) {
	case map[string]any:
		for key, value := range typedNode {
			switch key {
			case "token", "password":
				addSecret(value, secrets)
			case "headers", "queryParameters":
				collectHeaderValues(value, secrets)
			default:
				if credentialFields, hasCredentials := credentialFieldsPerConfigObject[key]; hasCredentials {
					collectCredentialFields(value, credentialFields, secrets)
				}
			}
			collectSecretsFromDocumentNodeRecursively(value, secrets)
		}
	case []any:
		// recursively collect secrets from nested objects
		for _, item := range typedNode {
			collectSecretsFromDocumentNodeRecursively(item, secrets)
		}
	}
}

// collectHeaderValues adds the literal header or query parameter values in node to secrets. Three shapes occur in the
// Dash0 custom resources: the list of name/value pairs of the exports and of the request of a synthetic check, the map
// of the generic webhook notification channel, and the single header value of the Incident.io notification channel.
func collectHeaderValues(node any, secrets map[string]struct{}) {
	switch typedNode := node.(type) {
	case []any:
		for _, header := range typedNode {
			headerMap, isMap := header.(map[string]any)
			if !isMap {
				continue
			}
			addSecretIfPlausible(headerMap["value"], secrets)
		}
	case map[string]any:
		for _, value := range typedNode {
			addSecretIfPlausible(value, secrets)
		}
	case string:
		addSecretIfPlausible(typedNode, secrets)
	}
}

// collectCredentialFields adds the values of the given fields of a configuration object to secrets, see
// credentialFieldsPerConfigObject.
func collectCredentialFields(node any, credentialFields []string, secrets map[string]struct{}) {
	configObject, isMap := node.(map[string]any)
	if !isMap {
		return
	}
	for _, field := range credentialFields {
		addSecretIfPlausible(configObject[field], secrets)
	}
}

// addSecretIfPlausible adds value to secrets, unless it is shorter than minCredentialValueLength or a well-known
// non-secret value.
func addSecretIfPlausible(value any, secrets map[string]struct{}) {
	secret, isString := value.(string)
	if !isString || len(secret) < minCredentialValueLength {
		return
	}
	if _, isWellKnown := wellKnownNonSecretValues[strings.ToLower(secret)]; isWellKnown {
		return
	}
	addSecret(secret, secrets)
}

// addSecret adds value to secrets if it is a non-empty string.
func addSecret(value any, secrets map[string]struct{}) {
	if secret, isString := value.(string); isString && secret != "" {
		secrets[secret] = struct{}{}
	}
}

// jsonEscapedVariant returns the value as encoding/json renders it within a JSON document, without the enclosing
// quotes, or "" when it is rendered verbatim. The secrets are collected from a parsed document (or from the parsed
// response of the invocation that re-reads the resources) and are therefore unescaped, while the response they are
// redacted from is still encoded: kubectl renders "&", "<" and ">" as &, < and >, and quotes,
// backslashes and control characters with their usual escape sequences. A credential containing any of them - a
// webhook URL with several query parameters, for instance - does not occur literally in a "-o json" response and would
// be handed out in full.
func jsonEscapedVariant(secret string) string {
	encoded, err := json.Marshal(secret)
	if err != nil {
		return ""
	}
	// json.Marshal encloses a string in quotes, which are part of the response but not of the value.
	escaped := string(encoded[1 : len(encoded)-1])
	if escaped == secret {
		return ""
	}
	return escaped
}

// sortedSecrets returns the collected secrets, plus the escaped rendering of every secret the JSON serializer does not
// render verbatim (see jsonEscapedVariant), ordered from longest to shortest, so that redactAllSecrets replaces a
// longer secret before a shorter one that is contained in it.
func sortedSecrets(secrets map[string]struct{}) []string {
	// Collected first, then added: adding to a map while ranging over it may or may not visit the new entries, which
	// would escape an already escaped variant a second time.
	var escapedVariants []string
	for secret := range secrets {
		if escaped := jsonEscapedVariant(secret); escaped != "" {
			escapedVariants = append(escapedVariants, escaped)
		}
	}
	for _, escaped := range escapedVariants {
		secrets[escaped] = struct{}{}
	}

	sorted := make([]string, 0, len(secrets))
	for secret := range secrets {
		sorted = append(sorted, secret)
	}
	slices.SortFunc(sorted, func(first, second string) int {
		if diff := len(second) - len(first); diff != 0 {
			return diff
		}
		return strings.Compare(first, second)
	})
	return sorted
}

// redactAllSecrets replaces every occurrence of every secret in text with redactedValue.
func redactAllSecrets(text string, secrets []string) string {
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, redactedValue)
	}
	return text
}

// trimTruncatedSecretFragment replaces a fragment of a secret at the very end of truncated output with redactedValue.
// Output that has been capped at maxOutputBytesPerStream can end in the middle of a secret value, which
// redactAllSecrets (matching whole values) would leave in place.
func trimTruncatedSecretFragment(text string, secrets []string) string {
	for _, secret := range secrets {
		for length := len(secret) - 1; length >= minTruncatedSecretFragmentLength; length-- {
			if strings.HasSuffix(text, secret[:length]) {
				return text[:len(text)-length] + redactedValue
			}
		}
	}
	return text
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
