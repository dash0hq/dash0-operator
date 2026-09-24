// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const (
	helmChartValuesFile  = "../../../../helm-chart/dash0-operator/values.yaml"
	helmChartHelpersFile = "../../../../helm-chart/dash0-operator/templates/_helpers.tpl"
)

// defaultKubectlCommands are the kubectl commands the Helm chart allows by default, read from
// operator.agent0Connector.allowedKubectlCommands in helm-chart/dash0-operator/values.yaml.
var defaultKubectlCommands = mustParseAllowedKubectlCommands(strings.Join(helmChartDefaultKubectlCommands(true), ","))

func TestParseAllowedKubectlCommands(t *testing.T) {
	tests := []struct {
		name            string
		value           string
		expectedAllowed []string
	}{
		{name: "single command", value: "get", expectedAllowed: []string{"get"}},
		{name: "several commands", value: "logs,get,events", expectedAllowed: []string{"events", "get", "logs"}},
		{name: "whitespace around entries is tolerated", value: " get , logs\t",
			expectedAllowed: []string{"get", "logs"}},
		{name: "duplicates are tolerated", value: "get,get", expectedAllowed: []string{"get"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, err := ParseAllowedKubectlCommands(tt.value)
			if err != nil {
				t.Fatalf("expected the value %q to be parsed, but it was rejected: %v", tt.value, err)
			}
			assertAllowedKubectlCommands(t, allowed, tt.expectedAllowed, "")
		})
	}
}

func TestParseAllowedKubectlCommandsRejectsInvalidValues(t *testing.T) {
	notSetOrEmpty := "the environment variable DASH0_AGENT0_CONNECTOR_ALLOWED_KUBECTL_COMMANDS is not set or empty, " +
		"at least one kubectl command needs to be allowed"
	emptyEntry := func(value string) string {
		return fmt.Sprintf(
			"the environment variable DASH0_AGENT0_CONNECTOR_ALLOWED_KUBECTL_COMMANDS contains an empty entry: %q",
			value,
		)
	}
	unsupported := func(kubectlCommands string) string {
		return "the environment variable DASH0_AGENT0_CONNECTOR_ALLOWED_KUBECTL_COMMANDS contains kubectl commands " +
			"that the agent0-connector does not support: " + kubectlCommands
	}
	tests := []struct {
		name          string
		value         string
		expectedError string
	}{
		{name: "empty value", value: "", expectedError: notSetOrEmpty},
		{name: "only whitespace", value: " \t ", expectedError: notSetOrEmpty},
		{name: "only a comma", value: ",", expectedError: emptyEntry(",")},
		{name: "empty entry", value: "get,,logs", expectedError: emptyEntry("get,,logs")},
		{name: "trailing comma", value: "get,", expectedError: emptyEntry("get,")},
		{name: "blank entry", value: "get, ,logs", expectedError: emptyEntry("get, ,logs")},
		{name: "describe", value: "get,describe", expectedError: unsupported("describe")},
		{name: "only unsupported commands", value: "delete", expectedError: unsupported("delete")},
		{name: "every invalid command is listed", value: "get,delete,describe,exec,cluster-info dump",
			expectedError: unsupported("delete, describe, exec, cluster-info dump")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAllowedKubectlCommands(tt.value)
			if err == nil {
				t.Fatalf("expected the value %q to be rejected, but it was parsed", tt.value)
			}
			if err.Error() != tt.expectedError {
				t.Errorf("expected the error\n\t%s\ngot\n\t%s", tt.expectedError, err)
			}
		})
	}
}

