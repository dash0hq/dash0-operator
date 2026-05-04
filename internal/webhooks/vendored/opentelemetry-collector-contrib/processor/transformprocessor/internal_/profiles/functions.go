// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This is a copy of
// https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/refs/tags/v0.151.0/processor/transformprocessor/internal/profiles/functions.go

package profiles

import (
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottlprofile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/ottlfuncs"
)

func ProfileFunctions() map[string]ottl.Factory[*ottlprofile.TransformContext] {
	// No profiles-only functions yet.
	return ottlfuncs.StandardFuncs[*ottlprofile.TransformContext]()
}
