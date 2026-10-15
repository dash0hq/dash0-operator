// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

// kubectlArguments is the resolved form of the argument list of a kubectl invocation. It is produced once per command
// request (see parser.go#parseKubectlArguments) and is the basis for validating the request as well as for redacting
// the response, so that the rules for interpreting an argument list live in a single place.
type kubectlArguments struct {
	// kubectlCommand is the kubectl command to which kubectl resolves the argument list, by its canonical name.
	// Example: In `kubectl -n foo get pods`, the kubectlCommand is "get", in `kubectl auth can-i --list` it is "auth". It
	// is empty when the argument list resolves to the root command, e.g. a bare `kubectl`, or only flags such as
	// `kubectl --help`. The term used throughout the images/agent0-connector is "kubectl command" to distinguish it from
	// the actual executable or shell command of a command request (e.g. the string "kubectl" itself); and to also
	// distinguish it from kubectl subcommands, e.g. the "can-i" in `kubectl auth can-i`.
	kubectlCommand string

	// subcommand is the rest of the resolved command path below the kubectlCommand, by its canonical names, e.g. "can-i"
	// for `kubectl auth can-i`, or "dump" for `kubectl cluster-info dump`. It is empty if the argument list resolves to
	// the kubectlCommand itself. An argument that does not name a subcommand of the kubectlCommand is a positional
	// argument.
	subcommand string

	// flags holds the flags the argument list sets, sorted by name. A flag that is set more than once is listed once,
	// with the value kubectl applies (the last one).
	flags []parsedFlag

	// hasEndOfFlagsSeparator is true if the argument list contains the end-of-flags separator "--".
	hasEndOfFlagsSeparator bool

	// resourceTypes holds the normalized resource types referenced by the positional arguments, in the order they occur.
	// The raw strings are also additionally available in positionalArguments.
	resourceTypes []string

	// positionalArguments holds the positional arguments of the resolved command, verbatim and in the order they occur.
	positionalArguments []string
}

// parsedFlag is a single flag that an argument list sets.
type parsedFlag struct {
	// longName is the long name of the flag (without leading dashes), whether the argument list sets it via its long name
	// or via its shorthand.
	longName string

	// shorthand is the shorthand of the flag (without the leading dash), or "" if the flag has none.
	shorthand string

	// value is the value kubectl applies to the flag, rendered as a string.
	// The value for a boolean flag set without an explicit value is "true".
	value string
}

// valueOf returns the value of the flag with the given long name and whether the argument list sets that flag at all.
func (p kubectlArguments) valueOf(name string) (string, bool) {
	for _, flag := range p.flags {
		if flag.longName == name {
			return flag.value, true
		}
	}
	return "", false
}

// outputFormat returns the normalized output format requested via -o/--output, and whether the flag is set at all. For
// composite formats it returns the base type, e.g. "jsonpath" for "jsonpath={.data}".
func (p kubectlArguments) outputFormat() (string, bool) {
	value, isSet := p.valueOf("output")
	if !isSet {
		return "", false
	}
	return normalizeOutputFormat(value), true
}

// hasTemplateFlag reports whether the --template flag is set, which selects go-template output and can therefore expose
// a resource's content.
func (p kubectlArguments) hasTemplateFlag() bool {
	_, isSet := p.valueOf("template")
	return isSet
}

// parseableOutputFormat returns the output format with which the invocation renders the targeted resources, provided
// that format is one the connector can parse itself (see parseableOutputFormats). It reports false for every other
// output format, and also for an invocation that combines it with --template. The caller is supposed to reject
// executing the command request if this method returns false. Composite formats such as jsonpath are excluded/rejected
// as well. Their output might be parseable as JSON or YAML, but we do not know the structure of the resulting document.
// Do not call this for command requests that do not render any resource content at all - check that with
// responseHasToBeRedacted before calling this method.
func (p kubectlArguments) parseableOutputFormat() (string, bool) {
	if p.hasTemplateFlag() {
		return "", false
	}
	format, isSet := p.outputFormat()
	if !isSet {
		return "", false
	}
	if _, parseable := parseableOutputFormats[format]; !parseable {
		return "", false
	}
	return format, true
}

// outputIsContentFree reports whether the requested output format is one that does not expose a resource's content.
// The --template flag selects go-template output and is therefore never content-free.
func (p kubectlArguments) outputIsContentFree() bool {
	if p.hasTemplateFlag() {
		return false
	}
	format, _ := p.outputFormat()
	return knownOutputFormats[format] == outputFormatContentFree
}
