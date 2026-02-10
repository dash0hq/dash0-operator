// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This is a copy of
// https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/tags/v0.145.0/processor/transformprocessor/internal/common/functions.go

package common

import (
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlresource"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlscope"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/ottlfuncs"
)

func ResourceFunctions() map[string]ottl.Factory[*ottlresource.TransformContext] {
	return ottlfuncs.StandardFuncs[*ottlresource.TransformContext]()
}

func ScopeFunctions() map[string]ottl.Factory[*ottlscope.TransformContext] {
	return ottlfuncs.StandardFuncs[*ottlscope.TransformContext]()
}
