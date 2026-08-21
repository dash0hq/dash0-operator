// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"slices"
	"testing"
)

//nolint:lll
func TestParseArguments(t *testing.T) {
	tests := []struct {
		name            string
		arguments       []string
		kubectlCommand  string
		flags           []parsedFlag
		disallowedFlags []string
		resourceTypes   []string
	}{
		{name: "no arguments"},
		{name: "kubectl command only", arguments: []string{"get"}, kubectlCommand: "get"},
		{name: "kubecl command and resource type", arguments: []string{"get", "pods"}, kubectlCommand: "get", resourceTypes: []string{"pods"}},

		// The value of a value-taking flag is neither the kubectl command nor a resource type, in every spelling.
		{name: "flag value before the kubectl command", arguments: []string{"-n", "foo", "get", "pods"},
			kubectlCommand: "get", flags: []parsedFlag{{token: "-n", valueTakingName: "n", value: "foo"}}, resourceTypes: []string{"pods"}},
		{name: "flag value after the kubectl command", arguments: []string{"get", "-n", "foo", "pods"},
			kubectlCommand: "get", flags: []parsedFlag{{token: "-n", valueTakingName: "n", value: "foo"}}, resourceTypes: []string{"pods"}},
		{name: "flag value looking like a flag", arguments: []string{"logs", "my-pod", "--tail", "-1"},
			// The first positional argument after the kubectl command is always taken as the resource type slot, which for
			// "logs" is a pod name. That is harmless: resource types are only ever used for lookups.
			kubectlCommand: "logs", flags: []parsedFlag{{token: "--tail", valueTakingName: "tail", value: "-1"}}, resourceTypes: []string{"my-pod"}},
		{name: "inline value in the long form", arguments: []string{"get", "pods", "--output=yaml"},
			kubectlCommand: "get", flags: []parsedFlag{{token: "--output=yaml", valueTakingName: "output", value: "yaml"}}, resourceTypes: []string{"pods"}},
		{name: "value attached to a shorthand", arguments: []string{"get", "pods", "-oyaml"},
			kubectlCommand: "get", flags: []parsedFlag{{token: "-oyaml", valueTakingName: "o", value: "yaml"}}, resourceTypes: []string{"pods"}},
		{name: "value-taking shorthand in a group", arguments: []string{"get", "pods", "-Aoyaml"},
			kubectlCommand: "get",
			flags:          []parsedFlag{{token: "-Aoyaml", valueTakingName: "o", value: "yaml", booleanNames: []string{"A"}}},
			resourceTypes:  []string{"pods"}},
		{name: "value-taking flag without a value at the end", arguments: []string{"get", "pods", "-o"},
			kubectlCommand: "get", flags: []parsedFlag{{token: "-o", valueTakingName: "o", value: ""}}, resourceTypes: []string{"pods"}},
		{name: "boolean flags hold no value", arguments: []string{"get", "pods", "-A", "--show-labels"},
			kubectlCommand: "get",
			flags: []parsedFlag{
				{token: "-A", booleanNames: []string{"A"}},
				{token: "--show-labels", booleanNames: []string{"show-labels"}},
			},
			resourceTypes: []string{"pods"}},
		{name: "grouped boolean shorthands", arguments: []string{"logs", "my-pod", "-pA"},
			kubectlCommand: "logs",
			flags:          []parsedFlag{{token: "-pA", booleanNames: []string{"p", "A"}}},
			resourceTypes:  []string{"my-pod"}},
		{name: "repeated flags are kept in order", arguments: []string{"get", "pods", "-o", "name", "--output=yaml"},
			kubectlCommand: "get",
			flags: []parsedFlag{
				{token: "-o", valueTakingName: "o", value: "name"},
				{token: "--output=yaml", valueTakingName: "output", value: "yaml"},
			},
			resourceTypes: []string{"pods"}},

		// Resource references: a bare type only counts in the resource type slot, a type/name pair in any slot.
		{name: "a bare type in a later slot is a resource name", arguments: []string{"get", "pods", "cm"},
			kubectlCommand: "get", resourceTypes: []string{"pods"}},
		{name: "type/name pairs in every slot", arguments: []string{"get", "pod/a", "cm/b"},
			kubectlCommand: "get", resourceTypes: []string{"pod", "cm"}},
		{name: "comma-separated resource list", arguments: []string{"get", "pods,cm"},
			kubectlCommand: "get", resourceTypes: []string{"pods", "cm"}},
		{name: "resource types are normalized", arguments: []string{"get", "Secrets.v1."},
			kubectlCommand: "get", resourceTypes: []string{"secrets"}},

		// Flags outside the allowlist are collected; the remaining fields are not to be trusted then, because whether
		// such a flag consumes the following argument as its value is unknown ("/x" below is not really a kubectl command).
		{name: "disallowed flag with an inline value", arguments: []string{"get", "pods", "--kubeconfig=/x"},
			kubectlCommand: "get", disallowedFlags: []string{"--kubeconfig=/x"}, resourceTypes: []string{"pods"}},
		{name: "disallowed flag with a separate value", arguments: []string{"--kubeconfig", "/x", "get", "pods"},
			kubectlCommand: "/x", disallowedFlags: []string{"--kubeconfig"}, resourceTypes: []string{"get"}},
		{name: "the end-of-flags separator references no flag", arguments: []string{"get", "pods", "--"},
			kubectlCommand: "get", disallowedFlags: []string{"--"}, resourceTypes: []string{"pods"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := parseKubectlArguments(tt.arguments)
			if parsed.kubectlCommand != tt.kubectlCommand {
				t.Errorf("expected kubectl command %q, got %q", tt.kubectlCommand, parsed.kubectlCommand)
			}
			if !slices.EqualFunc(parsed.flags, tt.flags, parsedFlagsAreEqual) {
				t.Errorf("expected flags %+v, got %+v", tt.flags, parsed.flags)
			}
			if !slices.Equal(parsed.disallowedFlags, tt.disallowedFlags) {
				t.Errorf("expected disallowed flags %q, got %q", tt.disallowedFlags, parsed.disallowedFlags)
			}
			if !slices.Equal(parsed.resourceTypes, tt.resourceTypes) {
				t.Errorf("expected resource types %q, got %q", tt.resourceTypes, parsed.resourceTypes)
			}
		})
	}
}

