// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.

package common

import (
	"errors"
	"testing"
)

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		want      string
	}{
		{
			name:      "empty string, length 0",
			input:     "",
			maxLength: 0,
			want:      "",
		},
		{
			name:      "empty string, length > 0",
			input:     "",
			maxLength: 10,
			want:      "",
		},
		{
			name:      "empty string, negative length",
			input:     "",
			maxLength: -10,
			want:      "",
		},
		{
			name:      "non-empty string, negative length",
			input:     "abcdefg",
			maxLength: -10,
			want:      "",
		},
		{
			name:      "string shorter than max length",
			input:     "abcdefg",
			maxLength: 10,
			want:      "abcdefg",
		},
		{
			name:      "string has max length",
			input:     "abcdefg",
			maxLength: 7,
			want:      "abcdefg",
		},
		{
			name:      "string too long",
			input:     "abcdefghijklmn",
			maxLength: 7,
			want:      "abcdefg",
		},
		{
			name:      "shorten to length 0",
			input:     "abcdefghijklmn",
			maxLength: 0,
			want:      "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateString(tt.input, tt.maxLength); got != tt.want {
				t.Errorf("TruncateString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateError(t *testing.T) {
	tests := []struct {
		name  string
		input error
		want  string
	}{
		{
			name:  "nil error",
			input: nil,
			want:  "nil",
		},
		{
			name:  "error with empty message",
			input: errors.New(""),
			want:  "",
		},
		{
			name:  "error with short message",
			input: errors.New("short error message"),
			want:  "short error message",
		},
		{
			name:  "error with long message",
			input: errors.New("cannot store offset files in configmap operator-namespace/dash0-operator-filelogoffsets-cm: cannot update 'some-node-name' field of configuration map  operator-namespace/dash0-operator-filelogoffsets-cm: etcdserver: request timed out; merge patch sent: '{\"binaryData\":{\"some-node-name\":\"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"),
			want:  "cannot store offset files in configmap operator-namespace/dash0-operator-filelog",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateError(tt.input); got != tt.want {
				t.Errorf("TruncateString():\nwant: \"%v\"\ngot:  \"%v\", ", tt.want, got)
			}
		})
	}
}
