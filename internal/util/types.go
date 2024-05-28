// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

type ConditionType string
type Reason string

const (
	ConditionTypeAvailable ConditionType = "Available"
	ConditionTypeDegraded  ConditionType = "Degraded"

	ReasonSuccessfulInstrumentation   Reason = "SuccessfulInstrumentation"
	ReasonAlreadyInstrumented         Reason = "ReasonAlreadyInstrumented"
	ReasonFailedInstrumentation       Reason = "FailedInstrumentation"
	ReasonSuccessfulUninstrumentation Reason = "SuccessfulUninstrumentation"
	ReasonAlreadyNotInstrumented      Reason = "ReasonAlreadyNotInstrumented"
	ReasonFailedUninstrumentation     Reason = "FailedUninstrumentation"
)

type Versions struct {
	OperatorVersion           string
	InitContainerImageVersion string
}

type InstrumentationMetadata struct {
	Versions
	InstrumentedBy string
}