// parsedFlagsAreEqual compares two parsed flags field by field; parsedFlag is not comparable, since it holds the
// boolean flag names as a slice.
func parsedFlagsAreEqual(first parsedFlag, second parsedFlag) bool {
	return first.token == second.token &&
		first.valueTakingName == second.valueTakingName &&
		first.value == second.value &&
		slices.Equal(first.booleanNames, second.booleanNames)
}

//nolint:lll
func TestExtractNormalizedResourceTypes(t *testing.T) {
	tests := []struct {
		name               string
		argument           string
		isResourceTypeSlot bool
		expected           []string
	}{
		{name: "bare type in the resource type slot", argument: "secret", isResourceTypeSlot: true, expected: []string{"secret"}},
		{name: "bare type in a later slot is a resource name", argument: "secret"},
		{name: "type/name pair in the resource type slot", argument: "secret/my-secret", isResourceTypeSlot: true, expected: []string{"secret"}},
		{name: "type/name pair in a later slot", argument: "secret/my-secret", expected: []string{"secret"}},
		{name: "only the first slash separates type and name", argument: "pod/a/b", expected: []string{"pod"}},

		{name: "comma-separated bare types in the resource type slot", argument: "secret,configmap", isResourceTypeSlot: true,
			expected: []string{"secret", "configmap"}},
		{name: "comma-separated bare types in a later slot", argument: "secret,configmap"},
		{name: "comma-separated type/name pairs in a later slot", argument: "secret/a,cm/b", expected: []string{"secret", "cm"}},
		{name: "bare types are dropped from a mixed list in a later slot", argument: "secret/a,cm", expected: []string{"secret"}},
		{name: "mixed list in the resource type slot keeps every entry", argument: "secret/a,cm", isResourceTypeSlot: true,
			expected: []string{"secret", "cm"}},

		// Normalization: lower-case, and the API group/version suffix of fully qualified forms is stripped.
		{name: "upper-case type", argument: "Secret", isResourceTypeSlot: true, expected: []string{"secret"}},
		{name: "type with a version suffix", argument: "secrets.v1.", isResourceTypeSlot: true, expected: []string{"secrets"}},
		{name: "type with an API group", argument: "Dash0Monitorings.operator.dash0.com", isResourceTypeSlot: true,
			expected: []string{"dash0monitorings"}},
		{name: "fully qualified type/name pair in a later slot", argument: "Secrets.v1./my-secret", expected: []string{"secrets"}},

		// Degenerate inputs are normalized to empty types rather than being rejected here: resource types are only ever
		// used for lookups, and an empty type matches nothing.
		{name: "empty argument in the resource type slot", argument: "", isResourceTypeSlot: true, expected: []string{""}},
		{name: "empty argument in a later slot", argument: ""},
		{name: "trailing comma in the resource type slot", argument: "secret,", isResourceTypeSlot: true, expected: []string{"secret", ""}},
		{name: "pair without a type", argument: "/my-secret", expected: []string{""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractNormalizedResourceTypes(tt.argument, tt.isResourceTypeSlot)
			if !slices.Equal(got, tt.expected) {
				t.Errorf("expected resource types %q, got %q", tt.expected, got)
			}
		})
	}
}

