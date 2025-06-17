// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

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
	FilelogOffsetSyncImage               string
	FilelogOffsetSyncImagePullPolicy     corev1.PullPolicy
}

func (i Images) GetOperatorVersion() string {
	return getImageVersion(i.OperatorImage)
}

func getImageVersion(image string) string {
	idx := strings.LastIndex(image, "@")
	if idx >= 0 {
		return image[idx+1:]
	}
	idx = strings.LastIndex(image, ":")
	if idx >= 0 {
		return image[idx+1:]
	}
	return ""
}

type InstrumentationMetadata struct {
	Images
	OTelCollectorBaseUrl string
	InstrumentedBy       WorkloadModifierActor
	InstrumentationDebug bool
}

type ModificationMode string

const (
	ModificationModeInstrumentation   ModificationMode = "instrumentation"
	ModificationModeUninstrumentation ModificationMode = "uninstrumentation"
)

type DanglingEventsTimeouts struct {
	InitialTimeout time.Duration
	Backoff        wait.Backoff
}
