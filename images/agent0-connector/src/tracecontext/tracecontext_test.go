// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package tracecontext

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestExtract(t *testing.T) {
	t.Run("valid traceparent", func(t *testing.T) {
		ctx, tc := Extract(context.Background(), "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
		if tc.TraceID != "0af7651916cd43dd8448eb211c80319c" {
			t.Errorf("unexpected traceID: got %q", tc.TraceID)
		}
		if tc.SpanID != "b7ad6b7169203331" {
			t.Errorf("unexpected spanID: got %q", tc.SpanID)
		}
		spanContext := trace.SpanContextFromContext(ctx)
		if !spanContext.IsValid() {
			t.Error("expected the returned context to carry the span context of the upstream caller")
		}
		if spanContext.TraceID().String() != tc.TraceID || spanContext.SpanID().String() != tc.SpanID {
			t.Errorf("unexpected span context in the returned context: got %+v", spanContext)
		}
	})

	t.Run("invalid traceparent", func(t *testing.T) {
		ctx, tc := Extract(context.Background(), "not-a-valid-traceparent")
		if tc != (TraceContext{}) {
			t.Errorf("expected zero traceContext, got %+v", tc)
		}
		if trace.SpanContextFromContext(ctx).IsValid() {
			t.Error("expected the returned context to carry no span context")
		}
	})
}
