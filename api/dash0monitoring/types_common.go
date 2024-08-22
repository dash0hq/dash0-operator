// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

type ConditionType string

const (
	ConditionTypeAvailable ConditionType = "Available"
	ConditionTypeDegraded  ConditionType = "Degraded"
)

type Dash0Resource interface {
	IsMarkedForDeletion() bool
	IsAvailable() bool
	EnsureResourceIsMarkedAsAvailable()
	EnsureResourceIsMarkedAsDegraded(
		reason string,
		message string,
	)
	EnsureResourceIsMarkedAsAboutToBeDeleted()
	SetAvailableConditionToUnknown()
}

func ReadBooleanOptOutSetting(setting *bool) bool {
	return readOptionalBooleanWithDefault(setting, true)
}

func readOptionalBooleanWithDefault(setting *bool, defaultValue bool) bool {
	if setting == nil {
		return defaultValue
	}
	return *setting
}
