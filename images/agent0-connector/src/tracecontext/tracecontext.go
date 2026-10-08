// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package tracecontext

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TraceContext holds the trace and span IDs extracted from the W3C traceparent header of a CommandRequest. Either field
// may be empty if the request carried no (or a malformed) traceparent.
type TraceContext struct {
	TraceID string
	SpanID  string
}

// traceparentPropagator parses W3C traceparent headers.
var traceparentPropagator = propagation.TraceContext{}

// Extract parses a W3C traceparent header (e.g. "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01") and returns
// the given context with the span context of the upstream caller attached, together with the trace and span ID for
// logging. A missing or malformed header returns the given context unchanged and yields a zero TraceContext.
//
// To correlate telemetry emitted by the connector with the trace of the upstream caller, use the context returned by
// this method when processing the request. The OTel slog bridge adds the trace and span ID from the context to every
// log record it converts.
func Extract(ctx context.Context, traceparent string) (context.Context, TraceContext) {
	extractedCtx := traceparentPropagator.Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
	sc := trace.SpanContextFromContext(extractedCtx)
	if !sc.IsValid() {
		return ctx, TraceContext{}
	}
	return extractedCtx, TraceContext{
		TraceID: sc.TraceID().String(),
		SpanID:  sc.SpanID().String(),
	}
}
