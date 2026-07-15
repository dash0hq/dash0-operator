// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"bytes"
	"encoding/json"
	"strings"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// sensitiveEnvVarNamePrefixes are the (prefixes of the) env var names under which the operator injects plaintext
// secrets into managed workloads. Prefix-matched because the names carry per-export/-namespace/-index suffixes.
var sensitiveEnvVarNamePrefixes = []string{
	"OTELCOL_AUTH_TOKEN",                // collector Dash0 export auth token(s)
	"SELF_MONITORING_AUTH_TOKEN",        // collector self-monitoring auth token
	"DASH0_AGENT0_CONNECTOR_AUTH_TOKEN", // agent0-connector auth token
	"EDGE_PROXY_AUTH_TOKEN",             // signal-control edge proxy auth token
	"DASH0_HEADER",                      // collector gRPC/HTTP header secrets
}

func isSensitiveEnvVarName(name string) bool {
	for _, prefix := range sensitiveEnvVarNamePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// RedactSensitiveEnvVarsInPatch returns the reconcile patch with the values of secret-bearing env vars (see
// sensitiveEnvVarNamePrefixes) replaced by the redaction marker, so patch logs never expose a literal auth token. An
// unparseable patch is omitted rather than risk a leak.
func RedactSensitiveEnvVarsInPatch(patch []byte) string {
	var parsed any
	if err := json.Unmarshal(patch, &parsed); err != nil {
		return "<patch omitted: not parseable as JSON, cannot verify it is free of secrets>"
	}
	redactSensitiveEnvVars(parsed)
	// Encode without HTML escaping to keep the marker and endpoints readable.
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(parsed); err != nil {
		return "<patch omitted: redaction failed>"
	}
	return strings.TrimSpace(buf.String())
}

func redactSensitiveEnvVars(node any) {
	switch n := node.(type) {
	case map[string]any:
		if nameRaw, hasName := n["name"]; hasName {
			if name, ok := nameRaw.(string); ok && isSensitiveEnvVarName(name) {
				if _, hasValue := n["value"]; hasValue {
					n["value"] = dash0common.RedactedValue
				}
			}
		}
		for _, child := range n {
			redactSensitiveEnvVars(child)
		}
	case []any:
		for _, child := range n {
			redactSensitiveEnvVars(child)
		}
	}
}
