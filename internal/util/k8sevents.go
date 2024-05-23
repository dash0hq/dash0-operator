// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

func QueueSuccessfulInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(ReasonSuccessfulInstrumentation),
		fmt.Sprintf("Dash0 instrumentation by %s has been successful.", eventSource),
	)
}

func QueueAlreadyInstrumentedEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(ReasonSuccessfulInstrumentation),
		fmt.Sprintf("Dash0 instrumentation already present, no modification by %s is necessary.", eventSource),
	)
}

func QueueFailedInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string, err error) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeWarning,
		string(ReasonFailedInstrumentation),
		fmt.Sprintf("Dash0 instrumentation by %s has not been successful. Error message: %s", eventSource, err.Error()),
	)
}
