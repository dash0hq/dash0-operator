// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This is a copy of
// https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/tags/v0.126.0/processor/transformprocessor/internal/traces/functions.go

package traces

import (
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlspan"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlspanevent"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/ottlfuncs"
)

func SpanFunctions() map[string]ottl.Factory[ottlspan.TransformContext] {
	// No trace-only functions yet.
	m := ottlfuncs.StandardFuncs[ottlspan.TransformContext]()
	isRootSpanFactory := ottlfuncs.NewIsRootSpanFactory()
	m[isRootSpanFactory.Name()] = isRootSpanFactory
	return m
}

func SpanEventFunctions() map[string]ottl.Factory[ottlspanevent.TransformContext] {
	// No trace-only functions yet.
	return ottlfuncs.StandardFuncs[ottlspanevent.TransformContext]()
}
