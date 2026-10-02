// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/component-base/logs"
	kubectlcmd "k8s.io/kubectl/pkg/cmd"
)

var (
	// errUnknownKubectlCommand is wrapped by the error parseKubectlArguments returns when the argument list does not
	// resolve to a command of kubectl's command tree.
	errUnknownKubectlCommand = errors.New("the arguments do not resolve to a kubectl command")

	// commandTreeMutex serializes building and using kubectl's command tree: kubectl.NewKubectlCommand writes to
	// package-level variables (e.g. the profiling flags), so building two command trees concurrently is a data race.
	commandTreeMutex sync.Mutex
)

// parseKubectlArguments resolves the argument list of a kubectl invocation into its kubectl command, subcommand,
// flags, positional arguments and the resource types it references. It uses kubectl's own command tree
// (from k8s.io/kubectl), so the result is exactly what kubectl executes.
//
// It returns an error if kubectl would reject the argument list itself, i.e. if it references a command that does not
// exist (wrapping errUnknownKubectlCommand) or a flag that the resolved command does not define.
func parseKubectlArguments(arguments []string) (kubectlArguments, error) {
	// The command parsing machinery from k8s.io/kubectl/pkg/cmd is not meant to be used multiple times, let alone
	// concurrently. For this reason command parsing requires acquiring a lock.
	commandTreeMutex.Lock()
	defer commandTreeMutex.Unlock()

	// The command tree is built from scratch for every incoming command request. (Parsing the flags stores their values
	// in the tree, hence we cannot re-use a parsed command tree.)

	// First, build an "empty" kubectl command struct without passing the argument list. The argument list is not passed
	// here to NewKubectlCommand, but passed below via root.Find. Reason: kubectl only applies the user preferences of a
	// kuberc file (aliases and default flag values) when NewKubectlCommand receives the argument list. The kubectl
	// subprocess has user preferences and kuberc disabled as well (see kubectlEnv).
	root := kubectlcmd.NewKubectlCommand(kubectlcmd.KubectlOptions{
		Arguments: []string{kubectlCommand},
		IOStreams: genericiooptions.IOStreams{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard},
	})

	addKlogFlagStandIns(root.PersistentFlags())
	root.DisableSuggestions = true

	// This is where we actually hand over the arguments from the command request to kubectl's command line parsing
	// machinery. The following lines mirror cobra's Command.ExecuteC, which kubectl's main function invokes. That is:
	// - resolve the command via Find,
	// - register the help flag,
	// - then parse the remaining arguments with the flags of the resolved command.
	resolved, remainingArguments, err := findCommand(root, arguments)
	if err != nil {
		return kubectlArguments{}, err
	}
	if resolved.DisableFlagParsing {
		// No allowed kubectl command disables flag parsing. A command that does that is therefore implicitly disallowed.
		// Allowing commands which use DisableFlagParsing would receive its flags as positional arguments, which invalidates
		// assumptions that validation.go makes.
		return kubectlArguments{}, fmt.Errorf("the kubectl command %q does not support flag parsing", resolved.CommandPath())
	}
	// Required so ParseFlags does not reject --help and -h as unknown flags.
	resolved.InitDefaultHelpFlag()
	if err = resolved.ParseFlags(remainingArguments); err != nil {
		return kubectlArguments{}, err
	}

	parsed := kubectlArguments{
		positionalArguments:    resolved.Flags().Args(),
		hasEndOfFlagsSeparator: resolved.Flags().ArgsLenAtDash() >= 0,
	}
	commandPath := commandPathBelowRoot(resolved)
	if len(commandPath) > 0 {
		parsed.kubectlCommand = commandPath[0]
		parsed.subcommand = strings.Join(commandPath[1:], " ")
	}
	resolved.Flags().Visit(func(flag *pflag.Flag) {
		parsed.flags = append(parsed.flags, parsedFlag{
			longName:  flag.Name,
			shorthand: flag.Shorthand,
			value:     flag.Value.String(),
		})
	})
	for positionalIndex, argument := range parsed.positionalArguments {
		parsed.resourceTypes =
			append(parsed.resourceTypes, extractNormalizedResourceTypes(argument, positionalIndex == 0)...)
	}
	return parsed, nil
}

