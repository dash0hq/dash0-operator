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
		fmt.Sprintf("Dash0 instrumentation of this workload by the %s has been successful.", eventSource),
	)
}

func QueueNoInstrumentationNecessaryEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(ReasonNoInstrumentationNecessary),
		fmt.Sprintf("Dash0 instrumentation was already present on this workload, or the workload is part of a higher "+
			"order workload that will be instrumented, no modification by the %s is necessary.", eventSource),
	)
}

func QueueFailedInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string, err error) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeWarning,
		string(ReasonFailedInstrumentation),
		fmt.Sprintf("Dash0 instrumentation of this workload by the %s has not been successful. Error message: %s", eventSource, err.Error()),
	)
}

func QueueSuccessfulUninstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(ReasonSuccessfulUninstrumentation),
		fmt.Sprintf("The %s successfully removed the Dash0 instrumentation from this workload.", eventSource),
	)
}

func QueueNoUninstrumentationNecessaryEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(ReasonAlreadyNotInstrumented),
		fmt.Sprintf("Dash0 instrumentation was not present on this workload, no modification by the %s has been necessary.", eventSource),
	)
}

func QueueFailedUninstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string, err error) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeWarning,
		string(ReasonFailedUninstrumentation),
		fmt.Sprintf("The %s's attempt to remove the Dash0 instrumentation from this workload has not been successful. Error message: %s", eventSource, err.Error()),
	)
}
