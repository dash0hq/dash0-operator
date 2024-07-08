// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

func ReadOptOutSetting(setting *bool) bool {
	return ReadOptionalBooleanWithDefault(setting, true)
}

func ReadOptionalBooleanWithDefault(setting *bool, defaultValue bool) bool {
	if setting == nil {
		return defaultValue
	}
	return *setting
}
