// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
)

func QueueSuccessfulInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(operatorv1alpha1.ReasonSuccessfulInstrumentation),
		fmt.Sprintf("Dash0 instrumentation by %s has been successful.", eventSource),
	)
}

func QueueAlreadyInstrumentedEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeNormal,
		string(operatorv1alpha1.ReasonSuccessfulInstrumentation),
		fmt.Sprintf("Dash0 instrumentation already present, no modification by %s is necessary.", eventSource),
	)
}

func QueueFailedInstrumentationEvent(eventRecorder record.EventRecorder, resource runtime.Object, eventSource string, err error) {
	eventRecorder.Event(
		resource,
		corev1.EventTypeWarning,
		string(operatorv1alpha1.ReasonFailedInstrumentation),
		fmt.Sprintf("Dash0 instrumentation by %s has not been successful. Error message: %s", eventSource, err.Error()),
	)
}
