// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This is a copy of
// https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/tags/v0.142.0/processor/transformprocessor/internal/logs/functions.go

package logs

import (
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottllog"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/ottlfuncs"
)

func LogFunctions() map[string]ottl.Factory[*ottllog.TransformContext] {
	// No logs-only functions yet.
	return ottlfuncs.StandardFuncs[*ottllog.TransformContext]()
}
