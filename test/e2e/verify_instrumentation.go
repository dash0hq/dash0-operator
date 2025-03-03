// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testEndpoint           = "/dash0-k8s-operator-test"
	labelChangeTimeout     = 25 * time.Second
	verifyTelemetryTimeout = 40 * time.Second
	pollingInterval        = 500 * time.Millisecond

	cronjob = "cronjob"
)

func verifyThatWorkloadHasBeenInstrumented(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	images Images,
	instrumentationBy string,
	expectClusterName bool,
) {
	By(fmt.Sprintf("%s %s: waiting for the workload to get instrumented (polling its labels and events to check)",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Eventually(func(g Gomega) {
		verifyLabels(
			g,
			namespace,
			runtime,
			workloadType,
			true,
			images,
			instrumentationBy,
		)
		verifySuccessfulInstrumentationEvent(g, namespace, runtime, workloadType, instrumentationBy)
	}, labelChangeTimeout, pollingInterval).Should(Succeed())

	By(fmt.Sprintf("%s %s: waiting for spans to be captured", runtime.runtimeTypeLabel, workloadType.workloadTypeString))

	// For workload types that are available as a service (daemonset, deployment etc.) we send HTTP requests with
	// a unique ID as a query parameter. When checking the produced spans, we can verify that the span is
	// actually from the currently running test case by inspecting the http.target span attribute. This guarantees
	// that we do not accidentally pass the test due to a span from a previous test case.
	//
	// For batch workloads (job, cronjob), which are not reachable via a service, the application will call itself via
	// HTTP instead, which will create spans as well.
	spanTimeout := verifyTelemetryTimeout
	if workloadType.workloadTypeString == "cronjob" {
		// Cronjob pods are only scheduled once a minute, so we might need to wait a while for a job to be started
		// and for spans to become available, hence increasing the timeout for "Eventually" block that waits for spans.
		spanTimeout = 90 * time.Second
	}
	query := fmt.Sprintf("id=%s", testId)
	timestampLowerBound := time.Now()
	Eventually(func(g Gomega) {
		verifySpans(g, runtime, workloadType, testEndpoint, query, timestampLowerBound, expectClusterName)
	}, spanTimeout, pollingInterval).Should(Succeed())
	By(fmt.Sprintf("%s %s: matching spans have been received", runtime.runtimeTypeLabel, workloadType.workloadTypeString))
}

func verifyThatInstrumentationHasBeenReverted(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	instrumentationBy string,
) {
	verifyThatInstrumentationIsRevertedEventually(
		namespace,
		runtime,
		workloadType,
		testId,
		instrumentationBy,
		false,
	)
}

func verifyThatInstrumentationHasBeenRevertedAfterAddingOptOutLabel(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	instrumentationBy string,
) {
	verifyThatInstrumentationIsRevertedEventually(
		namespace,
		runtime,
		workloadType,
		testId,
		instrumentationBy,
		true,
	)
}

func verifyThatInstrumentationIsRevertedEventually(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	instrumentationBy string,
	expectOptOutLabel bool,
) {
	By(fmt.Sprintf(
		"%s %s: waiting for the instrumentation to get removed from the workload (polling its labels and events to check)",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Eventually(func(g Gomega) {
		if expectOptOutLabel {
			verifyOnlyOptOutLabelIsPresent(g, namespace, runtime, workloadType)
		} else {
			verifyNoDash0Labels(g, namespace, runtime, workloadType)
		}
		verifySuccessfulUninstrumentationEvent(g, namespace, runtime, workloadType, instrumentationBy)
	}, labelChangeTimeout, pollingInterval).Should(Succeed())

	// Add some buffer time between the workloads being restarted and verifying that no spans are produced/captured.
	time.Sleep(10 * time.Second)

	secondsToCheckForSpans := 20
	if workloadType.workloadTypeString == cronjob {
		// Pod for cron jobs only get scheduled once a minute, since the cronjob schedule format does not allow for jobs
		// starting every second. Thus, to make the test valid, we need to monitor for spans a little bit longer than
		// for other workload types.
		secondsToCheckForSpans = 80
	}
	By(
		fmt.Sprintf("%s %s: verifying that spans are no longer being captured (checking for %d seconds)",
			runtime.runtimeTypeLabel,
			workloadType.workloadTypeString,
			secondsToCheckForSpans,
		))
	query := fmt.Sprintf("id=%s", testId)
	timestampLowerBound := time.Now()
	Consistently(func(g Gomega) {
		verifyNoSpans(g, runtime, workloadType, testEndpoint, query, timestampLowerBound)
	}, time.Duration(secondsToCheckForSpans)*time.Second, 1*time.Second).Should(Succeed())

	By(fmt.Sprintf(
		"%s %s: matching spans are no longer captured",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString,
	))
}

func waitForApplicationToBecomeReady(runtimeTypeLabel, workloadType string, waitCommand *exec.Cmd) error {
	By(fmt.Sprintf("waiting for %s %s to become ready", runtimeTypeLabel, workloadType))
	err := runAndIgnoreOutput(waitCommand)
	if err != nil {
		By(fmt.Sprintf("%s %s never became ready", runtimeTypeLabel, workloadType))
	} else {
		By(fmt.Sprintf("%s %s is ready now", runtimeTypeLabel, workloadType))
	}
	return err
}
