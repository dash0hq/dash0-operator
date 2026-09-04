// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package dash0telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/dash0hq/dash0-operator/images/pkg/nodeuid"
)

// stubResolvedNodeUID makes the prefetched lookup resolve to the given UID for the duration of the test. An empty UID
// makes it fail, which is how the shared lookup reports every error.
func stubResolvedNodeUID(t *testing.T, uid string) {
	t.Helper()
	t.Cleanup(nodeuid.SetResolverForTest(func(context.Context, string) string { return uid }))
}

func TestAddNodeUID(t *testing.T) {
	tests := []struct {
		name        string
		resolvedUID string
		nodeName    string
		presetUID   string
		expectedUID string // "" means the attribute must be absent afterwards
	}{
		{
			name:        "sets attribute from resolved uid",
			resolvedUID: "uid-a",
			nodeName:    "node-a",
			expectedUID: "uid-a",
		},
		{
			name:        "no node name means no lookup and no attribute",
			resolvedUID: "uid-a",
			nodeName:    "",
			expectedUID: "",
		},
		{
			// The shared lookup reports every failure as an empty string, after logging the reason itself.
			name:        "unresolved uid is best effort",
			resolvedUID: "",
			nodeName:    "node-a",
			expectedUID: "",
		},
		{
			name:        "does not override existing attribute",
			resolvedUID: "uid-a",
			nodeName:    "node-a",
			presetUID:   "preset-uid",
			expectedUID: "preset-uid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(nodeNameEnvVarName, tc.nodeName)
			stubResolvedNodeUID(t, tc.resolvedUID)

			res := pcommon.NewResource()
			if tc.presetUID != "" {
				res.Attributes().PutStr(k8sNodeUIDResourceAttrKey, tc.presetUID)
			}

			startNodeUIDPrefetch(context.Background())
			addNodeUID(res)

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
	t.Setenv(nodeNameEnvVarName, "node-a")
	release := make(chan struct{})
	defer close(release)
	t.Cleanup(nodeuid.SetResolverForTest(func(context.Context, string) string {
		<-release
		return "uid-a"
	}))

	original := nodeUIDStartupWaitTimeout
	nodeUIDStartupWaitTimeout = 10 * time.Millisecond
	t.Cleanup(func() { nodeUIDStartupWaitTimeout = original })

	startNodeUIDPrefetch(context.Background())

	res := pcommon.NewResource()
	start := time.Now()
	addNodeUID(res)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("addNodeUID blocked for %s, expected it to give up after ~%s", elapsed, nodeUIDStartupWaitTimeout)
	}
	if _, ok := res.Attributes().Get(k8sNodeUIDResourceAttrKey); ok {
		t.Fatalf("expected %s to be absent when the lookup does not finish in time", k8sNodeUIDResourceAttrKey)
	}
}

// TestReloadKeepsTheNodeUID covers the configuration reload path: the collector builds a new factory, and therefore
// prefetches again, on every reload, but the attribute has to stay on the resource each time.
func TestReloadKeepsTheNodeUID(t *testing.T) {
	t.Setenv(nodeNameEnvVarName, "node-a")
	stubResolvedNodeUID(t, "uid-a")

	for reload := range 3 {
		startNodeUIDPrefetch(context.Background())
		res := pcommon.NewResource()
		addNodeUID(res)

		value, ok := res.Attributes().Get(k8sNodeUIDResourceAttrKey)
		if !ok {
			t.Fatalf("reload %d: expected %s to be set", reload, k8sNodeUIDResourceAttrKey)
		}
		if value.Str() != "uid-a" {
			t.Fatalf("reload %d: expected uid-a, got %q", reload, value.Str())
		}
	}
}
