// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// AllowedKubectlCommandsEnvVarName is the environment variable through which the operator passes the comma-separated
// list of kubectl commands the connector may execute (set from the Helm value
// operator.agent0Connector.allowedKubectlCommands). The variable is required.
const AllowedKubectlCommandsEnvVarName = "DASH0_AGENT0_CONNECTOR_ALLOWED_KUBECTL_COMMANDS"

// AllowedKubectlCommands is the set of kubectl commands the connector may execute. It is never empty, always a subset
// of supportedKubectlCommands and never contains a command listed in unconditionallyRejectedKubectlCommands.
type AllowedKubectlCommands struct {
	commands      map[string]struct{}
	humanReadable string
}

// ParseAllowedKubectlCommands parses a comma-separated list of kubectl commands, as passed via
// DASH0_AGENT0_CONNECTOR_ALLOWED_KUBECTL_COMMANDS. Whitespace around an entry is ignored. It returns an error if the
// value is empty, if it contains an empty entry, or if an entry is not a supported kubectl command or names a command
// the connector rejects unconditionally (e.g. "describe"); the error lists every such entry.
func ParseAllowedKubectlCommands(value string) (AllowedKubectlCommands, error) {
	if strings.TrimSpace(value) == "" {
		return AllowedKubectlCommands{}, fmt.Errorf(
			"the environment variable %s is not set or empty, at least one kubectl command needs to be allowed",
			AllowedKubectlCommandsEnvVarName,
		)
	}
	commands := make(map[string]struct{})
	var invalid []string
	for entry := range strings.SplitSeq(value, ",") {
		kubectlCmd := strings.TrimSpace(entry)
		if kubectlCmd == "" {
			return AllowedKubectlCommands{}, fmt.Errorf(
				"the environment variable %s contains an empty entry: %q",
				AllowedKubectlCommandsEnvVarName,
				value,
			)
		}
		_, supported := supportedKubectlCommands[kubectlCmd]
		_, rejected := unconditionallyRejectedKubectlCommands[kubectlCmd]
		if !supported || rejected {
			invalid = append(invalid, kubectlCmd)
			continue
		}
		commands[kubectlCmd] = struct{}{}
	}
	if len(invalid) > 0 {
		return AllowedKubectlCommands{}, fmt.Errorf(
			"the environment variable %s contains kubectl commands that the agent0-connector does not support: %s",
			AllowedKubectlCommandsEnvVarName,
			strings.Join(invalid, ", "),
		)
	}
	if len(commands) == 0 {
		return AllowedKubectlCommands{}, fmt.Errorf(
			"the environment variable %s does not contain any kubectl command, at least one kubectl command needs to "+
				"be allowed",
			AllowedKubectlCommandsEnvVarName,
		)
	}
	return AllowedKubectlCommands{
		commands:      commands,
		humanReadable: renderAllowedKubectlCommandsHumanReadable(slices.Sorted(maps.Keys(commands))),
	}, nil
}

// Allows reports whether the given kubectl command may be executed.
func (a AllowedKubectlCommands) Allows(kubectlCmd string) bool {
	_, allowed := a.commands[kubectlCmd]
	return allowed
}

// String returns the allowed kubectl commands as a sorted, comma-separated list.
func (a AllowedKubectlCommands) String() string {
	return strings.Join(slices.Sorted(maps.Keys(a.commands)), ",")
}

// renderAllowedKubectlCommandsHumanReadable renders the phrase that rejection messages use to tell the calling agent
// which kubectl commands it may use instead.
func renderAllowedKubectlCommandsHumanReadable(sortedKubectlCommands []string) string {
	if len(sortedKubectlCommands) == 1 {
		return fmt.Sprintf("the only allowed kubectl command is %q", sortedKubectlCommands[0])
	}
	quoted := make([]string, 0, len(sortedKubectlCommands))
	for _, kubectlCmd := range sortedKubectlCommands {
		quoted = append(quoted, fmt.Sprintf("%q", kubectlCmd))
	}
	return fmt.Sprintf(
		"the only allowed kubectl commands are %s and %s",
		strings.Join(quoted[:len(quoted)-1], ", "),
		quoted[len(quoted)-1],
	)
}
