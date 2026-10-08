// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Validation for kubectl command requests. Checks whether a command kubectl invocation is allowed or if it needs to be
// rejected.

package kubectl

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/client-go/util/jsonpath"
	kubectlcmd "k8s.io/kubectl/pkg/cmd"
	"k8s.io/kubectl/pkg/cmd/get"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

// supportedKubectlCommands is the allowlist of kubectl commands the executor can run at all. Everything else is
// rejected, independent of the command allowlist that has been configured. The list deliberately contains only kubectl
// commands that read cluster state and never mutate it. This is additional defense-in-depth on top of the read-only
// RBAC (get & list only) granted to the agent0-connector service account.
//
// The configuration can restrict this list further (see AllowedKubectlCommands). The default configuration blocks
// the "logs" and "events" command, these restrictions can be lifted by the user.
//
// It also (despite the name) lists rejected kubectl commands that need a more specific rejection message
// (e.g. kubectl describe)
//
// The exact shape of supported kubectl commands can be further restricted via allowedSubcommandsPerKubectlCommand
// (e.g. allowing "kubectl auth can-i", but not other "kubectl auth" subcommands).
//
// Maintenance note: The list of supported commands is duplicated in the template
// dash0-operator.agent0ConnectorAllowedKubectlCommands in helm-chart/dash0-operator/templates/_helpers.tpl and in
// operator.agent0Connector.allowedKubectlCommands in helm-chart/dash0-operator/values.yaml, see
// TestHelmChartListsEverySupportedKubectlCommand.
var supportedKubectlCommands = map[string]struct{}{
	"api-resources":    {},
	"auth":             {},
	"api-versions":     {},
	"cluster-info":     {},
	"describe":         {}, // describe is not actually supported, see unconditionallyRejectedKubectlCommands
	"events":           {},
	"explain":          {},
	kubectlCommandGet:  {},
	kubectlCommandLogs: {},
	"top":              {},
	"version":          {},
}

// unconditionallyRejectedKubectlCommands are kubectl commands that are listed in supportedKubectlCommands so that a
// later check can reject them with a more specific reason rather than with the generic "not an allowed read-only
// command". They can never be enabled via the configuration (see AllowedKubectlCommands). They are also not included in
// the list of allowed commands that rejection messages advertise. The map value is the rejection message.
var unconditionallyRejectedKubectlCommands = map[string]string{
	"describe": "\"kubectl describe\" is not supported, because its output cannot be redacted reliably; read the " +
		"resource with \"kubectl get ... -o yaml\" or \"-o json\" instead",
}

// allowedSubcommandsPerKubectlCommand lists the allowed kubectl commands that may only be invoked with one of the
// listed subcommands. Every other subcommand is rejected for that kubectl command. This way a future kubectl release
// adding a new subcommand does not widen what the connector accepts. The empty string stands for the bare subcommand,
// that is, an invocation of that particular kubectl command without a subcommand. A kubctl command that may only be
// invoked bare must list the empty string as its only entry.
var allowedSubcommandsPerKubectlCommand = map[string][]string{
	// "auth can-i" only reports what the agent0-connector's service account is allowed to do, but its sibling
	// subcommand "auth reconcile" creates and updates roles and role bindings, hence "auth" is only allowed with the
	// subcommand "can-i".
	"auth": {"can-i"},

	// "cluster-info" is read-only and harmless on its own, but its only subcommand, "kubectl cluster-info dump", writes
	// the full JSON of the nodes, events, replication controllers, services, daemon sets, deployments, replica sets and
	// pods of a namespace or the whole cluster to stdout. This response cannot be redacted reliably, hence
	// "cluster-info" is only allowed in its bare form.
	"cluster-info": {""},
}

// outputFormatHandling states what the connector does with a response rendered in a particular output format.
type outputFormatHandling int

