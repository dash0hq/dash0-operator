// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package exporters

import "fmt"

// NameSuffixDefault derives the exporter name suffix for the export with the given index of the operator
// configuration's export settings (the "default" export, in contrast to per-namespace exports configured via the
// Dash0Monitoring resource). It is used both when rendering the exporters section of the collector configuration and
// when deriving the environment variable names for secret-backed header values, which must agree on the suffix so the
// ${env:...} references resolve to the environment variables actually injected into the collector pod.
func NameSuffixDefault(index int) string {
	return fmt.Sprintf("default_%d", index)
}

// NameSuffixNs derives the exporter name suffix for the export with the given index of a per-namespace export
// configured via the Dash0Monitoring resource in the given namespace. See NameSuffixDefault for how the suffix is
// used.
func NameSuffixNs(index int, namespace string) string {
	return fmt.Sprintf("ns/%s_%d", namespace, index)
}
