// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import "net"

// IsIPv6Address reports whether the given string is a valid IPv6 address literal. It returns false for IPv4 addresses
// and for strings that are not valid IP addresses at all.
func IsIPv6Address(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() == nil
}
