// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import "slices"

// parsedArguments is the resolved form of the argument list of a kubectl invocation. It is produced once per command
// request (see parser.go#parseArguments) and is the basis for validating the request as well as for redacting the
// response, so that the rules for interpreting an argument list live in a single place.
type parsedArguments struct {
	// subcommand is the kubectl subcommand, that is, the first positional argument, which may be preceded by global
	// flags ("get" in `kubectl -n foo get pods`), mirroring how kubectl/Cobra resolves it. It is empty when the
	// invocation has no positional argument at all (bare `kubectl`, or only flags such as `kubectl --help`).
	subcommand string

	// flags holds the flag tokens of the argument list, in the order they occur.
	flags []parsedFlag

	// disallowedFlags holds the tokens referencing a flag that is not in the allowedFlags allowlist. When it is
	// non-empty, the other fields must be ignored. Whether a disallowed flag consumes the following argument as its value
	// is unknown. The request has to be rejected since it includes a disallowed flag.
	disallowedFlags []string

	// resourceTypes holds the normalized resource types referenced by the positional arguments that follow the
	// subcommand, in the order they occur.
	resourceTypes []string

	// positionalArguments holds the positional arguments that follow the subcommand, verbatim and in the order they
	// occur.
	positionalArguments []string
}

// parsedFlag is a single flag token of an argument list, resolved against the allowedFlags allowlist.
type parsedFlag struct {
	// token is the flag as written, e.g. "-Aoyaml" or "--output=yaml".
	token string

	// booleanNames holds the long names or shorthands (without leading dashes) of the flags in this token that take no
	// value. A single token can group several boolean shorthands (pflag accepts "-Aw").
	booleanNames []string

	// valueTakingName is the long name or shorthand (without leading dashes) of the value-taking flag in this token, or
	// "" if the token holds no value-taking flag.
	valueTakingName string

	// value is the value assigned to valueTakingName, be it within the token itself ("-oyaml", "--output=yaml") or as the
	// following argument ("-o yaml"). It is empty for a token that holds no value-taking flag, and for a value-taking
	// flag at the very end of the argument list (e.g. not followed by an actual value).
	value string
}

// valuesOf returns the values assigned to the given flag names (long names or shorthands, without leading dashes), in
// the order the flags occur. Every occurrence is reported, not just the effective (last) one, so that callers can
// reject an argument list in which any occurrence is problematic.
func (p parsedArguments) valuesOf(names ...string) []string {
	var values []string
	for _, flag := range p.flags {
		if flag.valueTakingName != "" && slices.Contains(names, flag.valueTakingName) {
			values = append(values, flag.value)
		}
	}
	return values
}

// outputFormats returns the normalized output formats requested via -o/--output (handling the "-o yaml", "-o=yaml",
// "-oyaml", "-Aoyaml" and "--output=yaml" forms), or an empty slice if none is set. For composite formats it returns
// the base type, e.g. "jsonpath" for "jsonpath={.data}". Repeating the flag yields one entry per occurrence: kubectl
// applies the last one, but each occurrence is reported so callers do not have to replicate that precedence.
func (p parsedArguments) outputFormats() []string {
	values := p.valuesOf("o", "output")
	formats := make([]string, 0, len(values))
	for _, value := range values {
		formats = append(formats, normalizeOutputFormat(value))
	}
	return formats
}

// hasTemplateFlag reports whether the --template flag is set, which selects go-template output and can therefore expose
// a resource's content.
func (p parsedArguments) hasTemplateFlag() bool {
	return len(p.valuesOf("template")) > 0
}

// parseableOutputFormat returns the output format with which the invocation renders the targeted resources, provided
// that format is one the connector can parse itself (see parseableOutputFormats). It reports false for every other
// output format, and also for an invocation that sets the output format more than once or combines it with --template,
// so that kubectl's precedence rules do not have to be replicated here. Composite formats such as jsonpath are excluded
// as well: their output can happen to parse as JSON or YAML while holding none of the structure of a resource document.
func (p parsedArguments) parseableOutputFormat() (string, bool) {
	if p.hasTemplateFlag() {
		return "", false
	}
	formats := p.outputFormats()
	if len(formats) != 1 {
		return "", false
	}
	if _, parseable := parseableOutputFormats[formats[0]]; !parseable {
		return "", false
	}
	return formats[0], true
}

// outputIsContentFree reports whether every requested output format is one that does not expose a resource's content.
// The --template flag selects go-template output and is therefore never content-free.
func (p parsedArguments) outputIsContentFree() bool {
	if p.hasTemplateFlag() {
		return false
	}
	for _, format := range p.outputFormats() {
		if _, contentFree := contentFreeOutputFormats[format]; !contentFree {
			return false
		}
	}
	return true
}
