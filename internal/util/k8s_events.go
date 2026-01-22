// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func HandlePotentiallySuccessfulInstrumentationEvent(
	eventRecorder events.EventRecorder,
	resource runtime.Object,
	eventSource WorkloadModifierActor,
	containersTotal int,
	instrumentationIssuesPerContainer map[string][]string,
	logger *logr.Logger,
) {
	if len(instrumentationIssuesPerContainer) == 0 {
		// All containers have been instrumented, no container had instrumentation issues.
		logger.Info(fmt.Sprintf("The %s has added Dash0 instrumentation to the workload.", eventSource))
		queueSuccessfulInstrumentationEvent(eventRecorder, resource, eventSource)
		return
	}

	// The action has been partially unsuccessful, i.e. some containers have been instrumented, some (or all) had
	// instrumentation issues. Even if all containers have issues, this is reported as a partial success, since an issue
	// does not necessarily mean that no telmetry is collected (i.e. a manually set OTEL_EXPORT_OTLP_ENDPOINT that we do
	// not override).
	queuePartiallyUnsuccessfulInstrumentationEvent(
		eventRecorder,
		resource,
		eventSource,
		containersTotal,
		instrumentationIssuesPerContainer,
	)
}

func queueSuccessfulInstrumentationEvent(
	eventRecorder events.EventRecorder,
	resource runtime.Object,
	eventSource WorkloadModifierActor,
) {
	eventRecorder.Eventf(
		resource,
		nil,
		corev1.EventTypeNormal,
		string(ReasonSuccessfulInstrumentation),
		string(ActionInstrumentation),
		fmt.Sprintf("Dash0 instrumentation of this workload by the %s has been successful.", eventSource),
	)
}

func queuePartiallyUnsuccessfulInstrumentationEvent(
	eventRecorder events.EventRecorder,
	resource runtime.Object,
	eventSource WorkloadModifierActor,
	containersTotal int,
	instrumentationIssuesPerContainer map[string][]string,
) {
	eventRecorder.Eventf(
		resource,
		nil,
		corev1.EventTypeWarning,
		string(ReasonPartiallyUnsuccessfulInstrumentation),
		string(ActionInstrumentation),
		fmt.Sprintf(
			"Dash0 instrumentation of this workload by the %s has been partially unsuccessful, %d out of %d containers have instrumentation issues. %s",
			eventSource,
			len(instrumentationIssuesPerContainer),
			containersTotal,
			stringifyContainerInstrumentationIssues(instrumentationIssuesPerContainer),
		),
	)
}

func QueueNoInstrumentationNecessaryEvent(eventRecorder events.EventRecorder, resource runtime.Object, note string) {
	eventRecorder.Eventf(
		resource,
		nil,
		corev1.EventTypeNormal,
		string(ReasonNoInstrumentationNecessary),
		string(ActionInstrumentation),
		note,
	)
}

func QueueFailedInstrumentationEvent(eventRecorder events.EventRecorder, resource runtime.Object, eventSource WorkloadModifierActor, err error) {
	eventRecorder.Eventf(
		resource,
		nil,
		corev1.EventTypeWarning,
		string(ReasonFailedInstrumentation),
		string(ActionInstrumentation),
		fmt.Sprintf("Dash0 instrumentation of this workload by the %s has not been successful. Error message: %s", eventSource, err.Error()),
	)
}

func QueueSuccessfulUninstrumentationEvent(eventRecorder events.EventRecorder, resource runtime.Object, eventSource WorkloadModifierActor) {
	eventRecorder.Eventf(
		resource,
		nil,
		corev1.EventTypeNormal,
		string(ReasonSuccessfulUninstrumentation),
		string(ActionUninstrumentation),
		fmt.Sprintf("The %s successfully removed the Dash0 instrumentation from this workload.", eventSource),
	)
}

func QueueNoUninstrumentationNecessaryEvent(eventRecorder events.EventRecorder, resource runtime.Object, eventSource WorkloadModifierActor) {
	eventRecorder.Eventf(
		resource,
		nil,
		corev1.EventTypeNormal,
		string(ReasonNoUninstrumentationNecessary),
		string(ActionUninstrumentation),
		fmt.Sprintf("Dash0 instrumentation was not present on this workload, no modification by the %s has been necessary.", eventSource),
	)
}

func QueueFailedUninstrumentationEvent(eventRecorder events.EventRecorder, resource runtime.Object, eventSource WorkloadModifierActor, err error) {
	eventRecorder.Eventf(
		resource,
		nil,
		corev1.EventTypeWarning,
		string(ReasonFailedUninstrumentation),
		string(ActionUninstrumentation),
		fmt.Sprintf("The %s's attempt to remove the Dash0 instrumentation from this workload has not been successful. Error message: %s", eventSource, err.Error()),
	)
}

func stringifyContainerInstrumentationIssues(instrumentationIssuesPerContainer map[string][]string) string {
	var sb strings.Builder
	for idx, containerName := range slices.Sorted(maps.Keys(instrumentationIssuesPerContainer)) {
		sb.WriteString(containerName)
		sb.WriteString(": ")
		sb.WriteString(strings.Join(instrumentationIssuesPerContainer[containerName], " "))
		if idx < len(instrumentationIssuesPerContainer)-1 {
			sb.WriteString(" ")
		}
	}
	return sb.String()
}

// AttachEventToInvolvedObject attaches an event that has been created by the webhook to its referenced object by
// setting the event's InvolvedObject.UID. This only works with the legacy event API from k8s.io/api/core/v1, since the
// equivalent field (Regarding.UID) is immutable in the new API from k8s.io/api/events/v1.
//
// Without doing this, the event will not show up in the event section of "kubectl describe" for the workload, because
// kubectl always searches for related events with the object's uid.
func AttachEventToInvolvedObject(
	ctx context.Context,
	k8sClient client.Client,
	legacyEventApi clientcorev1.EventInterface,
	legacyEvent *corev1.Event,
) error {
	if err := setUidAndResourceVersionForEvent(ctx, k8sClient, legacyEvent); err != nil {
		return fmt.Errorf("could not update event.Regarding: %w", err)
	}
	if _, err := legacyEventApi.Update(ctx, legacyEvent, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("could not update the dangling event %v: %w", legacyEvent.UID, err)
	}
	return nil
}

func setUidAndResourceVersionForEvent(
	ctx context.Context,
	k8sClient client.Client,
	legacyEvent *corev1.Event,
) error {
	involvedObject := &legacyEvent.InvolvedObject
	object, err := GetReceiverForWorkloadType(involvedObject.APIVersion, involvedObject.Kind)
	if err != nil {
		return err
	}

	if err = k8sClient.Get(
		ctx,
		client.ObjectKey{Namespace: involvedObject.Namespace, Name: involvedObject.Name},
		object,
	); err != nil {
		return fmt.Errorf(
			"could not load involved object %s %s/%s: %w",
			involvedObject.Kind,
			involvedObject.Namespace,
			involvedObject.Name,
			err,
		)
	}

	involvedObject.UID = object.GetUID()
	involvedObject.ResourceVersion = object.GetResourceVersion()

	return nil
}