const (
	// outputFormatUnknown is the zero value, so that an output format that is not listed in knownOutputFormats is treated
	// as unknown (i.e. it is rejected), rather than accidentally being treated as content free.
	outputFormatUnknown outputFormatHandling = iota

	// outputFormatContentFree renders no resource content at all, hence there is nothing to redact. Such a request
	// lists resources or checks for the presence of a particular one, which is allowed even for a sensitive resource
	// type.
	outputFormatContentFree

	// outputFormatRedactable renders resource content in a form the connector can parse and redact. Requests using one
	// of these are processed.
	outputFormatRedactable

	// outputFormatUnredactable renders resource content the connector cannot parse and hence not redact. A request
	// using it is rejected.
	outputFormatUnredactable
)

// goTemplateOutputFormat is the output format the --template flag selects, even without -o.
const goTemplateOutputFormat = "go-template"

// supportedOutputFormatsHint names the output formats that do work, for the rejection of one that does not.
const supportedOutputFormatsHint = "reading a resource is supported with -o json/yaml/name/wide (or without an " +
	"output format)"

// knownOutputFormats lists every output format the connector knows. The key is the normalized base type (see
// normalizeOutputFormat). The map contains output formats that the connector allows in kubectl get commands, and
// rejected formats for which it returns a specific rejection message. Formats not listed here are rejeced with a
// general purpose message. This might be fine for some formats, so deliberately not listing a kubectl get format here
// can also be a valid decision. This also makes sure that formats added by future kubectl releases do not widen what
// the connector accepts.
//
// Command requests are generated by an agent, so the argument stream is arbitrary. The rejection messages are also
// tailored for an agent. The important bit is that we offer alternatives and tell the caller what it can do instead.
// The specific reason a format is rejected is not always relevant.
//
// Deliberately not listed (and thus rejected with a general purpose message) are
// - the formats that take their template from a file: "go-template-file",  "templatefile", "jsonpath-file" and
// "custom-columns-file" render the referenced file. They could be abused to read arbitrary files in the
// agent0-connector's container, such as its service account token or its environment. Plus, these would refer to a
// file that exists in agent0's local file system, not in the local file system of agent0-connector, hence none of these
// formats would work anyway.
// - "kyaml": it renders values verbatim and could be parsed and redacted like YAML, but we currently do not support it.
// It offers no real benefit to an agent over "-o yaml" or "-o json".
var knownOutputFormats = map[string]outputFormatHandling{
	"":     outputFormatContentFree, // the default, human-readable table output
	"name": outputFormatContentFree,
	"wide": outputFormatContentFree,

	"json": outputFormatRedactable,
	"yaml": outputFormatRedactable,

	"custom-columns":       outputFormatUnredactable,
	goTemplateOutputFormat: outputFormatUnredactable,
	"jsonpath":             outputFormatUnredactable,
	"jsonpath-as-json":     outputFormatUnredactable,
	"template":             outputFormatUnredactable,
}

// safeSortByPathPrefixes are the top-level fields a --sort-by expression may address. Neither the metadata nor the
// status of a resource holds a credential, while its spec holds every field the response redacts, and the data of a
// secret sits outside both prefixes. The termination messages in the status of a pod are the one exception, they are
// guarded separately, see terminationMessageSortByRequested.
var safeSortByPathPrefixes = [][]string{{"metadata"}, {"status"}}

var safeSortByPathPrefixesHumanReadable = func() string {
	quoted := make([]string, 0, len(safeSortByPathPrefixes))
	for _, prefix := range safeSortByPathPrefixes {
		quoted = append(quoted, fmt.Sprintf("%q", strings.Join(prefix, ".")))
	}
	return strings.Join(quoted, " or ")
}()

// unsafeSortByPathPrefix is the one field below safeSortByPathPrefixes that a --sort-by expression may not address:
// kubectl apply stores a verbatim copy of the applied manifest, credentials included, in the
// "kubectl.kubernetes.io/last-applied-configuration" annotation, see redactAnnotationValues.
var unsafeSortByPathPrefix = []string{"metadata", "annotations"}

// sensitiveResource describes how a resource type whose contents must not be exposed is guarded.
type sensitiveResource struct {
	// displayName names the resource in rejection messages.
	displayName string
}

var secretResource = sensitiveResource{displayName: "secret"}

