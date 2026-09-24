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
// operator.agent0Connector.allowedKubectlCommands). When the variable is absent, DefaultAllowedKubectlCommands applies.
const AllowedKubectlCommandsEnvVarName = "DASH0_AGENT0_CONNECTOR_ALLOWED_KUBECTL_COMMANDS"

// disabledByDefaultKubectlCommands lists the supported kubectl commands that are only allowed when the configuration
// enables them explicitly. This mirrors the defaults of
// operator.agent0Connector.allowedKubectlCommands in helm-chart/dash0-operator/values.yaml.
var disabledByDefaultKubectlCommands = map[string]struct{}{
	"events": {},
	"logs":   {},
}

// AllowedKubectlCommands is the set of kubectl commands the connector may execute. It is always a subset of
// supportedKubectlCommands and never contains a command listed in unconditionallyRejectedKubectlCommands.
type AllowedKubectlCommands struct {
	commands      map[string]struct{}
	humanReadable string
}

// DefaultAllowedKubectlCommands returns the kubectl commands the connector executes when the configuration does not
// list them explicitly: every supported kubectl command except for the ones in disabledByDefaultKubectlCommands.
func DefaultAllowedKubectlCommands() AllowedKubectlCommands {
	var kubectlCommands []string
	for kubectlCmd := range supportedKubectlCommands {
		if _, disabledByDefault := disabledByDefaultKubectlCommands[kubectlCmd]; !disabledByDefault {
			kubectlCommands = append(kubectlCommands, kubectlCmd)
		}
	}
	allowed, _ := newAllowedKubectlCommands(kubectlCommands)
	return allowed
}

// ParseAllowedKubectlCommands parses a comma-separated list of kubectl commands, as passed via
// DASH0_AGENT0_CONNECTOR_ALLOWED_KUBECTL_COMMANDS. An empty value allows no kubectl command at all. Entries that are
// not a supported kubectl command, or that name a command the connector rejects unconditionally (e.g. "describe"), are
// left out of the result and returned as the second return value.
func ParseAllowedKubectlCommands(value string) (AllowedKubectlCommands, []string) {
	var kubectlCommands []string
	for entry := range strings.SplitSeq(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry != "" {
			kubectlCommands = append(kubectlCommands, entry)
		}
	}
	return newAllowedKubectlCommands(kubectlCommands)
}

func newAllowedKubectlCommands(kubectlCommands []string) (AllowedKubectlCommands, []string) {
	commands := make(map[string]struct{}, len(kubectlCommands))
	var ignored []string
	for _, kubectlCmd := range kubectlCommands {
		_, supported := supportedKubectlCommands[kubectlCmd]
		_, rejected := unconditionallyRejectedKubectlCommands[kubectlCmd]
		if !supported || rejected {
			ignored = append(ignored, kubectlCmd)
			continue
		}
		commands[kubectlCmd] = struct{}{}
	}
	return AllowedKubectlCommands{
		commands:      commands,
		humanReadable: renderAllowedKubectlCommandsHumanReadable(slices.Sorted(maps.Keys(commands))),
	}, ignored
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
	switch len(sortedKubectlCommands) {
	case 0:
		return "no kubectl command is allowed by the configuration of the agent0-connector"
	case 1:
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
