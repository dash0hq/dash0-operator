// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/component-base/logs"
)

//nolint:lll
func TestParseArguments(t *testing.T) {
	tests := []struct {
		name                   string
		arguments              []string
		kubectlCommand         string
		subcommand             string
		flags                  []parsedFlag
		positionalArguments    []string
		resourceTypes          []string
		hasEndOfFlagsSeparator bool
	}{
		{name: "no arguments"},
		{name: "only flags", arguments: []string{"--help"}, flags: []parsedFlag{{longName: "help", shorthand: "h", value: "true"}}},
		{name: "kubectl command only", arguments: []string{"get"}, kubectlCommand: "get"},
		{name: "kubecl command and resource type", arguments: []string{"get", "pods"}, kubectlCommand: "get",
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},

		// The value of a value-taking flag is neither the kubectl command nor a resource type, in every spelling.
		{name: "flag value before the kubectl command", arguments: []string{"-n", "foo", "get", "pods"},
			kubectlCommand: "get", flags: []parsedFlag{{longName: "namespace", shorthand: "n", value: "foo"}},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		{name: "flag value after the kubectl command", arguments: []string{"get", "-n", "foo", "pods"},
			kubectlCommand: "get", flags: []parsedFlag{{longName: "namespace", shorthand: "n", value: "foo"}},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		{name: "flag value looking like a flag", arguments: []string{"logs", "my-pod", "--tail", "-1"},
			// The first positional argument after the kubectl command is always taken as the resource type slot, which for
			// "logs" is a pod name. This is not entirely clean, but it is harmless:
			// resource types are only ever used for lookups.
			kubectlCommand: "logs", flags: []parsedFlag{{longName: "tail", value: "-1"}},
			positionalArguments: []string{"my-pod"}, resourceTypes: []string{"my-pod"}},
		{name: "inline value in the long form", arguments: []string{"get", "pods", "--output=yaml"},
			kubectlCommand: "get", flags: []parsedFlag{{longName: "output", shorthand: "o", value: "yaml"}},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		{name: "value attached to a shorthand", arguments: []string{"get", "pods", "-oyaml"},
			kubectlCommand: "get", flags: []parsedFlag{{longName: "output", shorthand: "o", value: "yaml"}},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		{name: "value-taking shorthand in a group", arguments: []string{"get", "pods", "-Aoyaml"},
			kubectlCommand: "get",
			flags: []parsedFlag{
				{longName: "all-namespaces", shorthand: "A", value: "true"},
				{longName: "output", shorthand: "o", value: "yaml"},
			},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		{name: "boolean flags", arguments: []string{"get", "pods", "-A", "--show-labels"},
			kubectlCommand: "get",
			flags: []parsedFlag{
				{longName: "all-namespaces", shorthand: "A", value: "true"},
				{longName: "show-labels", value: "true"},
			},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		{name: "boolean flag with an explicit value", arguments: []string{"get", "pods", "--all-namespaces=false"},
			kubectlCommand: "get", flags: []parsedFlag{{longName: "all-namespaces", shorthand: "A", value: "false"}},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		{name: "grouped boolean shorthands", arguments: []string{"get", "pods", "-Aw"},
			kubectlCommand: "get",
			flags: []parsedFlag{
				{longName: "all-namespaces", shorthand: "A", value: "true"},
				{longName: "watch", shorthand: "w", value: "true"},
			},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		{name: "a repeated flag holds the value kubectl applies", arguments: []string{"get", "pods", "-o", "name", "--output=yaml"},
			kubectlCommand: "get", flags: []parsedFlag{{longName: "output", shorthand: "o", value: "yaml"}},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		// Flags are resolved whether or not they are allowed, validation.go checks them against the allowlist.
		{name: "a flag outside the allowlist with a separate value", arguments: []string{"--kubeconfig", "/x", "get", "pods"},
			kubectlCommand: "get", flags: []parsedFlag{{longName: "kubeconfig", value: "/x"}},
			positionalArguments: []string{"pods"}, resourceTypes: []string{"pods"}},
		{name: "the end-of-flags separator", arguments: []string{"get", "pods", "--", "-x"},
			kubectlCommand: "get", positionalArguments: []string{"pods", "-x"}, resourceTypes: []string{"pods"},
			hasEndOfFlagsSeparator: true},

		// Subcommands are part of the resolved command path, not positional arguments.
		{name: "subcommand", arguments: []string{"auth", "can-i", "--list"},
			kubectlCommand: "auth", subcommand: "can-i", flags: []parsedFlag{{longName: "list", value: "true"}}},
		{name: "subcommand with positional arguments", arguments: []string{"auth", "can-i", "get", "pods"},
			kubectlCommand: "auth", subcommand: "can-i", positionalArguments: []string{"get", "pods"},
			resourceTypes: []string{"get"}},
		{name: "an argument that is no subcommand is a positional argument", arguments: []string{"cluster-info", "somethingelse"},
			kubectlCommand: "cluster-info", positionalArguments: []string{"somethingelse"}, resourceTypes: []string{"somethingelse"}},
		{name: "an alias of a subcommand resolves to its canonical name", arguments: []string{"top", "pods"},
			kubectlCommand: "top", subcommand: "pod"},

		// Regression tests for inconsistencies between kubectl's cobra based parsing and agent0-connector's parsing.
		// kubectl's root command does not define -A, so cobra assumes that it takes a value and consumes "version" as
		// that value; the kubectl command that is executed is "cluster-info dump".
		{name: "a subcommand flag before the kubectl command", arguments: []string{"-A", "version", "cluster-info", "dump"},
			kubectlCommand: "cluster-info", subcommand: "dump",
			flags:               []parsedFlag{{longName: "all-namespaces", shorthand: "A", value: "true"}},
			positionalArguments: []string{"version"}, resourceTypes: []string{"version"}},
		// cobra only strips the value of a two-character shorthand from the arguments of the root command, so a grouped
		// shorthand ending in a value-taking flag ("-An") consumes nothing there, and the following argument is the kubectl
		// command. Only the flag parsing of that kubectl command then assigns the next argument ("version") to -n.
		{name: "a grouped shorthand with a value-taking flag before describe", arguments: []string{"-An", "describe", "version", "configmaps"},
			kubectlCommand: "describe",
			flags: []parsedFlag{
				{longName: "all-namespaces", shorthand: "A", value: "true"},
				{longName: "namespace", shorthand: "n", value: "version"},
			},
			positionalArguments: []string{"configmaps"}, resourceTypes: []string{"configmaps"}},
		{name: "a grouped shorthand with a value-taking flag before get", arguments: []string{"-An", "get", "version", "configmaps", "-o", "yaml"},
			kubectlCommand: "get",
			flags: []parsedFlag{
				{longName: "all-namespaces", shorthand: "A", value: "true"},
				{longName: "namespace", shorthand: "n", value: "version"},
				{longName: "output", shorthand: "o", value: "yaml"},
			},
			positionalArguments: []string{"configmaps"}, resourceTypes: []string{"configmaps"}},
		{name: "a grouped shorthand with a value-taking flag before events", arguments: []string{"-An", "events", "version"},
			kubectlCommand: "events",
			flags: []parsedFlag{
				{longName: "all-namespaces", shorthand: "A", value: "true"},
				{longName: "namespace", shorthand: "n", value: "version"},
			}},

		// Resource references: a bare type only counts in the resource type slot, a type/name pair in any slot.
		{name: "a bare type in a later slot is a resource name", arguments: []string{"get", "pods", "cm"},
			kubectlCommand: "get", positionalArguments: []string{"pods", "cm"}, resourceTypes: []string{"pods"}},
		{name: "type/name pairs in every slot", arguments: []string{"get", "pod/a", "cm/b"},
			kubectlCommand: "get", positionalArguments: []string{"pod/a", "cm/b"}, resourceTypes: []string{"pod", "cm"}},
		{name: "comma-separated resource list", arguments: []string{"get", "pods,cm"},
			kubectlCommand: "get", positionalArguments: []string{"pods,cm"}, resourceTypes: []string{"pods", "cm"}},
		{name: "resource types are normalized", arguments: []string{"get", "Secrets.v1."},
			kubectlCommand: "get", positionalArguments: []string{"Secrets.v1."}, resourceTypes: []string{"secrets"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseKubectlArguments(tt.arguments)
			if err != nil {
				t.Fatalf("expected the arguments to be parsed, but got an error: %v", err)
			}
			if parsed.kubectlCommand != tt.kubectlCommand {
				t.Errorf("expected kubectl command %q, got %q", tt.kubectlCommand, parsed.kubectlCommand)
			}
			if parsed.subcommand != tt.subcommand {
				t.Errorf("expected subcommand %q, got %q", tt.subcommand, parsed.subcommand)
			}
			if !slices.Equal(parsed.flags, tt.flags) {
				t.Errorf("expected flags %+v, got %+v", tt.flags, parsed.flags)
			}
			if !slices.Equal(parsed.positionalArguments, tt.positionalArguments) {
				t.Errorf("expected positional arguments %q, got %q", tt.positionalArguments, parsed.positionalArguments)
			}
			if !slices.Equal(parsed.resourceTypes, tt.resourceTypes) {
				t.Errorf("expected resource types %q, got %q", tt.resourceTypes, parsed.resourceTypes)
			}
			if parsed.hasEndOfFlagsSeparator != tt.hasEndOfFlagsSeparator {
				t.Errorf("expected hasEndOfFlagsSeparator=%t, got %t", tt.hasEndOfFlagsSeparator, parsed.hasEndOfFlagsSeparator)
			}
		})
	}
}

// TestFindCommandRejectsTraverseChildren makes sure the connector fails closed if a kubectl release starts setting
// TraverseChildren on its root command, since cobra then resolves commands via Traverse instead of Find.
func TestFindCommandRejectsTraverseChildren(t *testing.T) {
	root := &cobra.Command{Use: "kubectl", TraverseChildren: true}
	root.AddCommand(&cobra.Command{Use: "get", Run: func(*cobra.Command, []string) {}})

	_, _, err := findCommand(root, []string{"get", "pods"})
	if err == nil {
		t.Fatal("expected an error for a root command with TraverseChildren, but the command was resolved")
	}
	if !strings.Contains(err.Error(), "TraverseChildren") {
		t.Errorf("expected the error to mention TraverseChildren, got %q", err)
	}
}

// TestParseArgumentsRejectsWhatKubectlRejects covers argument lists that kubectl itself refuses to execute.
func TestParseArgumentsRejectsWhatKubectlRejects(t *testing.T) {
	tests := []struct {
		name           string
		arguments      []string
		unknownCommand bool
		errorContains  string
	}{
		{name: "unknown kubectl command", arguments: []string{"foo", "bar"}, unknownCommand: true,
			errorContains: `unknown command "foo" for "kubectl"`},
		{name: "unknown kubectl command after a flag", arguments: []string{"-n", "x", "foo"}, unknownCommand: true,
			errorContains: `unknown command "foo" for "kubectl"`},
		{name: "a flag the kubectl command does not define", arguments: []string{"get", "pods", "--tail", "5"},
			errorContains: "unknown flag: --tail"},
		{name: "a shorthand the kubectl command does not define", arguments: []string{"logs", "my-pod", "-pA"},
			errorContains: "unknown shorthand flag: 'A'"},
		{name: "a value-taking flag without a value", arguments: []string{"get", "pods", "-o"},
			errorContains: "flag needs an argument"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseKubectlArguments(tt.arguments)
			if err == nil {
				t.Fatal("expected an error, but the arguments were parsed")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("expected the error to contain %q, got %q", tt.errorContains, err)
			}
			if errors.Is(err, errUnknownKubectlCommand) != tt.unknownCommand {
				t.Errorf("expected errors.Is(err, errUnknownKubectlCommand)=%t, got %v", tt.unknownCommand, err)
			}
		})
	}
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
		{name: "padded type in the resource type slot", argument: " secret\t", isResourceTypeSlot: true, expected: []string{"secret"}},
		{name: "padded type in a comma-separated list", argument: "configmap, secret", isResourceTypeSlot: true,
			expected: []string{"configmap", "secret"}},
		{name: "padded type/name pair in a later slot", argument: " secret/my-secret", expected: []string{"secret"}},

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

func TestOutputFormat(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		expected  string
		isSet     bool
	}{
		{name: "no output flag", arguments: []string{"get", "pods"}},
		{name: "separate value", arguments: []string{"get", "pods", "-o", "YAML"}, expected: "yaml", isSet: true},
		{name: "inline value", arguments: []string{"get", "pods", "-o=json"}, expected: "json", isSet: true},
		{name: "attached value", arguments: []string{"get", "pods", "-oyaml"}, expected: "yaml", isSet: true},
		{name: "grouped shorthand", arguments: []string{"get", "pods", "-Aoyaml"}, expected: "yaml", isSet: true},
		{name: "long form", arguments: []string{"get", "pods", "--output=wide"}, expected: "wide", isSet: true},
		{name: "composite format",
			arguments: []string{"get", "pods", "-o", "jsonpath={.items}"}, expected: "jsonpath", isSet: true},
		{name: "the last occurrence of a repeated flag is the one kubectl applies",
			arguments: []string{"get", "pods", "-o", "name", "--output=yaml"}, expected: "yaml", isSet: true},
		{name: "value of another flag is not read as the format",
			arguments: []string{"get", "pods", "-l", "o=yaml"}},
		{name: "empty value", arguments: []string{"get", "pods", "-o="}, expected: "", isSet: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseKubectlArguments(tt.arguments)
			if err != nil {
				t.Fatalf("expected the arguments to be parsed, but got an error: %v", err)
			}
			format, isSet := parsed.outputFormat()
			if format != tt.expected || isSet != tt.isSet {
				t.Errorf("expected output format %q (set: %t), got %q (set: %t)", tt.expected, tt.isSet, format, isSet)
			}
		})
	}
}

// TestParseArgumentsIsSafeForConcurrentUse covers that command requests, which are executed concurrently, can be parsed
// concurrently; building kubectl's command tree writes to package-level variables. Run with -race to be meaningful.
func TestParseArgumentsIsSafeForConcurrentUse(t *testing.T) {
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			parsed, err := parseKubectlArguments([]string{"-An", "get", "version", "pods"})
			if err != nil {
				t.Errorf("expected the arguments to be parsed, but got an error: %v", err)
				return
			}
			if parsed.kubectlCommand != "get" {
				t.Errorf("expected kubectl command %q, got %q", "get", parsed.kubectlCommand)
			}
		})
	}
	wg.Wait()
}

// TestParseArgumentsDoesNotChangeTheLoggingConfigurationOfTheProcess covers that parsing a command request does not
// write to global klog flags.
func TestParseArgumentsDoesNotChangeTheLoggingConfigurationOfTheProcess(t *testing.T) {
	before := globalLoggingFlagValues()

	parsed, err := parseKubectlArguments(
		[]string{"get", "-v", "10", "--vmodule=parser=5", "--log-flush-frequency=1ms", "pods"},
	)
	if err != nil {
		t.Fatalf("expected the arguments to be parsed, but got an error: %v", err)
	}

	expectedFlags := []parsedFlag{
		{longName: "log-flush-frequency", value: "1ms"},
		{longName: "v", shorthand: "v", value: "10"},
		{longName: "vmodule", value: "parser=5"},
	}
	if !slices.Equal(parsed.flags, expectedFlags) {
		t.Errorf("expected flags %v, got %v", expectedFlags, parsed.flags)
	}
	if !slices.Equal(parsed.positionalArguments, []string{"pods"}) {
		t.Errorf("expected positional arguments %v, got %v", []string{"pods"}, parsed.positionalArguments)
	}

	after := globalLoggingFlagValues()
	if len(before) == 0 {
		t.Fatal("expected logs.AddFlags to add flags")
	}
	for name, value := range before {
		if after[name] != value {
			t.Errorf("expected the global value of the flag %q to stay %q, but it is %q", name, value, after[name])
		}
	}
}

// globalLoggingFlagValues returns the current values of the flags that logs.AddFlags binds to the global state of klog
// and component-base, keyed by flag name.
func globalLoggingFlagValues() map[string]string {
	flags := pflag.NewFlagSet("global-logging-flags", pflag.ContinueOnError)
	logs.AddFlags(flags)
	values := map[string]string{}
	flags.VisitAll(func(flag *pflag.Flag) {
		values[flag.Name] = flag.Value.String()
	})
	return values
}

// TestParseArgumentsDoesNotRetainMemory covers that parsing a command request does not leave memory behind that
// outlives the request. The connector parses every command request it receives, so memory retained per parse adds up
// over the lifetime of the process until it runs into its memory limit.
func TestParseArgumentsDoesNotRetainMemory(t *testing.T) {
	const (
		warmUpParses       = 5
		measuredParses     = 100
		maxHeapGrowthBytes = 5 << 20
	)
	arguments := []string{"get", "pods", "-n", "default", "-o", "json"}

	// State that is initialized once per process on the first parse is not a leak, hence it is excluded from the
	// measurement.
	for range warmUpParses {
		if _, err := parseKubectlArguments(arguments); err != nil {
			t.Fatalf("expected the arguments to be parsed, but got an error: %v", err)
		}
	}

	before := heapAllocAfterGC()
	for range measuredParses {
		if _, err := parseKubectlArguments(arguments); err != nil {
			t.Fatalf("expected the arguments to be parsed, but got an error: %v", err)
		}
	}
	growth := int64(heapAllocAfterGC()) - int64(before)

	if growth > maxHeapGrowthBytes {
		t.Errorf(
			"expected the heap to grow by at most %d MiB over %d parses, but it grew by %.1f MiB (%d KiB per parse)",
			maxHeapGrowthBytes>>20,
			measuredParses,
			float64(growth)/(1<<20),
			growth/measuredParses>>10,
		)
	}
}

// heapAllocAfterGC returns the number of bytes of allocated heap objects after a garbage collection, i.e. the heap that
// is still reachable.
func heapAllocAfterGC() uint64 {
	// The second collection frees objects whose finalizers ran during the first one.
	runtime.GC()
	runtime.GC()
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return memStats.HeapAlloc
}
