// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package enablement

// Result is the organization's Signal Control entitlement as determined by the Dash0 API. It is a three-state value
// so that the collector can distinguish "not yet checked" (Unknown) from a definitive negative answer (NotAllowed).
type Result int32

const (
	// ResultUnknown means the entitlement has not been determined yet, or the last check could not complete.
	ResultUnknown Result = iota
	// ResultAllowed means the organization is entitled to use Signal Control.
	ResultAllowed
	// ResultNotAllowed means the organization is not entitled to use Signal Control.
	ResultNotAllowed
)

func (r Result) String() string {
	switch r {
	case ResultAllowed:
		return "Allowed"
	case ResultNotAllowed:
		return "NotAllowed"
	default:
		return "Unknown"
	}
}
