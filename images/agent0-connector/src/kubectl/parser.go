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
	// package-level variables (e.g. the profiling flags), and resolving a command merges the persistent flags of its
	// parents into its flag set, so neither is safe for concurrent use.
	commandTreeMutex sync.Mutex

	// commandTree is kubectl's command tree, built once lazily by the first call to parseKubectlArguments and guarded by
	// commandTreeMutex. It is only used to resolve commands and to look up the flags they define. Flag values are parsed
	// into a separate flag set per command request (see newRequestFlagSet), so no state carries over between requests.
	// The tree is not built per request, because building it registers flag completion functions in a package-level map
	// of cobra that is never pruned. If we built a new command tree for every request, the tree would never be garbage
	// collected and stay in memory for the lifetime of the process, leading to a memory leak.
	commandTree *cobra.Command
)

const (
	helpFlagName      = "help"
	helpFlagShorthand = "h"
)

// parseKubectlArguments resolves the argument list of a kubectl invocation into its kubectl command, subcommand,
// flags, positional arguments and the resource types it references. It uses kubectl's own command tree
// (from k8s.io/kubectl), so the result is exactly what kubectl executes.
//
// It returns an error if kubectl would reject the argument list itself, i.e. if it references a command that does not
// exist (wrapping errUnknownKubectlCommand) or a flag that the resolved command does not define.
func parseKubectlArguments(arguments []string) (kubectlArguments, error) {
	commandTreeMutex.Lock()
	defer commandTreeMutex.Unlock()

	if commandTree == nil {
		// Initialize the singleton kubectl command tree lazily.
		commandTree = newKubectlCommandTree()
	}

	// This is where we actually hand over the arguments from the command request to kubectl's command line parsing
	// machinery. The following lines mirror cobra's Command.ExecuteC, which kubectl's main function invokes.
	resolved, remainingArguments, err := findCommand(commandTree, arguments)
	if err != nil {
		return kubectlArguments{}, err
	}

	flags, err := newRequestFlagSet(resolved)
	if err != nil {
		return kubectlArguments{}, err
	}
	if err = flags.Parse(remainingArguments); err != nil {
		return kubectlArguments{}, err
	}

	parsed := kubectlArguments{
		positionalArguments:    flags.Args(),
		hasEndOfFlagsSeparator: flags.ArgsLenAtDash() >= 0,
	}
	commandPath := commandPathBelowRoot(resolved)
	if len(commandPath) > 0 {
		parsed.kubectlCommand = commandPath[0]
		parsed.subcommand = strings.Join(commandPath[1:], " ")
	}
	flags.Visit(func(flag *pflag.Flag) {
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

// newKubectlCommandTree builds kubectl's command tree the way the kubectl binary does, in a way that makes it resolve
// argument lists in the same way as the kubectl subprocess. Additionally, it takes precautions to avoid writing to the
// global state of the connector process when using the resulting Command to parse an argument list.
func newKubectlCommandTree() *cobra.Command {
	// Without an argument list beyond the executable name, NewKubectlCommand applies no user preferences from a kuberc
	// file (aliases and default flag values). This matches the kubectl subprocess, which has kuberc disabled (see
	// kubectlEnv).
	root := kubectlcmd.NewKubectlCommand(kubectlcmd.KubectlOptions{
		Arguments: []string{kubectlCommand},
		IOStreams: genericiooptions.IOStreams{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard},
	})
	addKlogFlagStandIns(root.PersistentFlags())
	root.DisableSuggestions = true
	return root
}

// newRequestFlagSet returns a flag set for parsing the flags of one command request for the given resolved command. It
// mirrors the flag set that cobra's Command.ParseFlags parses into: the flags of the command including the persistent
// flags of its parents and the help flag, the same normalization function and the same allowlist of tolerated parse
// errors. Each flag is a copy of the command's flag with the same name, shorthand and NoOptDefVal (which together
// determine how pflag splits the argument list), but with a stand-in value, so parsing never writes to the command
// tree or to the variables kubectl binds its flags to.
func newRequestFlagSet(resolved *cobra.Command) (*pflag.FlagSet, error) {
	// InheritedFlags merges the persistent flags of the parents into resolved.Flags(), as cobra's ParseFlags does.
	resolved.InheritedFlags()

	commandFlags := resolved.Flags()
	flags := pflag.NewFlagSet(resolved.Name(), pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.SetNormalizeFunc(commandFlags.GetNormalizeFunc())
	flags.ParseErrorsAllowlist = pflag.ParseErrorsAllowlist(resolved.FParseErrWhitelist)
	commandFlags.VisitAll(func(flag *pflag.Flag) {
		flags.AddFlag(newStandInFlag(flag))
	})

	// The help flag is only added to the returned flag set, never to the command tree. cobra's ExecuteC adds it to the
	// resolved command after resolving it, and resolving takes the flags known at that time into account (whether "-h x"
	// means "-h" plus the argument "x" or "-h" with the value "x"). Adding it to the command tree would make the command
	// resolution of later requests differ from the kubectl subprocess.
	if flags.Lookup(helpFlagName) == nil {
		// Mirrors cobra's Command.InitDefaultHelpFlag.
		if flags.ShorthandLookup(helpFlagShorthand) != nil {
			return nil, fmt.Errorf(
				"the kubectl command %q uses the shorthand -%s, which cobra reserves for --%s",
				resolved.CommandPath(),
				helpFlagShorthand,
				helpFlagName,
			)
		}
		flags.BoolP(helpFlagName, helpFlagShorthand, false, "")
	}
	return flags, nil
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

	if resolved.DisableFlagParsing {
		// No allowed kubectl command disables flag parsing. Allowing commands which use DisableFlagParsing would receive
		// its flags as positional arguments, which invalidates assumptions that validation.go makes. To avoid that
		// situation, a command that disables flag parsing is therefore explicitly rejected here.
		return nil, nil, fmt.Errorf("the kubectl command %q does not support flag parsing", resolved.CommandPath())
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

// addKlogFlagStandIns adds the flags that component-base's logs.AddFlags adds to kubectl's root command, with the
// same names, shorthands and parsing behavior; but with values that are not bound to the global state of klog and
// component-base. Without this, logs.AddFlags itself would hand out flags whose values write directly to the global
// logging configuration of the connector process.
func addKlogFlagStandIns(flags *pflag.FlagSet) {
	klogFlags := pflag.NewFlagSet("klog", pflag.ContinueOnError)
	logs.AddFlags(klogFlags)
	klogFlags.VisitAll(func(flag *pflag.Flag) {
		if flags.Lookup(flag.Name) != nil {
			return
		}
		flags.AddFlag(newStandInFlag(flag))
	})
}

// newStandInFlag returns a copy of the given flag whose value only records the string it is set to, starting with the
// flag's default value.
func newStandInFlag(flag *pflag.Flag) *pflag.Flag {
	standIn := *flag
	standIn.Value = &standInFlagValue{value: flag.DefValue, valueType: flag.Value.Type()}
	// The copy is always unchanged: pflag only records a flag as set (i.e. Visit reports it) if the flag is still
	// unchanged when the argument list sets it. A copy of a changed flag would be invisible to the validation even when
	// the argument list sets it.
	standIn.Changed = false
	return &standIn
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