func TestRenderAllowedKubectlCommandsHumanReadable(t *testing.T) {
	tests := []struct {
		kubectlCommands []string
		expected        string
	}{
		{kubectlCommands: []string{"get"}, expected: `the only allowed kubectl command is "get"`},
		{kubectlCommands: []string{"get", "top"}, expected: `the only allowed kubectl commands are "get" and "top"`},
		{kubectlCommands: []string{"auth", "get", "top"},
			expected: `the only allowed kubectl commands are "auth", "get" and "top"`},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := renderAllowedKubectlCommandsHumanReadable(tt.kubectlCommands); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

// TestHelmChartListsEverySupportedKubectlCommand guards against drift between the kubectl commands the connector
// supports and the ones the Helm chart lists and accepts in operator.agent0Connector.allowedKubectlCommands. A command
// that the Helm chart accepts but the connector does not support makes the connector terminate on startup.
func TestHelmChartListsEverySupportedKubectlCommand(t *testing.T) {
	configurable := configurableKubectlCommands()

	t.Run("the default values", func(t *testing.T) {
		if listed := helmChartDefaultKubectlCommands(false); !slices.Equal(listed, configurable) {
			t.Errorf(
				"operator.agent0Connector.allowedKubectlCommands in %s lists %v, but the configurable kubectl commands are %v",
				helmChartValuesFile,
				listed,
				configurable,
			)
		}
	})

	t.Run("the validation of the Helm value", func(t *testing.T) {
		content, err := os.ReadFile(helmChartHelpersFile)
		if err != nil {
			t.Fatalf("cannot read %s: %v", helmChartHelpersFile, err)
		}
		match := regexp.MustCompile(`\$supportedCommands := list ([^}]*)}}`).FindSubmatch(content)
		if match == nil {
			t.Fatalf("cannot find the list of supported kubectl commands in %s", helmChartHelpersFile)
		}
		fields := strings.Fields(string(match[1]))
		listed := make([]string, 0, len(fields))
		for _, quoted := range fields {
			listed = append(listed, strings.Trim(quoted, `"`))
		}
		slices.Sort(listed)
		if !slices.Equal(listed, configurable) {
			t.Errorf(
				"the Helm chart accepts the kubectl commands %v in operator.agent0Connector.allowedKubectlCommands (see "+
					"%s), but the configurable kubectl commands are %v",
				listed,
				helmChartHelpersFile,
				configurable,
			)
		}
	})
}

// assertAllowedKubectlCommands verifies that allowed contains exactly the expected kubectl commands.
func assertAllowedKubectlCommands(
	t *testing.T,
	allowed AllowedKubectlCommands,
	expected []string,
	messagePrefix string,
) {
	t.Helper()
	if len(allowed.commands) != len(expected) {
		t.Errorf(
			"%sexpected %d allowed kubectl commands (%v), got %d (%v)",
			messagePrefix,
			len(expected),
			expected,
			len(allowed.commands),
			slices.Sorted(maps.Keys(allowed.commands)),
		)
	}
	for _, kubectlCmd := range expected {
		if _, ok := allowed.commands[kubectlCmd]; !ok {
			t.Errorf("%sexpected the kubectl command %q to be allowed", messagePrefix, kubectlCmd)
		}
	}
	for kubectlCmd := range allowed.commands {
		if !slices.Contains(expected, kubectlCmd) {
			t.Errorf("%sexpected the kubectl command %q to not be allowed", messagePrefix, kubectlCmd)
		}
	}
}

// everySupportedKubectlCommandAllowed returns the configuration that enables every kubectl command the configuration
// can enable, so that tests of the individual checks do not depend on the defaults.
func everySupportedKubectlCommandAllowed() AllowedKubectlCommands {
	return mustParseAllowedKubectlCommands(strings.Join(configurableKubectlCommands(), ","))
}

// configurableKubectlCommands returns the sorted list of supported kubectl commands that are not rejected
// unconditionally.
func configurableKubectlCommands() []string {
	var configurable []string
	for kubectlCmd := range supportedKubectlCommands {
		if _, rejected := unconditionallyRejectedKubectlCommands[kubectlCmd]; !rejected {
			configurable = append(configurable, kubectlCmd)
		}
	}
	slices.Sort(configurable)
	return configurable
}

func mustParseAllowedKubectlCommands(value string) AllowedKubectlCommands {
	allowed, err := ParseAllowedKubectlCommands(value)
	if err != nil {
		panic(err)
	}
	return allowed
}

// helmChartDefaultKubectlCommands returns the sorted keys of operator.agent0Connector.allowedKubectlCommands in the
// default values of the Helm chart. With onlyEnabled, only the kubectl commands that are enabled by default are
// returned.
func helmChartDefaultKubectlCommands(onlyEnabled bool) []string {
	content, err := os.ReadFile(helmChartValuesFile)
	if err != nil {
		panic(fmt.Sprintf("cannot read %s: %v", helmChartValuesFile, err))
	}
	var values struct {
		Operator struct {
			Agent0Connector struct {
				AllowedKubectlCommands map[string]bool `json:"allowedKubectlCommands"`
			} `json:"agent0Connector"`
		} `json:"operator"`
	}
	if err := yaml.Unmarshal(content, &values); err != nil {
		panic(fmt.Sprintf("cannot parse %s: %v", helmChartValuesFile, err))
	}
	var kubectlCommands []string
	for kubectlCmd, enabled := range values.Operator.Agent0Connector.AllowedKubectlCommands {
		if enabled || !onlyEnabled {
			kubectlCommands = append(kubectlCommands, kubectlCmd)
		}
	}
	slices.Sort(kubectlCommands)
	return kubectlCommands
}
