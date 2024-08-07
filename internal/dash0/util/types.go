// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import corev1 "k8s.io/api/core/v1"

type Reason string

const (
	ReasonSuccessfulInstrumentation    Reason = "SuccessfulInstrumentation"
	ReasonNoInstrumentationNecessary   Reason = "AlreadyInstrumented"
	ReasonFailedInstrumentation        Reason = "FailedInstrumentation"
	ReasonSuccessfulUninstrumentation  Reason = "SuccessfulUninstrumentation"
	ReasonNoUninstrumentationNecessary Reason = "AlreadyNotInstrumented"
	ReasonFailedUninstrumentation      Reason = "FailedUninstrumentation"
)

var AllEvents = []Reason{
	ReasonSuccessfulInstrumentation,
	ReasonNoInstrumentationNecessary,
	ReasonFailedInstrumentation,
	ReasonSuccessfulUninstrumentation,
	ReasonNoUninstrumentationNecessary,
	ReasonFailedUninstrumentation,
}

type Images struct {
	OperatorImage                        string
	InitContainerImage                   string
	InitContainerImagePullPolicy         corev1.PullPolicy
	CollectorImage                       string
	CollectorImagePullPolicy             corev1.PullPolicy
	ConfigurationReloaderImage           string
	ConfigurationReloaderImagePullPolicy corev1.PullPolicy
}

type InstrumentationMetadata struct {
	Images
	OTelCollectorBaseUrl string
	InstrumentedBy       string
}
