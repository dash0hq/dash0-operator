// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

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
