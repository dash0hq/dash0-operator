// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"

	corev1 "k8s.io/api/core/v1"

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
		util.ReasonSuccessfulInstrumentation,
		fmt.Sprintf("Dash0 instrumentation of this workload by the %s has been successful.", eventSource),
	)
}

func verifyFailedInstrumentationEvent(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	message string,
) {
	verifyEvent(
		g,
		namespace,
		runtime,
		workloadType,
		util.ReasonFailedInstrumentation,
		message,
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
		util.ReasonSuccessfulUninstrumentation,
		fmt.Sprintf("The %s successfully removed the Dash0 instrumentation from this workload.", eventSource),
	)
}

func verifyFailedUninstrumentationEvent(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	message string,
) {
	verifyEvent(
		g,
		namespace,
		runtime,
		workloadType,
		util.ReasonFailedUninstrumentation,
		message,
	)
}

func verifyEvent(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	reason util.Reason,
	message string,
) {
	resourceName := workloadName(runtime, workloadType)
	eventsJson, err := run(exec.Command(
		"kubectl",
		"events",
		"-ojson",
		"--namespace",
		namespace,
		"--for",
		fmt.Sprintf("%s/%s", workloadType.workloadTypeString, resourceName),
	), false)
	g.Expect(err).NotTo(HaveOccurred())
	var events corev1.EventList
	err = json.Unmarshal([]byte(eventsJson), &events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events.Items).To(
		ContainElement(
			testUtil.MatchEvent(
				namespace,
				resourceName,
				reason,
				message,
			)))
}
