// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package dash0telemetry

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

func TestAddNodeUID(t *testing.T) {
	tests := []struct {
		name        string
		nodeName    string
		presetUID   string
		hasPreset   bool
		resolver    func(context.Context, string) (string, error)
		expectedUID string // "" means the attribute must be absent afterwards
	}{
		{
			name:     "sets attribute from resolver",
			nodeName: "node-a",
			resolver: func(_ context.Context, nodeName string) (string, error) {
				if nodeName != "node-a" {
					t.Errorf("expected node name node-a, got %q", nodeName)
				}
				return "uid-a", nil
			},
			expectedUID: "uid-a",
		},
		{
			name:     "no node name skips lookup",
			nodeName: "",
			resolver: func(context.Context, string) (string, error) {
				t.Error("resolver must not be called when the node name is empty")
				return "", nil
			},
			expectedUID: "",
		},
		{
			name:     "lookup failure is best effort",
			nodeName: "node-a",
			resolver: func(context.Context, string) (string, error) {
				return "", errors.New("boom")
			},
			expectedUID: "",
		},
		{
			name:      "does not override existing attribute",
			nodeName:  "node-a",
			presetUID: "preset-uid",
			hasPreset: true,
			resolver: func(context.Context, string) (string, error) {
				t.Error("resolver must not be called when the attribute is already set")
				return "", nil
			},
			expectedUID: "preset-uid",
		},
		{
			name:     "empty resolved uid is not set",
			nodeName: "node-a",
			resolver: func(context.Context, string) (string, error) {
				return "", nil
			},
			expectedUID: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(nodeNameEnvVarName, tc.nodeName)
			original := nodeUIDResolver
			nodeUIDResolver = tc.resolver
			t.Cleanup(func() { nodeUIDResolver = original })

			res := pcommon.NewResource()
			if tc.hasPreset {
				res.Attributes().PutStr(k8sNodeUIDResourceAttrKey, tc.presetUID)
			}

			addNodeUID(context.Background(), res)

			value, ok := res.Attributes().Get(k8sNodeUIDResourceAttrKey)
			if tc.expectedUID == "" {
				if ok {
					t.Fatalf("expected %s to be absent, got %q", k8sNodeUIDResourceAttrKey, value.Str())
				}
				return
			}
			if !ok {
				t.Fatalf("expected %s to be set to %q", k8sNodeUIDResourceAttrKey, tc.expectedUID)
			}
			if value.Str() != tc.expectedUID {
				t.Fatalf("expected %q, got %q", tc.expectedUID, value.Str())
			}
		})
	}
}
