// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package dash0telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

// futureWith returns a prefetch channel already holding the given result.
func futureWith(result nodeUIDResult) <-chan nodeUIDResult {
	ch := make(chan nodeUIDResult, 1)
	ch <- result
	return ch
}

func TestAddNodeUID(t *testing.T) {
	tests := []struct {
		name        string
		future      <-chan nodeUIDResult
		presetUID   string
		hasPreset   bool
		expectedUID string // "" means the attribute must be absent afterwards
	}{
		{
			name:        "sets attribute from resolved uid",
			future:      futureWith(nodeUIDResult{uid: "uid-a"}),
			expectedUID: "uid-a",
		},
		{
			name:        "nil future skips (no node name)",
			future:      nil,
			expectedUID: "",
		},
		{
			name:        "lookup failure is best effort",
			future:      futureWith(nodeUIDResult{err: errors.New("boom")}),
			expectedUID: "",
		},
		{
			name:        "empty resolved uid is not set",
			future:      futureWith(nodeUIDResult{uid: ""}),
			expectedUID: "",
		},
		{
			name:        "does not override existing attribute",
			future:      futureWith(nodeUIDResult{uid: "uid-a"}),
			presetUID:   "preset-uid",
			hasPreset:   true,
			expectedUID: "preset-uid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := pcommon.NewResource()
			if tc.hasPreset {
				res.Attributes().PutStr(k8sNodeUIDResourceAttrKey, tc.presetUID)
			}

			addNodeUID(context.Background(), res, tc.future)

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

func TestAddNodeUIDDoesNotBlockPastWaitTimeout(t *testing.T) {
	// A future that never receives a result simulates a lookup that has not finished yet.
	neverResolves := make(chan nodeUIDResult)

	original := nodeUIDStartupWaitTimeout
	nodeUIDStartupWaitTimeout = 10 * time.Millisecond
	t.Cleanup(func() { nodeUIDStartupWaitTimeout = original })

	res := pcommon.NewResource()
	start := time.Now()
	addNodeUID(context.Background(), res, neverResolves)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("addNodeUID blocked for %s, expected it to give up after ~%s", elapsed, nodeUIDStartupWaitTimeout)
	}
	if _, ok := res.Attributes().Get(k8sNodeUIDResourceAttrKey); ok {
		t.Fatalf("expected %s to be absent when the lookup does not finish in time", k8sNodeUIDResourceAttrKey)
	}
}

func TestAddNodeUIDRespectsContextCancellation(t *testing.T) {
	neverResolves := make(chan nodeUIDResult)

	// A long wait timeout ensures the context cancellation, not the deadline, ends the wait.
	original := nodeUIDStartupWaitTimeout
	nodeUIDStartupWaitTimeout = time.Hour
	t.Cleanup(func() { nodeUIDStartupWaitTimeout = original })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := pcommon.NewResource()
	start := time.Now()
	addNodeUID(ctx, res, neverResolves)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("addNodeUID blocked for %s, expected it to return promptly on context cancellation", elapsed)
	}
	if _, ok := res.Attributes().Get(k8sNodeUIDResourceAttrKey); ok {
		t.Fatalf("expected %s to be absent when the context is cancelled", k8sNodeUIDResourceAttrKey)
	}
}

func TestStartNodeUIDPrefetch(t *testing.T) {
	t.Run("returns nil when node name is not set", func(t *testing.T) {
		t.Setenv(nodeNameEnvVarName, "")
		if future := startNodeUIDPrefetch(context.Background()); future != nil {
			t.Fatalf("expected nil future when node name is not set, got %v", future)
		}
	})

	t.Run("resolves the node uid via the resolver", func(t *testing.T) {
		t.Setenv(nodeNameEnvVarName, "node-a")
		original := nodeUIDResolver
		nodeUIDResolver = func(_ context.Context, nodeName string) (string, error) {
			if nodeName != "node-a" {
				t.Errorf("expected node name node-a, got %q", nodeName)
			}
			return "uid-a", nil
		}
		t.Cleanup(func() { nodeUIDResolver = original })

		future := startNodeUIDPrefetch(context.Background())
		if future == nil {
			t.Fatal("expected a non-nil future when the node name is set")
		}
		select {
		case result := <-future:
			if result.err != nil {
				t.Fatalf("unexpected error: %v", result.err)
			}
			if result.uid != "uid-a" {
				t.Fatalf("expected uid-a, got %q", result.uid)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for the prefetch result")
		}
	})
}
