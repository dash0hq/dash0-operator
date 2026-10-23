// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import "strings"

const (
	userAgentProduct        = "Dash0Operator"
	userAgentUnknownVersion = "unknown"
)

// RenderUserAgent returns the value the operator uses for the User-Agent header
// when calling the Dash0 API. If version is empty (for example when the operator
// image carries neither a tag nor a digest) the version segment is reported as
// "unknown" so that the header is always well-formed. Colons in the version
// (as produced for digest-pinned images like "sha256:abc...") are replaced with
// "-" so the result is a valid RFC 7230 token.
func RenderUserAgent(version string) string {
	if version == "" {
		version = userAgentUnknownVersion
	}
	return userAgentProduct + "/" + strings.ReplaceAll(version, ":", "-")
}