// sensitiveResourceTypes maps the resource type names whose contents must not be exposed to their guard, in singular
// and plural form.
var sensitiveResourceTypes = map[string]sensitiveResource{
	"secret":  secretResource,
	"secrets": secretResource,
}

const (
	kubectlCommandGet    = "get"
	kubectlCommandEvents = "events"
	kubectlCommandLogs   = "logs"
)

// eventResourceTypes are the normalized resource types under which "kubectl get" reads Kubernetes events, from the core
// API group as well as from events.k8s.io, in singular, plural and short form.
var eventResourceTypes = map[string]struct{}{
	"event":  {},
	"events": {},
	"ev":     {},
}

// validateCommandAndParseArguments parses the request's argument list and ensures the request invokes an allowed
// read-only kubectl command, and only uses allowed flags. It returns the parsed argument list, which the caller may
// reuse for further processing (e.g. redacting the response). If the request is allowed, the returned error is nil.
// If an error is returned, the error describes why the request is rejected. The returned argument list must not be
// used when the error is non-nil.
func validateCommandAndParseArguments(
	req *pb.CommandRequest,
	allowedKubectlCommands AllowedKubectlCommands,
) (kubectlArguments, error) {
	if reason, blocked := disallowedExecutableRequested(req); blocked {
		return kubectlArguments{}, errors.New(reason)
	}

	arguments, err := parseKubectlArguments(req.GetArguments())
	if err != nil {
		if errors.Is(err, errUnknownKubectlCommand) {
			return kubectlArguments{}, fmt.Errorf("%w, %s", err, allowedKubectlCommands.humanReadable)
		}
		return kubectlArguments{}, fmt.Errorf("the kubectl arguments cannot be parsed: %w", err)
	}

	if reason, blocked := disallowedFlagRequested(arguments); blocked {
		return kubectlArguments{}, errors.New(reason)
	}
	if reason, blocked := hiddenLogVerbosityRequested(req); blocked {
		return kubectlArguments{}, errors.New(reason)
	}
	if reason, blocked := hiddenKubercRequested(req); blocked {
		return kubectlArguments{}, errors.New(reason)
	}

	// Check the kubectl command first, reject any kubectl command that is not on the allowlist (supportedKubectlCommands)
	// or that has not been enabled via the configuration (allowedKubectlCommands).
	if reason, blocked := disallowedKubectlCommandRequested(arguments, allowedKubectlCommands); blocked {
		return kubectlArguments{}, errors.New(reason)
	}
	if reason, blocked := disallowedSubcommandRequested(arguments); blocked {
		return kubectlArguments{}, errors.New(reason)
	}
	if reason, blocked := eventsReadViaGetRequested(arguments, allowedKubectlCommands); blocked {
		return kubectlArguments{}, errors.New(reason)
	}
	// Checked before the output format, so that a request reading a secret is rejected with the reason that names what
	// makes the secret special, whatever output format it asks for.
	if reason, blocked := sensitiveContentRequested(arguments); blocked {
		return kubectlArguments{}, errors.New(reason)
	}
	if reason, blocked := unsupportedOutputFormatRequested(arguments); blocked {
		return kubectlArguments{}, errors.New(reason)
	}
	if reason, blocked := unsafeSortByRequested(arguments); blocked {
		return kubectlArguments{}, errors.New(reason)
	}
	if reason, blocked := terminationMessageSortByRequested(arguments, allowedKubectlCommands); blocked {
		return kubectlArguments{}, errors.New(reason)
	}
	return arguments, nil
}

func disallowedExecutableRequested(req *pb.CommandRequest) (string, bool) {
	if req.GetCommand() == "" {
		return fmt.Sprintf(
			"invalid command request without command, only the %q command is allowed",
			kubectlCommand,
		), true
	}
	if req.GetCommand() != kubectlCommand {
		return fmt.Sprintf(
			"only the command %q is allowed, but got %q",
			kubectlCommand,
			req.GetCommand(),
		), true
	}
	return "", false
}

