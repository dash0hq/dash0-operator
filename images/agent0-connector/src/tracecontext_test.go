// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
)

func TestParseTraceparent(t *testing.T) {
	t.Run("valid traceparent", func(t *testing.T) {
		tc := parseTraceparent("00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
		if tc.traceID != "0af7651916cd43dd8448eb211c80319c" {
			t.Errorf("unexpected traceID: got %q", tc.traceID)
		}
		if tc.spanID != "b7ad6b7169203331" {
			t.Errorf("unexpected spanID: got %q", tc.spanID)
		}
	})

	t.Run("invalid traceparent", func(t *testing.T) {
		tc := parseTraceparent("not-a-valid-traceparent")
		if tc != (traceContext{}) {
			t.Errorf("expected zero traceContext, got %+v", tc)
		}
	})
}
