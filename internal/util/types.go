// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

type ConditionType string
type Reason string

const (
	ConditionTypeAvailable ConditionType = "Available"
	ConditionTypeDegraded  ConditionType = "Degraded"

	ReasonSuccessfulInstrumentation    Reason = "SuccessfulInstrumentation"
	ReasonNoInstrumentationNecessary   Reason = "ReasonAlreadyInstrumented"
	ReasonFailedInstrumentation        Reason = "FailedInstrumentation"
	ReasonSuccessfulUninstrumentation  Reason = "SuccessfulUninstrumentation"
	ReasonNoUninstrumentationNecessary Reason = "ReasonAlreadyNotInstrumented"
	ReasonFailedUninstrumentation      Reason = "FailedUninstrumentation"
)

type Images struct {
	OperatorImage      string
	InitContainerImage string
}

type InstrumentationMetadata struct {
	Images
	InstrumentedBy string
}