// disallowedFlagRequested reports whether the kubectl arguments set a flag that is not in the allowedFlags allowlist.
// It also returns true (i.e. disallowed) if the end-of-flags separator "--" is used.
// It returns a human-readable reason if the result is true, i.e. if this command request must be disallowed.
func disallowedFlagRequested(parsed kubectlArguments) (string, bool) {
	if parsed.hasEndOfFlagsSeparator {
		return "the kubectl flag \"--\" is not allowed", true
	}
	for _, flag := range parsed.flags {
		if _, allowed := allowedFlags[flag.longName]; allowed {
			continue
		}
		if flag.shorthand != "" {
			return fmt.Sprintf("the kubectl flag %q (%q) is not allowed", "--"+flag.longName, "-"+flag.shorthand), true
		}
		return fmt.Sprintf("the kubectl flag %q is not allowed", "--"+flag.longName), true
	}
	return "", false
}

// hiddenLogVerbosityRequested reports whether the raw argument list sets kubectl's log verbosity outside of the -v
// flag, returning a human-readable reason when it does. Before parsing any flags, the main function of the kubectl
// binary scans the raw arguments with kubectlcmd.GetLogVerbosity, which takes the level from any argument that merely
// contains "-v=" or "--v=" - a label selector such as "-l x-v=10", a container name or a positional argument. The
// level is applied globally, so at 8 and above kubectl logs the HTTP response bodies, and with them the unredacted
// contents of any resource, to stderr. parseKubectlArguments does not see this, since no -v flag is set.
func hiddenLogVerbosityRequested(req *pb.CommandRequest) (string, bool) {
	level := kubectlcmd.GetLogVerbosity(append([]string{kubectlCommand}, req.GetArguments()...))
	if level == "0" {
		return "", false
	}
	return fmt.Sprintf(
		"the kubectl arguments set the log verbosity to %q, since kubectl reads \"-v=\" anywhere in an argument; "+
			"change the argument that contains \"-v=\" or \"--v=\"",
		level,
	), true
}

// hiddenKubercRequested reports whether the raw argument list selects a kuberc file outside of the --kuberc flag,
// returning a human-readable reason when it does. Before parsing any flags, kubectl scans the raw arguments for the
// kuberc file to load user preferences (aliases and default flag values) from (see getExplicitKuberc in
// k8s.io/kubectl/pkg/kuberc). It takes the path from any argument that merely contains "--kuberc=", or from the
// argument following an argument that equals "--kuberc" - a label selector such as "-l --kuberc=/x", a container name
// or a positional argument. Preferences from that file would make kubectl resolve the argument list differently than
// parseKubectlArguments does. parseKubectlArguments does not see this, since no --kuberc flag is set. Unlike kubectl,
// the scan does not stop at "--", which only makes it stricter.
func hiddenKubercRequested(req *pb.CommandRequest) (string, bool) {
	for _, argument := range req.GetArguments() {
		if argument == "--kuberc" || strings.Contains(argument, "--kuberc=") {
			return fmt.Sprintf(
				"the kubectl arguments select a kuberc file, since kubectl reads \"--kuberc\" anywhere in an argument; "+
					"change the argument %q",
				argument,
			), true
		}
	}
	return "", false
}

func disallowedKubectlCommandRequested(
	parsed kubectlArguments,
	allowedKubectlCommands AllowedKubectlCommands,
) (string, bool) {
	if parsed.kubectlCommand == "" {
		// Invoking kubectl with no command at all - bare `kubectl`, or only global flags such as `kubectl --help` - is
		// allowed.
		return "", false
	}
	if _, supported := supportedKubectlCommands[parsed.kubectlCommand]; !supported {
		return fmt.Sprintf(
			"the kubectl command %q is not an allowed read-only command, %s",
			parsed.kubectlCommand,
			allowedKubectlCommands.humanReadable,
		), true
	}
	if rejectionMessage, rejected := unconditionallyRejectedKubectlCommands[parsed.kubectlCommand]; rejected {
		return rejectionMessage, true
	}
	if !allowedKubectlCommands.Allows(parsed.kubectlCommand) {
		return fmt.Sprintf(
			"the kubectl command %q has been disabled in the configuration of the agent0-connector (via the Helm value "+
				"operator.agent0Connector.allowedKubectlCommands), %s",
			parsed.kubectlCommand,
			allowedKubectlCommands.humanReadable,
		), true
	}
	return "", false
}

