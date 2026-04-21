// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"
)

func TestDeriveDecisionMakerEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard Dash0 ingress endpoint",
			input:    "ingress.eu-central-1.aws.dash0.com:4317",
			expected: "decision-maker.eu-central-1.aws.dash0.com:443",
		},
		{
			name:     "no ingress prefix",
			input:    "custom-endpoint.example.com:4317",
			expected: "custom-endpoint.example.com:443",
		},
		{
			name:     "no port 4317",
			input:    "ingress.dash0.com:443",
			expected: "decision-maker.dash0.com:443",
		},
		{
			name:     "neither ingress prefix nor port 4317",
			input:    "some-host.example.com:9090",
			expected: "some-host.example.com:9090",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DeriveDecisionMakerEndpoint(tt.input)
			if result != tt.expected {
				t.Errorf("DeriveDecisionMakerEndpoint(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
