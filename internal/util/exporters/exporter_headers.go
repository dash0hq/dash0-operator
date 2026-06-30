// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package exporters

import (
	"fmt"
	"strings"
)

const headerSecretEnvVarPrefix = "DASH0_HEADER"

// HeaderSecretEnvVarName derives a unique, valid environment variable name for a secret-backed header value. The
// protocol ("GRPC" or "HTTP") and nameSuffix (e.g. "default_0" or "ns/some-namespace_0") together with the header
// index guarantee uniqueness across all exporters of a single collector pod. The collector references this environment
// variable from the rendered header value via ${env:...}. It is used both when rendering the exporters section and when
// rendering the self-monitoring pipelines, which must agree on the name so the references resolve to the same
// environment variable injected into the collector pod.
func HeaderSecretEnvVarName(protocol string, nameSuffix string, headerIndex int) string {
	return fmt.Sprintf("%s_%s_%s_%d", headerSecretEnvVarPrefix, protocol, sanitizeForEnvVarName(nameSuffix), headerIndex)
}

// sanitizeForEnvVarName converts an arbitrary string into an uppercase identifier consisting only of the characters
// [A-Z0-9_], which is required for environment variable names.
func sanitizeForEnvVarName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