// disallowedSubcommandRequested reports whether the kubectl arguments invoke a kubectl command that is only allowed
// with a fixed set of subcommands, and the invocation uses a disallowed subcommand. It returns a human-readable reason
// if that is the case. See allowedSubcommandsPerKubectlCommand.
func disallowedSubcommandRequested(parsed kubectlArguments) (string, bool) {
	allowedSubcommands, restricted := allowedSubcommandsPerKubectlCommand[parsed.kubectlCommand]
	if !restricted {
		return "", false
	}
	requestedSubcommand := parsed.subcommand
	if slices.Contains(allowedSubcommands, requestedSubcommand) {
		return "", false
	}
	if slices.Equal(allowedSubcommands, []string{""}) {
		return fmt.Sprintf(
			"the kubectl command %q may only be used without additional subcommands, but got %q",
			parsed.kubectlCommand,
			requestedSubcommand,
		), true
	}
	if requestedSubcommand == "" {
		return fmt.Sprintf(
			"the kubectl command %q is only allowed with the subcommand %q, but no subcommand was given",
			parsed.kubectlCommand,
			strings.Join(allowedSubcommands, "\" or \""),
		), true
	}
	return fmt.Sprintf(
		"the kubectl command %q is only allowed with the subcommand %q, but the subcommand was %q",
		parsed.kubectlCommand,
		strings.Join(allowedSubcommands, "\" or \""),
		requestedSubcommand,
	), true
}

// eventsReadViaGetRequested reports whether the kubectl arguments read events via "kubectl get" while the kubectl
// command "events" has been disabled in the configuration, returning a human-readable reason when they do. Disabling
// "kubectl events" would be pointless otherwise, since "kubectl get events" hands out the same content.
func eventsReadViaGetRequested(parsed kubectlArguments, allowedKubectlCommands AllowedKubectlCommands) (string, bool) {
	if parsed.kubectlCommand != kubectlCommandGet || allowedKubectlCommands.Allows(kubectlCommandEvents) {
		return "", false
	}
	for _, resourceType := range parsed.resourceTypes {
		if _, isEvent := eventResourceTypes[resourceType]; isEvent {
			return "reading events via \"kubectl get\" is not allowed, because the kubectl command \"events\" has " +
				"been disabled in the configuration of the agent0-connector (via the Helm value " +
				"operator.agent0Connector.allowedKubectlCommands)", true
		}
	}
	return "", false
}

// sensitiveContentRequested reports whether the kubectl arguments would read the contents of a sensitive resource,
// returning a human-readable reason when they do. The default RBAC permissions disallow any access to secrets
// outright, even checking for the presence of a secret is not possible. However, overriding the default RBAC
// permissions with a custom cluster role is possible, and sensitiveContentRequested adds defense in-depth for those
// setups.
//
// Listing secrets (e.g. `kubectl get secrets`) and checking for the presence of a particular one
// (`kubectl get secret <name>`) are allowed (if the corresponding RBAC permissions are granted); serializing the data
// via an output format such as -o yaml/json/jsonpath/go-template/custom-columns (or --template) is not. This is a
// fail-closed check: output formats that could expose the data are rejected even if a particular invocation would only
// read metadata.
func sensitiveContentRequested(parsed kubectlArguments) (string, bool) {
	resource, targeted := targetedSensitiveResource(parsed)
	if !targeted {
		return "", false
	}
	if parsed.outputIsContentFree() {
		return "", false
	}
	return fmt.Sprintf(
		"reading the contents of a %s is not allowed; listing %ss or checking for the presence of a particular %s is "+
			"supported, but serializing its data (e.g. via -o yaml/json/jsonpath/go-template/custom-columns) is not",
		resource.displayName,
		resource.displayName,
		resource.displayName,
	), true
}

// targetedSensitiveResource returns the guard for the first sensitive resource the kubectl arguments reference.
func targetedSensitiveResource(parsed kubectlArguments) (sensitiveResource, bool) {
	for _, resourceType := range parsed.resourceTypes {
		if resource, isSensitive := lookupSensitiveResourceType(resourceType); isSensitive {
			return resource, true
		}
	}
	return sensitiveResource{}, false
}

