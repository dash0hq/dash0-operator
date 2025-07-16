// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"strings"
)

func ReadBoolPointerWithDefault(pointer *bool, defaultValue bool) bool {
	if pointer != nil {
		return *pointer
	}
	return defaultValue
}

func IsOptOutFlagWithDeprecatedVariantEnabled(deprecatedSetting *bool, newSetting *bool) bool {
	deprecateValue := ReadBoolPointerWithDefault(deprecatedSetting, true)
	newValue := ReadBoolPointerWithDefault(newSetting, true)
	// Opt-out switches default to true if not explicitly set, so if one of the values is false, we know it has
	// explicitly been set to false with the purpose of disabling the respective feature.
	return deprecateValue && newValue
}

// IsStringPointerValueDifferent returns true if and only if one of the pointers is nil and the other is not nil, not
// the empty string, or only contains whitespace; or if both pointers are not nil and their values differ, except for
// surrounding whitespace.
func IsStringPointerValueDifferent(a *string, b *string) bool {
	if IsEmpty(a) && !IsEmpty(b) {
		return true
	}
	if !IsEmpty(a) && IsEmpty(b) {
		return true
	}
	if a != nil && b != nil && strings.TrimSpace(*a) != strings.TrimSpace(*b) {
		return true
	}
	return false
}

// IsEmpty checks if the string pointer is nil, or if is the empty string or only contains whitespace.
func IsEmpty(s *string) bool {
	if s == nil {
		return true
	}
	return strings.TrimSpace(*s) == ""
}
