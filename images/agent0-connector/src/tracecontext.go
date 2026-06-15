// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// traceContext holds the trace and span IDs extracted from the W3C traceparent header of a CommandRequest. Either field
// may be empty if the request carried no (or a malformed) traceparent.
type traceContext struct {
	traceID string
	spanID  string
}

// traceparentPropagator parses W3C traceparent headers.
var traceparentPropagator = propagation.TraceContext{}

// parseTraceparent parses a W3C traceparent header (e.g. "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
// into a traceContext. A missing or malformed header yields a zero traceContext.
func parseTraceparent(traceparent string) traceContext {
	ctx := traceparentPropagator.Extract(context.Background(), propagation.MapCarrier{"traceparent": traceparent})
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return traceContext{}
	}
	return traceContext{
		traceID: sc.TraceID().String(),
		spanID:  sc.SpanID().String(),
	}
}
