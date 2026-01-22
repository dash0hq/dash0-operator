// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/gomega"

	testUtil "github.com/dash0hq/dash0-operator/test/util"
)

func verifySuccessfulInstrumentationEvent(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	eventSource string,
) {
	verifyEvent(
		g,
		namespace,
		runtime,
		workloadType,
		corev1.EventTypeNormal,
		util.ReasonSuccessfulInstrumentation,
		util.ActionInstrumentation,
		fmt.Sprintf("Dash0 instrumentation of this workload by the %s has been successful.", eventSource),
	)
}

func verifyFailedInstrumentationEvent(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	note string,
) {
	verifyEvent(
		g,
		namespace,
		runtime,
		workloadType,
		corev1.EventTypeWarning,
		util.ReasonFailedInstrumentation,
		util.ActionInstrumentation,
		note,
	)
}

func verifySuccessfulUninstrumentationEvent(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	eventSource string,
) {
	verifyEvent(
		g,
		namespace,
		runtime,
		workloadType,
		corev1.EventTypeNormal,
		util.ReasonSuccessfulUninstrumentation,
		util.ActionUninstrumentation,
		fmt.Sprintf("The %s successfully removed the Dash0 instrumentation from this workload.", eventSource),
	)
}

func verifyFailedUninstrumentationEvent(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	note string,
) {
	verifyEvent(
		g,
		namespace,
		runtime,
		workloadType,
		corev1.EventTypeWarning,
		util.ReasonFailedUninstrumentation,
		util.ActionUninstrumentation,
		note,
	)
}

func verifyEvent(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	eventType string,
	reason util.Reason,
	action util.Action,
	note string,
) {
	resourceName := workloadName(runtime, workloadType)
	eventsJson, err := run(exec.Command(
		"kubectl",
		"get",
		"events.events.k8s.io",
		"-ojson",
		"--namespace",
		namespace,
		"--field-selector",
		fmt.Sprintf("regarding.kind=%s,regarding.name=%s", workloadType.workloadTypeStringCamelCase, resourceName),
	), false)
	g.Expect(err).NotTo(HaveOccurred())
	var events eventsv1.EventList
	err = json.Unmarshal([]byte(eventsJson), &events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events.Items).To(
		ContainElement(
			testUtil.MatchEvent(
				namespace,
				resourceName,
				eventType,
				reason,
				action,
				note,
			)))
}
