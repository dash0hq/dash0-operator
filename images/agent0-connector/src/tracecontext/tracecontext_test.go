// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package tracecontext

import (
	"testing"
)

func TestParseTraceparent(t *testing.T) {
	t.Run("valid traceparent", func(t *testing.T) {
		tc := ParseTraceparent("00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
		if tc.TraceID != "0af7651916cd43dd8448eb211c80319c" {
			t.Errorf("unexpected traceID: got %q", tc.TraceID)
		}
		if tc.SpanID != "b7ad6b7169203331" {
			t.Errorf("unexpected spanID: got %q", tc.SpanID)
		}
	})

	t.Run("invalid traceparent", func(t *testing.T) {
		tc := ParseTraceparent("not-a-valid-traceparent")
		if tc != (TraceContext{}) {
			t.Errorf("expected zero traceContext, got %+v", tc)
		}
	})
}
