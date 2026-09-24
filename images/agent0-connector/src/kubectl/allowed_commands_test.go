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

// defaultKubectlCommands is the configuration the connector uses when the operator does not pass one.
var defaultKubectlCommands = DefaultAllowedKubectlCommands()

// everySupportedKubectlCommandAllowed returns the configuration that enables every kubectl command the configuration
// can enable, so that tests of the individual checks do not depend on the defaults.
func everySupportedKubectlCommandAllowed() AllowedKubectlCommands {
	allowed, _ := ParseAllowedKubectlCommands(strings.Join(slices.Sorted(maps.Keys(supportedKubectlCommands)), ","))
	return allowed
}

func TestDefaultAllowedKubectlCommands(t *testing.T) {
	allowed := DefaultAllowedKubectlCommands()
	assertAllowedKubectlCommands(
		t,
		allowed,
		[]string{"api-resources", "api-versions", "auth", "cluster-info", "explain", "get", "top", "version"},
		"",
	)
	for _, kubectlCmd := range []string{"logs", "events", "describe"} {
		if allowed.Allows(kubectlCmd) {
			t.Errorf("expected the kubectl command %q to be disabled by default", kubectlCmd)
		}
	}
}

func TestParseAllowedKubectlCommands(t *testing.T) {
	tests := []struct {
		name            string
		value           string
		expectedAllowed []string
		expectedIgnored []string
	}{
		{name: "empty value allows nothing", value: "", expectedAllowed: nil},
		{name: "single command", value: "get", expectedAllowed: []string{"get"}},
		{name: "several commands", value: "logs,get,events", expectedAllowed: []string{"events", "get", "logs"}},
		{name: "whitespace and empty entries are tolerated", value: " get , ,logs,",
			expectedAllowed: []string{"get", "logs"}},
		{name: "describe cannot be enabled", value: "get,describe", expectedAllowed: []string{"get"},
			expectedIgnored: []string{"describe"}},
		{name: "unknown commands are ignored", value: "get,delete,exec,cluster-info dump",
			expectedAllowed: []string{"get"}, expectedIgnored: []string{"delete", "exec", "cluster-info dump"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, ignored := ParseAllowedKubectlCommands(tt.value)
			assertAllowedKubectlCommands(
				t,
				allowed,
				tt.expectedAllowed,
				"",
			)
			if !slices.Equal(ignored, tt.expectedIgnored) {
				t.Errorf("expected the ignored entries %v, got %v", tt.expectedIgnored, ignored)
			}
		})
	}
}

func TestDescribeAllowedKubectlCommands(t *testing.T) {
	tests := []struct {
		kubectlCommands []string
		expected        string
	}{
		{kubectlCommands: nil, expected: "no kubectl command is allowed by the configuration of the agent0-connector"},
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
// supports and the ones the Helm chart accepts in operator.agent0Connector.allowedKubectlCommands, as well as between
// the defaults of that Helm value and DefaultAllowedKubectlCommands.
func TestHelmChartListsEverySupportedKubectlCommand(t *testing.T) {
	var configurable []string
	for kubectlCmd := range supportedKubectlCommands {
		if _, rejected := unconditionallyRejectedKubectlCommands[kubectlCmd]; !rejected {
			configurable = append(configurable, kubectlCmd)
		}
	}
	slices.Sort(configurable)

	t.Run("the default values", func(t *testing.T) {
		content, err := os.ReadFile(helmChartValuesFile)
		if err != nil {
			t.Fatalf("cannot read %s: %v", helmChartValuesFile, err)
		}
		var values struct {
			Operator struct {
				Agent0Connector struct {
					AllowedKubectlCommands map[string]bool `json:"allowedKubectlCommands"`
				} `json:"agent0Connector"`
			} `json:"operator"`
		}
		if err := yaml.Unmarshal(content, &values); err != nil {
			t.Fatalf("cannot parse %s: %v", helmChartValuesFile, err)
		}
		defaults := values.Operator.Agent0Connector.AllowedKubectlCommands
		if listed := slices.Sorted(maps.Keys(defaults)); !slices.Equal(listed, configurable) {
			t.Errorf(
				"operator.agent0Connector.allowedKubectlCommands in %s lists %v, but the configurable kubectl commands are %v",
				helmChartValuesFile,
				listed,
				configurable,
			)
		}
		var enabledByDefault []string
		for kubectlCmd, enabled := range defaults {
			if enabled {
				enabledByDefault = append(enabledByDefault, kubectlCmd)
			}
		}
		slices.Sort(enabledByDefault)
		assertAllowedKubectlCommands(
			t,
			DefaultAllowedKubectlCommands(),
			enabledByDefault,
			fmt.Sprintf(
				"Drift test: DefaultAllowedKubectlCommands vs. the defaults of operator.agent0Connector.allowedKubectlCommands in %s",
				helmChartValuesFile,
			),
		)
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
