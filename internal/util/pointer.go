// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

func ReadBoolPointerWithDefault(pointer *bool, defaultValue bool) bool {
	if pointer != nil {
		return *pointer
	}
	return defaultValue
}