// lookupSensitiveResourceType returns the guard for the given resource type, accepting the singular and plural forms as
// well as fully qualified forms such as "secrets.v1." (the API group/version suffix is ignored).
func lookupSensitiveResourceType(resourceType string) (sensitiveResource, bool) {
	resource, isSensitive := sensitiveResourceTypes[normalizeResourceType(resourceType)]
	return resource, isSensitive
}

// unsupportedOutputFormatRequested reports whether the kubectl arguments would render a resource in an output format
// the connector cannot hand out, returning a human-readable reason when they do: a format it does not know, or one
// whose result it cannot redact reliably. The --template flag counts as go-template output even without -o, mirroring
// outputIsContentFree.
func unsupportedOutputFormatRequested(parsed kubectlArguments) (string, bool) {
	var formats []string
	if format, isSet := parsed.outputFormat(); isSet {
		formats = append(formats, format)
	}
	if parsed.hasTemplateFlag() {
		// Appended, not prepended, so that an explicit -o is named in the rejection rather than the format --template
		// implies.
		formats = append(formats, goTemplateOutputFormat)
	}
	for _, format := range formats {
		switch knownOutputFormats[format] {
		case outputFormatUnknown:
			return fmt.Sprintf(
				"the kubectl output format %q is not allowed; %s",
				format,
				supportedOutputFormatsHint,
			), true
		case outputFormatUnredactable:
			return fmt.Sprintf(
				"the output format %q cannot be redacted reliably; %s",
				format,
				supportedOutputFormatsHint,
			), true
		}
	}
	return "", false
}

// unsafeSortByRequested reports whether the "kubectl get" arguments sort a resource that can contain secrets by a field
// the response redacts, returning a human-readable reason when they do. kubectl evaluates a --sort-by expression
// against the unredacted resources, so the connector cannot redact what the expression exposes: sorting by a redacted
// field leaks its order, and a filter expression such as {.spec.containers[0].env[?(@.value>"S")].name} turns the
// presence of a match into a comparison oracle that reveals the value character by character over several requests.
// The check only applies to the kubectl command whose response is redacted (i.e. kubectl get).
func unsafeSortByRequested(parsed kubectlArguments) (string, bool) {
	if parsed.kubectlCommand != kubectlCommandGet {
		// "get" is the only command whose response is redacted, so it is the only one where the --sort-by length oracle
		// matters.
		return "", false
	}
	expression, isSet := parsed.valueOf("sort-by")
	if !isSet || sortByExpressionIsSafe(expression) {
		return "", false
	}
	return fmt.Sprintf(
		"the --sort-by expression %q is not allowed; kubectl evaluates the expression against the resources "+
			"before the connector redacts them, so only a plain path below %s may be sorted by, except %q "+
			"(e.g. --sort-by=.metadata.name or --sort-by=.status.startTime)",
		expression,
		safeSortByPathPrefixesHumanReadable,
		strings.Join(unsafeSortByPathPrefix, "."),
	), true
}

// terminationMessageSortByPaths are the paths of the termination messages in the container statuses of a pod, without
// list indices, see redactTerminationMessages.
var terminationMessageSortByPaths = func() [][]string {
	paths := make([][]string, 0, 2*len(containerSpecFieldsPerStatusField))
	for statusField := range containerSpecFieldsPerStatusField {
		for _, stateField := range []string{"state", "lastState"} {
			paths = append(paths, []string{"status", statusField, stateField, "terminated", "message"})
		}
	}
	return paths
}()