// findCommand resolves the argument list to a command of the given command tree the way cobra's Command.ExecuteC does.
func findCommand(root *cobra.Command, arguments []string) (*cobra.Command, []string, error) {
	// If kubectl ever starts setting TraverseChildren we will need to handle this differently. Currently kubectl does not
	// use TraverseChildren.
	if root.TraverseChildren {
		return nil, nil, fmt.Errorf(
			"the root command %q sets TraverseChildren, which parseKubectlArguments does not support",
			root.Name(),
		)
	}
	resolved, remainingArguments, err := root.Find(arguments)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errUnknownKubectlCommand, err)
	}
	return resolved, remainingArguments, nil
}

// commandPathBelowRoot returns the canonical names of the given command and its ancestors, without the root command
// ("kubectl") itself, e.g. ["auth", "can-i"] for "kubectl auth can-i". An alias used in the argument list resolves to
// the canonical name. The result is empty for the root command (e.g. a bare kubectl invocation).
func commandPathBelowRoot(command *cobra.Command) []string {
	var path []string
	for current := command; current.HasParent(); current = current.Parent() {
		path = append(path, current.Name())
	}
	slices.Reverse(path)
	return path
}

// extractNormalizedResourceTypes returns the normalized resource types a single positional argument references. The
// argument may be a comma-separated list of resources (e.g. "secret,configmap"), and each entry either a bare resource
// type or a type/name pair (e.g. "secret/my-secret"). A bare resource type only denotes a resource type in the first
// positional argument after the kubectl command (the resource type slot, isResourceTypeSlot), while a type/name pair
// does so in any slot, since `kubectl get secret/a pod/b` lists multiple pairs.
func extractNormalizedResourceTypes(argument string, isResourceTypeSlot bool) []string {
	var resourceTypes []string
	for _, part := range strings.Split(argument, ",") {
		resourceType, _, isTypeNamePair := strings.Cut(part, "/")
		if !isTypeNamePair && !isResourceTypeSlot {
			continue
		}
		resourceTypes = append(resourceTypes, normalizeResourceType(resourceType))
	}
	return resourceTypes
}

// normalizeResourceType trims surrounding whitespace, lower-cases a resource type and strips the API group/version
// suffix of fully qualified forms: "secrets.v1." -> "secrets", "Dash0Monitorings.operator.dash0.com" ->
// "dash0monitorings". Whitespace is trimmed so that a padded form such as " secret" is matched by the resource type
// lists of validation.go rather than being left to the API server to reject.
func normalizeResourceType(resourceType string) string {
	resourceType = strings.ToLower(strings.TrimSpace(resourceType))
	if idx := strings.Index(resourceType, "."); idx >= 0 {
		resourceType = resourceType[:idx]
	}
	return resourceType
}

// addKlogFlagStandIns overrides the flags that component-base's logs.AddFlags adds to kubectl's root command, with the
// same names, shorthands and parsing behavior; but with values that are not bound to the global state of klog and
// component-base.
// Without this, logs.AddFlags itself would hand out flags whose values write directly to the global logging
// configuration of the connector process, so parsing a command request with -v or --vmodule would change global state
// (before validation.go rejects the command based on the presence of -v).
func addKlogFlagStandIns(flags *pflag.FlagSet) {
	klogFlags := pflag.NewFlagSet("klog", pflag.ContinueOnError)
	logs.AddFlags(klogFlags)
	klogFlags.VisitAll(func(flag *pflag.Flag) {
		if flags.Lookup(flag.Name) != nil {
			return
		}
		standIn := *flag
		standIn.Value = &standInFlagValue{value: flag.DefValue, valueType: flag.Value.Type()}
		flags.AddFlag(&standIn)
	})
}

// standInFlagValue is a pflag.Value that only records the string it is set to.
type standInFlagValue struct {
	value     string
	valueType string
}

func (v *standInFlagValue) String() string {
	return v.value
}

func (v *standInFlagValue) Set(value string) error {
	v.value = value
	return nil
}

func (v *standInFlagValue) Type() string {
	return v.valueType
}