//nolint:lll
func TestInspectFlagToken(t *testing.T) {
	tests := []struct {
		token           string
		allowed         bool
		flag            parsedFlag
		consumesNextArg bool
	}{
		{token: "--namespace", allowed: true, flag: parsedFlag{valueTakingName: "namespace"}, consumesNextArg: true},
		{token: "--namespace=x", allowed: true, flag: parsedFlag{valueTakingName: "namespace", value: "x"}},
		// A value-taking flag with an empty inline value does not consume the following argument.
		{token: "--namespace=", allowed: true, flag: parsedFlag{valueTakingName: "namespace"}},
		{token: "--all-namespaces", allowed: true, flag: parsedFlag{booleanNames: []string{"all-namespaces"}}},
		// The value of a boolean flag is not tracked, the flag counts as referenced either way.
		{token: "--all-namespaces=false", allowed: true, flag: parsedFlag{booleanNames: []string{"all-namespaces"}}},
		{token: "--namespaced=false", allowed: true, flag: parsedFlag{booleanNames: []string{"namespaced"}}},
		{token: "-n", allowed: true, flag: parsedFlag{valueTakingName: "n"}, consumesNextArg: true},
		{token: "-nx", allowed: true, flag: parsedFlag{valueTakingName: "n", value: "x"}},
		{token: "-n=x", allowed: true, flag: parsedFlag{valueTakingName: "n", value: "x"}},
		{token: "-n=", allowed: true, flag: parsedFlag{valueTakingName: "n"}},
		{token: "-A", allowed: true, flag: parsedFlag{booleanNames: []string{"A"}}},
		{token: "-pA", allowed: true, flag: parsedFlag{booleanNames: []string{"p", "A"}}},
		{token: "-Ao", allowed: true, flag: parsedFlag{valueTakingName: "o", booleanNames: []string{"A"}}, consumesNextArg: true},
		{token: "-Aoyaml", allowed: true, flag: parsedFlag{valueTakingName: "o", booleanNames: []string{"A"}, value: "yaml"}},
		{token: "-oyaml", allowed: true, flag: parsedFlag{valueTakingName: "o", value: "yaml"}},
		// The characters following a value-taking shorthand are its value, not further flags.
		{token: "-oA", allowed: true, flag: parsedFlag{valueTakingName: "o", value: "A"}},
		{token: "--raw", allowed: false},
		{token: "--raw=/api/v1/secrets", allowed: false},
		{token: "-w", allowed: false},
		{token: "-Az", allowed: false},
		{token: "--", allowed: false},
		{token: "-", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			expected := tt.flag
			if tt.allowed {
				expected.token = tt.token
			}
			flag, consumesNextArg, allowed := inspectFlagToken(tt.token)
			if allowed != tt.allowed {
				t.Fatalf("expected allowed=%t, got %t", tt.allowed, allowed)
			}
			if !parsedFlagsAreEqual(flag, expected) {
				t.Errorf("expected flag %+v, got %+v", expected, flag)
			}
			if consumesNextArg != tt.consumesNextArg {
				t.Errorf("expected consumesNextArg=%t, got %t", tt.consumesNextArg, consumesNextArg)
			}
		})
	}
}

func TestOutputFormats(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		expected  []string
	}{
		{name: "no output flag", arguments: []string{"get", "pods"}, expected: nil},
		{name: "separate value", arguments: []string{"get", "pods", "-o", "YAML"}, expected: []string{"yaml"}},
		{name: "inline value", arguments: []string{"get", "pods", "-o=json"}, expected: []string{"json"}},
		{name: "attached value", arguments: []string{"get", "pods", "-oyaml"}, expected: []string{"yaml"}},
		{name: "grouped shorthand", arguments: []string{"get", "pods", "-Aoyaml"}, expected: []string{"yaml"}},
		{name: "long form", arguments: []string{"get", "pods", "--output=wide"}, expected: []string{"wide"}},
		{name: "composite format",
			arguments: []string{"get", "pods", "-o", "jsonpath={.items}"}, expected: []string{"jsonpath"}},
		{name: "every occurrence of a repeated flag is reported",
			arguments: []string{"get", "pods", "-o", "name", "--output=yaml"}, expected: []string{"name", "yaml"}},
		{name: "value of another flag is not read as the format",
			arguments: []string{"get", "pods", "-l", "o=yaml"}, expected: nil},
		{name: "output flag without a value", arguments: []string{"get", "pods", "-o"}, expected: []string{""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseKubectlArguments(tt.arguments).outputFormats(); !slices.Equal(got, tt.expected) {
				t.Errorf("expected output formats %q, got %q", tt.expected, got)
			}
		})
	}
}