// terminationMessageSortByRequested reports whether the "kubectl get" arguments sort by the termination messages of
// containers while "kubectl logs" has been disabled in the configuration, returning a human-readable reason when they
// do. Since the response redacts these messages in that case (see redactTerminationMessages), sorting by them would
// leak their order, see unsafeSortByRequested. An expression is rejected when it addresses a termination message or
// any field that contains one. The check runs after unsafeSortByRequested, which has already rejected every expression
// that is not a plain path.
func terminationMessageSortByRequested(
	parsed kubectlArguments,
	allowedKubectlCommands AllowedKubectlCommands,
) (string, bool) {
	if parsed.kubectlCommand != kubectlCommandGet || allowedKubectlCommands.Allows(kubectlCommandLogs) {
		return "", false
	}
	expression, isSet := parsed.valueOf("sort-by")
	if !isSet {
		return "", false
	}
	path, isPlainPath := parseSortByPath(expression)
	if !isPlainPath {
		return "", false
	}
	for _, messagePath := range terminationMessageSortByPaths {
		if isSortByPathBelow(messagePath, path) || isSortByPathBelow(path, messagePath) {
			return fmt.Sprintf(
				"the --sort-by expression %q is not allowed, because it addresses the termination messages of "+
					"containers, which can hold log output, and the kubectl command \"logs\" has been disabled in the "+
					"configuration of the agent0-connector (via the Helm value "+
					"operator.agent0Connector.allowedKubectlCommands)",
				expression,
			), true
		}
	}
	return "", false
}

// sortByExpressionIsSafe reports whether a --sort-by JSONPath expression addresses only fields that cannot hold a
// credential. It fails closed: anything it does not recognize as a plain path below safeSortByPathPrefixes is unsafe.
func sortByExpressionIsSafe(expression string) bool {
	path, isPlainPath := parseSortByPath(expression)
	if !isPlainPath {
		return false
	}
	if isSortByPathBelow(path, unsafeSortByPathPrefix) {
		return false
	}
	return slices.ContainsFunc(safeSortByPathPrefixes, func(prefix []string) bool {
		return isSortByPathBelow(path, prefix)
	})
}

// isSortByPathBelow reports whether a parsed --sort-by path addresses the given field or anything below it.
func isSortByPathBelow(path []string, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

// parseSortByPath reduces a --sort-by expression to the field names of the plain path it addresses, e.g.
// ".status.containerStatuses[0].state" yields ["status", "containerStatuses", "state"].
//
// It interprets the expression exactly like kubectl does, by handing it to the same functions: kubectl's
// RelaxedJSONPathExpression and client-go's JSONPath parser. That parser is more lenient than a plain dotted path
// suggests: it removes backslashes from field names, treats spaces, "$" and "@" between fields as separators, and reads
// ['annotations'] as .annotations. A string comparison would not recognize "terminated.mes\sage" or
// "terminated$.message" as the path of the termination message, although kubectl sorts by exactly that field.
//
// It reports false for anything but a sequence of named fields and single-element list indices, i.e. for a filter, a
// wildcard, a recursive descent, a union, a slice, a quoted literal, and for an expression kubectl cannot parse. Any of
// these can address fields other than the ones a prefix comparison would see. List indices are left out of the
// result, since selecting one element of a list cannot widen which field the expression addresses.
func parseSortByPath(expression string) ([]string, bool) {
	relaxed, err := get.RelaxedJSONPathExpression(expression)
	if err != nil || relaxed == "" {
		return nil, false
	}
	parser, err := jsonpath.Parse("sort-by", relaxed)
	if err != nil || len(parser.Root.Nodes) != 1 {
		return nil, false
	}
	list, isList := parser.Root.Nodes[0].(*jsonpath.ListNode)
	if !isList {
		return nil, false
	}
	path := make([]string, 0, len(list.Nodes))
	for _, node := range list.Nodes {
		switch typedNode := node.(type) {
		case *jsonpath.FieldNode:
			if typedNode.Value == "" {
				return nil, false
			}
			path = append(path, typedNode.Value)
		case *jsonpath.ArrayNode:
			if !selectsSingleListElement(typedNode) {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return path, len(path) > 0
}

// selectsSingleListElement reports whether a parsed array node is a plain index like [0], as opposed to a slice like
// [0:2] or [*]. The parser marks the end of a plain index as derived from its start.
func selectsSingleListElement(node *jsonpath.ArrayNode) bool {
	return node.Params[0].Known && node.Params[1].Derived && !node.Params[2].Known
}
